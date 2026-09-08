package parser

import (
	"fmt"

	"github.com/znasllc-io/memql/component/language/ast"
)

// ParseRuleDecl tokenises a single `rule NAME { }` slice (with its leading
// attribute set) and returns the typed *ast.RuleDecl.
//
// The caller -- the unified rule loader -- has already sliced one rule out of a
// multi-construct `.memql` file via ExtractKeywordSlices, so the input here is
// expected to contain exactly one rule declaration. Mirrors ParsePolicyDecl,
// including the doc-comment attachment: the /// block above the declaration is
// the description and wins over @description, and attaching it here is what
// makes that true on the loader's path as well as on a whole-file parse.
//
// Returns an error when the slice's syntax is malformed or doesn't produce a
// rule node.
func ParseRuleDecl(source string) (*ast.RuleDecl, error) {
	lexer := NewLexer(source)
	tokens, err := lexer.Tokenize()
	if err != nil {
		return nil, fmt.Errorf("tokenise: %w", err)
	}
	p := NewParser(tokens)
	p.SetDocComments(lexer.DocComments())
	defFirstLine := p.current.Line
	def, err := p.parseDefinition()
	if err != nil {
		return nil, err
	}
	attachDocComment(def, p.takeDocFor(defFirstLine))
	decl, ok := def.(*ast.RuleDecl)
	if !ok {
		return nil, fmt.Errorf("expected rule declaration, got %T", def)
	}
	return decl, nil
}
