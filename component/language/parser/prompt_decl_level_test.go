package parser

import (
	"strings"
	"testing"
)

// prompt_decl_level_test.go covers @level on a prompt (epic memql#5127) -- the
// annotation that says how much intelligence the call needs, and the reason a
// prompt names no model.

// TestParsePromptDecl_AcceptsEveryLevel walks the closed four. A level missing
// from this table is one the corpus cannot declare, which would present as a
// prompt that refuses to load for no reason an author can see.
func TestParsePromptDecl_AcceptsEveryLevel(t *testing.T) {
	for _, level := range []string{"fast", "strong", "reasoning", "embeddings"} {
		source := "@level(\"" + level + "\")\n@templateFile(\"x.tmpl\")\nprompt somePrompt {\n  space object @required\n}"
		got, err := ParsePromptDecl(source)
		if err != nil {
			t.Fatalf("ParsePromptDecl with @level(%q): %v", level, err)
		}
		if got.Level != level {
			t.Errorf("Level = %q, want %q -- the parser must STORE it, not merely accept it: a "+
				"routing rule branches on this field, and an empty one matches nothing", got.Level, level)
		}
	}
}

// TestParsePromptDecl_RefusesUnknownLevel pins the closed set, and that the
// message names all four.
//
// Naming them matters more here than usual: the failure an author is most
// likely to produce is a plausible synonym (`cheap`, `smart`, `mini`), and a
// refusal saying only "unknown level" leaves them guessing at a vocabulary that
// is deliberately small enough to print.
func TestParsePromptDecl_RefusesUnknownLevel(t *testing.T) {
	for _, bad := range []string{"smart", "cheap", "Fast", ""} {
		source := "@level(\"" + bad + "\")\n@templateFile(\"x.tmpl\")\nprompt somePrompt { }"
		_, err := ParsePromptDecl(source)
		if err == nil {
			t.Fatalf("ParsePromptDecl accepted @level(%q); the set is closed", bad)
		}
		for _, want := range []string{"fast", "strong", "reasoning", "embeddings"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal of @level(%q) does not name %q: %v", bad, want, err)
			}
		}
	}
}

// TestParsePromptDecl_AbsentLevelIsEmptyNotADefault.
//
// The parser does NOT require @level, deliberately: the corpus does not carry
// one yet, and requiring it here would refuse every prompt in the tree the
// moment the annotation landed. The requirement belongs to the loader, which
// can put a prompt with no level on the LoadReport as a skip strict boot
// refuses -- one place, with MEMQL_DSL_ALLOW_SKIPS as the break-glass, and the
// same rule applied to a bundle mounted at MEMQL_DSL_PATH.
//
// What the parser must NOT do is substitute a default. A guessed level is a
// silent routing decision, and it would be indistinguishable from one an author
// wrote.
func TestParsePromptDecl_AbsentLevelIsEmptyNotADefault(t *testing.T) {
	got, err := ParsePromptDecl("@templateFile(\"x.tmpl\")\nprompt noLevel { }")
	if err != nil {
		t.Fatalf("ParsePromptDecl: %v", err)
	}
	if got.Level != "" {
		t.Errorf("Level = %q, want empty -- an absent @level must not be filled in", got.Level)
	}
}
