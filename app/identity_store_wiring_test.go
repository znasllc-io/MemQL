package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// The handler tests supply their own Store. This guard covers the actual
// identity-tagged bootstrap, which previously omitted DirectDB even though
// the Store documentation claimed it was wired. Runtime endpoint selection
// is separately exercised by TestDirectDBGetter_* and the replica DB tests.
func TestIdentityBootstrapWiresSharedChallengeStorage(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "integrations_identity.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	stores, wired := 0, 0
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typ, ok := literal.Type.(*ast.SelectorExpr)
		if !ok || typ.Sel.Name != "Store" {
			return true
		}
		pkg, ok := typ.X.(*ast.Ident)
		if !ok || pkg.Name != "identity" {
			return true
		}
		stores++
		for _, element := range literal.Elts {
			field, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := field.Key.(*ast.Ident)
			if !ok || key.Name != "DirectDB" {
				continue
			}
			if value, nilValue := field.Value.(*ast.Ident); nilValue && value.Name == "nil" {
				continue
			}
			wired++
		}
		return true
	})
	if stores == 0 || wired != stores {
		t.Fatalf("identity bootstrap wires DirectDB on %d of %d stores; shared passkey challenges cannot work without it", wired, stores)
	}
}
