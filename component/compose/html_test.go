package compose

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestHTMLPreservesComposedFragmentStructure(t *testing.T) {
	t.Parallel()
	// This is the body returned by the composer for the live inventory file.
	body := `<h1>Orchard inventory</h1>
<table>
  <tr><th>Fruit</th><th>Count</th></tr>
  <tr><td>Apples</td><td>3</td></tr>
  <tr><td>Pears</td><td>5</td></tr>
</table>
<p>Total fruit count: 8</p>`
	doc := renderHTMLBodyForTest(t, body)
	assertHTMLTexts(t, doc, "h1", "Orchard inventory")
	assertHTMLTexts(t, doc, "th", "Fruit", "Count")
	assertHTMLTexts(t, doc, "td", "Apples", "3", "Pears", "5")
	assertHTMLTexts(t, doc, "p", "Total fruit count: 8")
}

func TestHTMLRendersMarkdownHeadingsListsAndTables(t *testing.T) {
	t.Parallel()
	body := "# Orchard inventory\n\n" +
		"| Fruit | Count |\n| --- | ---: |\n| Apples | 3 |\n| Pears | 5 |\n\n" +
		"## Notes\n\n- **Fresh** fruit\n- Store `cool`\n\n1. Count\n2. Save\n"
	doc := renderHTMLBodyForTest(t, body)
	assertHTMLTexts(t, doc, "h1", "Orchard inventory")
	assertHTMLTexts(t, doc, "h2", "Notes")
	assertHTMLTexts(t, doc, "th", "Fruit", "Count")
	assertHTMLTexts(t, doc, "td", "Apples", "3", "Pears", "5")
	assertHTMLTexts(t, doc, "li", "Fresh fruit", "Store cool", "Count", "Save")
	assertHTMLTexts(t, doc, "strong", "Fresh")
	assertHTMLTexts(t, doc, "code", "cool")
}

func TestHTMLStripsActiveContentAndBodyAttributes(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`<h2 onclick="alert(1)" style="background:url(javascript:alert(2))">Safe heading</h2><script>script-sentinel</script><style>style-sentinel</style><p>Safe paragraph</p>`,
		`<h2>Safe heading</h2><iframe srcdoc="<script>alert(1)</script>"></iframe><object data="javascript:alert(1)"></object><embed src="data:text/html,bad"><p>Safe paragraph</p>`,
		`<h2>Safe heading</h2><svg onload="alert(1)"><a xlink:href="javascript:alert(1)">link</a></svg><math><mtext><img src=x onerror=alert(1)></mtext></math><p>Safe paragraph</p>`,
		`<h2>Safe heading</h2><a href="javascript:alert(1)">link</a><a href="jav&#x61;script:alert(1)">encoded</a><a href="data:text/html,bad">data</a><form action="https://example.com"><input autofocus onfocus="alert(1)"></form><p>Safe paragraph</p>`,
		"## Safe heading\n\n[unsafe](javascript:alert%281%29) ![image](https://example.com/track)\n\nSafe paragraph\n",
	} {
		t.Run(body, func(t *testing.T) {
			doc := renderHTMLBodyForTest(t, body)
			assertHTMLTexts(t, doc, "h2", "Safe heading")
			if !strings.Contains(htmlNodeText(doc), "Safe paragraph") {
				t.Fatal("sanitization lost safe content")
			}
			for _, tag := range []string{"script", "style", "iframe", "object", "embed", "svg", "math", "img", "form", "input"} {
				assertHTMLTexts(t, doc, tag)
			}
			walkHTML(doc, func(n *html.Node) {
				if len(n.Attr) != 0 {
					t.Errorf("body element %s retained attributes: %v", n.Data, n.Attr)
				}
			})
			for _, dropped := range []string{"script-sentinel", "style-sentinel"} {
				if strings.Contains(htmlNodeText(doc), dropped) {
					t.Errorf("active content remained as text: %s", dropped)
				}
			}
		})
	}
}

func renderHTMLBodyForTest(t *testing.T, body string) *html.Node {
	t.Helper()
	res, err := Render(FormatHTML, Draft{Body: body}, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(string(res.Bytes)))
	if err != nil {
		t.Fatal(err)
	}
	var found *html.Node
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			found = n
		}
	})
	if found == nil {
		t.Fatal("rendered document has no body")
	}
	return found
}

func assertHTMLTexts(t *testing.T, doc *html.Node, tag string, want ...string) {
	t.Helper()
	var got []string
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			got = append(got, strings.TrimSpace(htmlNodeText(n)))
		}
	})
	if len(got) != len(want) || strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("%s texts = %q, want %q", tag, got, want)
	}
}

func htmlNodeText(n *html.Node) string {
	var out strings.Builder
	walkHTML(n, func(n *html.Node) {
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
		}
	})
	return out.String()
}

func walkHTML(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}
