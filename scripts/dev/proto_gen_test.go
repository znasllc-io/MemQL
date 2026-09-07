// Contract gate for scripts/dev/proto-gen.sh (znasllc-io/memql#2774, #5116).
//
// Every rule here encodes a failure that actually happened, so each assertion
// is a regression net rather than style policing. ("Both rules" said two, and
// there have been more than two since #2774 -- a count in prose is one more
// thing to drift.)
package dev

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

const protoGenScript = "proto-gen.sh"

func protoGenSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(protoGenScript)
	if err != nil {
		t.Fatalf("read %s: %v", protoGenScript, err)
	}
	return string(raw)
}

// TestProtoGen_CheckDoesNotRestoreFromGit is the data-loss net.
//
// `--check` regenerates into the working tree, then puts it back. It used to
// put it back with `git checkout -- <gen paths>`, which restores to HEAD --
// not to what was there before. Running `make proto-gen` and then
// `make proto-gen-check` before committing therefore DELETED the regeneration,
// while the script reported "no drift" and exited 0. The next build failed on
// undefined symbols with no obvious link to the command that caused it.
//
// The restore must come from a backup taken before regenerating, so the check
// leaves the tree exactly as it found it -- committed or not.
func TestProtoGen_CheckDoesNotRestoreFromGit(t *testing.T) {
	src := protoGenSource(t)

	restoreFromGit := regexp.MustCompile(`git checkout\s+--\s+"\$\{GEN_PATHS`)
	if restoreFromGit.MatchString(src) {
		t.Error("proto-gen.sh restores the generated tree with `git checkout -- ${GEN_PATHS[@]}`; " +
			"that resets to HEAD and silently discards an uncommitted regeneration (#2774). " +
			"Restore from a backup taken before regenerating instead.")
	}
	if !strings.Contains(src, "backup") {
		t.Error("proto-gen.sh --check must back the generated trees up before regenerating " +
			"so it can restore exactly what it found")
	}
}

// TestProtoGen_ProtocIsPinned is the churn net.
//
// The plugins were pinned but protoc was not, on the reasoning that the
// generated body is plugin-determined. True -- but the `// protoc vX.Y.Z`
// header comment is not, so a differing protoc rewrote that line in all eight
// generated files. A one-message proto change then produced an eight-file
// diff, and stamp noise was indistinguishable at a glance from a real
// regeneration.
func TestProtoGen_ProtocIsPinned(t *testing.T) {
	src := protoGenSource(t)

	pin := regexp.MustCompile(`readonly PROTOC_VERSION="[0-9]+\.[0-9]+"`)
	if !pin.MatchString(src) {
		t.Error("proto-gen.sh must pin PROTOC_VERSION -- an unpinned protoc rewrites the " +
			"version stamp in every generated file (#2774)")
	}
	if !strings.Contains(src, "resolve_protoc") {
		t.Error("proto-gen.sh must provision the pinned protoc itself (bin/tools/, mirroring " +
			"scripts/identity/build-css.sh) rather than trusting whatever is on PATH")
	}
}

// TestProtoGen_FallsBackToSystemProtoc pins the availability tradeoff.
//
// Pinning protoc introduced a network dependency on GitHub releases. An
// outage there must not block a proto change, so the script degrades to a
// system protoc rather than failing outright. That fallback is safe for the
// GATE specifically because --check diffs with the version stamp ignored, so a
// differing protoc still produces a correct drift verdict -- the only cost is
// cosmetic stamp churn on a plain regenerate, which is why the fallback warns
// loudly instead of degrading silently.
func TestProtoGen_FallsBackToSystemProtoc(t *testing.T) {
	src := protoGenSource(t)

	if !strings.Contains(src, "command -v protoc") {
		t.Error("proto-gen.sh must fall back to a system protoc when the pinned download " +
			"fails; a releases outage should not block the proto lane (#2774)")
	}
	if !strings.Contains(src, "WARNING") {
		t.Error("the protoc fallback must warn -- silently generating with an unpinned " +
			"protoc reintroduces the stamp churn the pin exists to prevent")
	}
	// The stamp-ignore is what makes the fallback safe for the gate; losing it
	// would turn a fallback into false drift.
	if !strings.Contains(src, "STAMP_IGNORE") {
		t.Error("the drift diff must keep ignoring the protoc version stamp, or a fallback " +
			"protoc would report false drift")
	}
}

// TestProtoGen_ScriptIsValidBash keeps a syntax error from reaching CI, where
// the failure surfaces as a confusing generation error rather than a parse
// error.
func TestProtoGen_ScriptIsValidBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	out, err := exec.Command("bash", "-n", protoGenScript).CombinedOutput()
	if err != nil {
		t.Errorf("bash -n %s failed: %v\n%s", protoGenScript, err, out)
	}
}

// TestProtoGen_PluginToolchainIsPinned is the reproducibility net (memql#5116).
//
// The two plugin version pins read as though they fix the output. They do not:
// protoc-gen-go runs its result through go/format, and go/doc/comment's
// renderer changed between Go 1.26 and 1.27 -- an indented comment block is
// `//<TAB>` with its blank `//` lines kept under one and `//<3 spaces>` with
// them dropped under the other, from the same .proto and the same plugin
// version. So the Go toolchain that COMPILES the plugin is a third pin, and it
// was missing.
//
// What made that expensive is how it presented. CI cached bin/tools -- protoc
// AND the two `go install`-ed plugins -- keyed on this script's hash alone, so
// the generated bytes depended on when that cache was last cold. A branch that
// regenerated on a developer's newer Go was green locally (the check
// regenerates and diffs against what you just committed, so it agrees with
// itself) and red in CI, with a diff full of comment indentation in files the
// branch never touched. Nothing in the failure named Go, the cache, or the
// plugin, and this script's own header said the body was plugin-determined.
func TestProtoGen_PluginToolchainIsPinned(t *testing.T) {
	src := protoGenSource(t)

	pin := regexp.MustCompile(`readonly PROTOC_PLUGIN_GOTOOLCHAIN="go[0-9]+\.[0-9]+(\.[0-9]+)?"`)
	if !pin.MatchString(src) {
		t.Error("proto-gen.sh must pin PROTOC_PLUGIN_GOTOOLCHAIN -- the Go toolchain that " +
			"compiles protoc-gen-go decides the comment rendering in every generated " +
			".pb.go, so leaving it unpinned makes the committed bytes depend on whoever " +
			"regenerated last (memql#5116)")
	}

	// Every plugin build must USE the pin. A pin that is declared and then not
	// applied to one of the two installs is the same defect with a constant
	// added: the tree still depends on the ambient toolchain, and now the
	// script looks like it does not.
	for i, line := range strings.Split(src, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "#") || !strings.Contains(code, "go install") {
			continue
		}
		if !strings.Contains(code, `GOTOOLCHAIN="${PROTOC_PLUGIN_GOTOOLCHAIN}"`) {
			t.Errorf("proto-gen.sh:%d installs a plugin without the pinned toolchain:\n  %s\n"+
				"Every `go install` here must be prefixed with "+
				"GOTOOLCHAIN=\"${PROTOC_PLUGIN_GOTOOLCHAIN}\" (memql#5116).", i+1, code)
		}
	}

	// The cached plugin directory is keyed on the pins, so that bumping one
	// lands in a NEW directory rather than silently reusing binaries built
	// under the old value -- the property the two version pins already had and
	// the toolchain did not. CI caches bin/tools wholesale, so without this a
	// cache restored from before a bump is indistinguishable from a correct one.
	if !strings.Contains(src, `protoc-plugins-${PROTOC_GEN_GO_VERSION}-${PROTOC_GEN_GO_GRPC_VERSION}-${PROTOC_PLUGIN_GOTOOLCHAIN}`) {
		t.Error("the cached plugin directory must be keyed on PROTOC_PLUGIN_GOTOOLCHAIN as " +
			"well as the two plugin versions, or a bump silently reuses the binaries " +
			"built under the previous pin (memql#5116)")
	}
}
