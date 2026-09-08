package app

// ai_resolver_wiring_test.go -- the router is INSTALLED on the engine, not
// merely constructed (epic memql#5127, design D2).
//
// # Why this test exists
//
// The AI resolver is a seam: component/memql declares the interface, and
// component/router satisfies it from a different Go module that imports
// component/memql and cannot be imported back. One line in engineAndBus joins
// them. A seam whose setter is never called is GREEN, SILENT AND INERT -- the
// build passes, every package compiles, every unit test in both halves goes on
// proving what it proved before, and the only symptom is at runtime, where
// every model call answers ErrAIResolverUnwired. This tree has been bitten by
// exactly that shape more than once: the healing loop had a complete emitter,
// a mesh routing rule and its own tests, and no non-test caller ever
// constructed the subscriber; the app door was written and registered and had
// no implementation behind it. In both cases the missing thing was a call, and
// nothing in the build could see it was missing.
//
// So the call is asserted, in the file that must contain it.
//
// # Why the source rather than a behavioural check
//
// Building an App far enough to observe HasAIResolver() means a database, a
// provider registry and a loaded DSL tree -- a db-gated test, which skips by
// default and is therefore not what stands between this wiring and its
// absence. The AST walk asks the parser for the call and costs nothing, and it
// is not a grep: it sees the code and not the prose, so the paragraph above
// naming SetAIResolver four times cannot green it.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// engineWiringFile is the file that must carry the install. It is named here
// rather than searched for across app/, because "somewhere in the package"
// would go on passing if the call moved into a build-tagged file that only one
// node type compiles -- which is the failure this test is about, wearing a
// different hat.
const engineWiringFile = "engine.go"

func TestTheRouterIsInstalledAsTheEngineResolver(t *testing.T) {
	if !callsMethodInFile(t, engineWiringFile, "SetAIResolver") {
		t.Fatalf("%s does not call SetAIResolver: the AI resolver seam is declared, satisfied by "+
			"component/router, and installed by nobody. Every re-pointed call site then answers "+
			"memql.ErrAIResolverUnwired at runtime while the whole tree builds and tests green",
			engineWiringFile)
	}
}

// TestTheWiringDetectorSeesACallAndOnlyACall is the negative control. Without
// it, a walk that examined nothing -- a renamed file, a parse failure, a
// matcher that matched everything -- would pass forever and read exactly like
// correct wiring.
func TestTheWiringDetectorSeesACallAndOnlyACall(t *testing.T) {
	const wired = `package example

func wire(a *App) { a.engine.SetAIResolver(a.router) }
`
	if !callsMethodInSource(t, wired, "SetAIResolver") {
		t.Fatal("the detector missed a call that is plainly there")
	}

	// The mirror: the NAME alone must not satisfy it. A doc comment, a string
	// literal and a field selector that is never called all mention the
	// method, and a detector that accepted any of them would report this
	// file's own header as the wiring.
	const unwired = `package example

// SetAIResolver is what this would call if anybody had written the call.
func almost(a *App) {
	_ = "SetAIResolver"
	_ = a.engine.SetAIResolver
}
`
	if callsMethodInSource(t, unwired, "SetAIResolver") {
		t.Fatal("the detector fired on a mention rather than on a call, so it cannot tell wiring from prose")
	}
}

func callsMethodInFile(t *testing.T, path, method string) bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		// A file this walk cannot parse is not evidence that the call is
		// there.
		t.Fatalf("parse %s: %v", path, err)
	}
	return fileCallsMethod(file, method)
}

func callsMethodInSource(t *testing.T, src, method string) bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return fileCallsMethod(file, method)
}

// fileCallsMethod reports whether file contains a CALL whose callee is a
// selector ending in method. A bare selector -- the method value, taken and
// dropped -- is deliberately not a call, because taking a function value and
// never invoking it wires nothing.
func fileCallsMethod(file *ast.File, method string) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == method {
			found = true
			return false
		}
		return true
	})
	return found
}
