package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestNoVendorApiKeyEntryPoint: a manually entered vendor API key has no way
// back into the product (epic memql#5088, design D5).
//
// The engine reaches both cloud vendors by workload identity federation. There
// is no key field in MemQL OS, no `providerKeySet` builtin, no `apiKey` entry
// on either base provider, no key variable in the env registry, no key step in
// the install graph, and no sentence in the docs offering one. This gate is
// what keeps that list at zero, because every item on it was individually easy
// to re-add and none of them would have failed anything.
//
// WHAT IS NOT BANNED, and why the list is names rather than concepts:
//
//   - `vendor_api_key` as a globalSecret KIND is legitimate and still in use.
//     The router's BYOK path seals a user's own model key under it, and the
//     Shopify connector seals three store credentials under it. Those are
//     credentials for vendors MemQL does not federate with, entered by the
//     person they belong to. Banning the kind would have deleted a working
//     feature to enforce a rule about a different one.
//   - `MEMQL_AI_OPENAI_PROJECT_ID` is not a key. It names a project.
//   - `MEMQL_TEST_OPENAI_BEARER` is a test input read by exactly one live,
//     env-gated test and by nothing in the product; it is how an engineer
//     answers the open question in the OpenAI runbook about whether the
//     Realtime WebSocket accepts a federated bearer.
//
// The walk is `git ls-files`, which is this repo's convention for a gate:
// untracked files are invisible to it, so a new file must be `git add`ed
// before this test can see it.
func TestNoVendorApiKeyEntryPoint(t *testing.T) {
	banned := []string{
		"MEMQL_AI_ANTHROPIC_API_KEY",
		"MEMQL_AI_OPENAI_API_KEY",
		"MEMQL_ANTHROPIC_API_KEY",
		"MEMQL_OPENAI_API_KEY",
		"providerKeySet",
	}

	// The allow-list is DESIGN RECORDS AND THIS FILE, and nothing else.
	//
	// A design record is a dated account of a decision. The three named here
	// argue about these exact names -- two of them made the key the design and
	// the third removed it -- and rewriting history to satisfy a gate would
	// destroy the only explanation of why the gate exists. Everything else in
	// the tree describes the product as it IS, and the product has no key.
	allowed := map[string]bool{
		"docs/superpowers/specs/2026-09-06-openai-federation-and-key-removal-design.md":     true,
		"docs/superpowers/specs/2026-08-22-anthropic-workload-identity-federation-design.md": true,
		"docs/superpowers/specs/2026-08-23-zero-key-install-design.md":                       true,
		"docs/superpowers/specs/2026-08-08-local-cluster-install-wizard-design.md":           true,
		"docs/superpowers/specs/2026-09-01-integration-config-design.md":                     true,
		"docs/superpowers/specs/2026-09-06-configuration-readiness-design.md":                true,
		"docs/superpowers/specs/2026-09-06-portal-removal-design.md":                         true,
		"docs/internal/design/dsl-syntax-audit-964.md":                                       true,
		"docs/internal/ops/codeql-alert-triage.md":                                           true,
		"vendor_api_key_gate_test.go":                                                        true,

		// A NEGATIVE CONTROL, asserting the same absence from the other side.
		// editors/vscode/test/inferenceFreeInstall.test.ts reads the install
		// graph and fails if any of these names appears in it. It has to spell
		// them out to look for them, and it runs in the vscode-extension lane
		// where this Go gate does not, so the two are complementary rather
		// than redundant.
		"editors/vscode/test/inferenceFreeInstall.test.ts": true,
	}

	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	type hit struct {
		path string
		line int
		name string
		text string
	}
	var hits []hit

	for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path = strings.TrimSpace(path)
		if path == "" || allowed[path] {
			continue
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go", ".memql", ".ts", ".tsx", ".js", ".yaml", ".yml", ".json", ".md", ".sh", ".tmpl", ".env":
		default:
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			// A path git knows about that cannot be read is a broken checkout,
			// not a passing gate.
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, name := range banned {
				if strings.Contains(line, name) {
					hits = append(hits, hit{path: path, line: i + 1, name: name, text: strings.TrimSpace(line)})
				}
			}
		}
	}

	if len(hits) == 0 {
		return
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].path != hits[j].path {
			return hits[i].path < hits[j].path
		}
		return hits[i].line < hits[j].line
	})

	var b strings.Builder
	b.WriteString("a manually entered vendor API key has a way back into the product.\n\n")
	b.WriteString("Every cloud vendor MemQL calls is reached by workload identity federation " +
		"(docs/public/operate/auth/{anthropic,openai}-federation.md). There is no key field, " +
		"no key variable, no key step and no key sentence, and this gate is what keeps it that way.\n\n")
	for _, h := range hits {
		text := h.text
		if len(text) > 120 {
			text = text[:120] + "..."
		}
		b.WriteString("  " + h.path + ":" + itoa(h.line) + "  " + h.name + "\n      " + text + "\n")
	}
	b.WriteString("\nIf one of these is a DESIGN RECORD explaining the removal, add its path to " +
		"the allow-list above rather than editing the record.\n")
	t.Fatal(b.String())
}
