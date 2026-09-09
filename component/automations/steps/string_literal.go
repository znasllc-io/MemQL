package steps

import (
	"fmt"

	langparser "github.com/znasllc-io/memql/component/language/parser"
)

// decodeArgStringLiteral uses the DSL's decoder, including raw multiline
// strings and its Unicode escape rules. Stripping quotes leaves source escapes
// in the value; JSON/Go unquoters accept a different set of literals.
func decodeArgStringLiteral(source string) (string, error) {
	lexer := langparser.NewLexer(source)
	literal, err := lexer.NextToken()
	if err != nil {
		return "", err
	}
	if literal.Type != langparser.TokenString {
		return "", fmt.Errorf("expected a MemQL string literal")
	}
	next, err := lexer.NextToken()
	if err != nil {
		return "", err
	}
	if next.Type != langparser.TokenEOF {
		return "", fmt.Errorf("expected exactly one MemQL string literal")
	}
	return literal.Literal, nil
}
