// Static guard over the extension publish lane
// (.github/workflows/publish-vscode-extension.yml), memql#5075.
//
// THE DEFECT IT CLOSES was the ABSENCE of this workflow: CI packaged the
// extension on every relevant PR and discarded the artifact, so every
// extension-side fix reached the one machine that could run
// `make vscode-install`. For a local install the extension IS the product --
// it carries the install graph, the capability scripts and the whole panel --
// so "how does a user get a fixed extension" was the same question as "how does
// a user get a fixed installer", and the answer was "clone the repo".
//
// The assertions below are the invariants whose regression would be INVISIBLE
// FROM A GREEN RUN, which is the same standard build_workflows_test.go sets for
// its sibling. Each one has a failure that looks like success.
package release

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func publishExtensionWorkflow(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	p := filepath.Join(filepath.Dir(thisFile), "..", "..", ".github", "workflows", "publish-vscode-extension.yml")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read publish-vscode-extension.yml: %v", err)
	}
	return string(raw)
}

func packageScript(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	p := filepath.Join(filepath.Dir(thisFile), "..", "..", "scripts", "vscode", "package.sh")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read package.sh: %v", err)
	}
	return string(raw)
}

// THE TWO TAG CONVENTIONS, again. Git tags carry the `v` and package versions
// do not, so a release-published run that forwarded `tag_name` unchanged would
// try to publish `v0.3.2` -- which the manifest check below would then refuse,
// making every automatic publish fail for a reason that reads as a version
// mismatch rather than as a missing `#v`.
func TestExtensionPublishStripsTheTagPrefix(t *testing.T) {
	wf := publishExtensionWorkflow(t)
	if !strings.Contains(wf, `version="${TAG#v}"`) {
		t.Error("the release path does not strip the `v` from the tag before using it as a version.\n" +
			"dispatch-engine-images-on-release.yml records the same rule at length: git tags carry the\n" +
			"prefix and package versions do not.")
	}
}

// THE MANIFEST IS WHAT A USER'S EDITOR COMPARES AGAINST. memql#5075's other
// half: `0.3.1` had not moved while the staged scripts inside the archive had,
// so the version string said nothing about what was in it. Deriving the
// published version from the tag alone would let a release ship an archive
// whose manifest still says 0.3.1 -- installed, and reported to the user as a
// version they may already have.
func TestExtensionPublishRefusesAVersionTheManifestDoesNotCarry(t *testing.T) {
	wf := publishExtensionWorkflow(t)
	if !strings.Contains(wf, `require('./editors/vscode/package.json').version`) {
		t.Fatal("the publish lane never reads editors/vscode/package.json, so it cannot tell whether\n" +
			"the version it is publishing is the one the archive claims")
	}
	if !strings.Contains(wf, `if [ "$version" != "$manifest" ]; then`) {
		t.Error("the manifest version is read but not compared; a publish must REFUSE a version the\n" +
			"manifest does not carry, not merely report it")
	}
}

// EACH CHANNEL IS SEPARATELY GATED, AND SAYS SO WHEN IT SKIPS.
//
// The GitHub Release asset needs no account, so it works the day this lands;
// the Marketplace and Open VSX steps need tokens that may not exist yet. A
// skip is fine. A SILENT skip is not: it is how a green run comes to mean
// "shipped" when nothing was, which is exactly the class of confident-and-wrong
// signal this repo keeps refusing.
func TestEveryPublishChannelIsGatedAndAnnouncesASkip(t *testing.T) {
	wf := publishExtensionWorkflow(t)
	for _, secret := range []string{"VSCE_PAT", "OVSX_PAT"} {
		if !strings.Contains(wf, `if [ -z "${`+secret+`:-}" ]; then`) {
			t.Errorf("%s is not checked before its publish step, so the lane fails on a missing token\n"+
				"instead of skipping that channel", secret)
		}
		if !strings.Contains(wf, "::warning::"+secret+" is not set") {
			t.Errorf("a missing %s is skipped without a warning; a channel that publishes nothing and\n"+
				"says nothing is indistinguishable from one that published", secret)
		}
	}
	// ...and Open VSX is deliberately in the set, because Cursor and VSCodium
	// read it and both are installed on a developer machine here (memql#5075).
	if !strings.Contains(wf, "ovsx publish") {
		t.Error("no Open VSX step at all; that was a decision, and removing it is a different one")
	}
}

// ONE PACKAGE PER PLATFORM, AND EACH MUST DECLARE ITS TARGET.
//
// The extension bundles the offline `memql-lsp` binary, so a .vsix runs on
// exactly one platform. Without `vsce package --target`, all four archives are
// identical "universal" packages carrying different binaries: publishing them
// under one version is the same version published four times, whichever lands
// last is what everyone gets, and three quarters of users receive a binary they
// cannot execute. Nothing goes red.
func TestExtensionPackagesDeclareTheirPlatform(t *testing.T) {
	wf := publishExtensionWorkflow(t)
	if !strings.Contains(wf, `--target="$TARGET"`) {
		t.Fatal("the packaging step does not pass --target, so every platform's archive is a universal\n" +
			"package and publishing more than one under a single version overwrites the others")
	}
	if !strings.Contains(packageScript(t), `args+=(--target "$VSCE_TARGET")`) {
		t.Error("scripts/vscode/package.sh accepts no --target, so the workflow's flag reaches nothing")
	}
	for _, target := range []string{"linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64"} {
		if !strings.Contains(wf, "target: "+target) {
			t.Errorf("the publish matrix does not build %s; development happens on macOS and Linux on\n"+
				"both architectures (CLAUDE.md), so dropping one silently strands those users", target)
		}
	}
}

// THE ARCHIVE MUST CARRY WHAT IT CLAIMS TO, and both of these fail silently.
// An unstamped package cannot report skew (memql#5076) and a package missing
// the staged tree installs fine and then cannot begin an install (memql#3487).
func TestExtensionPublishVerifiesTheArchiveContents(t *testing.T) {
	wf := publishExtensionWorkflow(t)
	for _, required := range []string{
		"extension/staged/buildinfo.json",
		"extension/staged/scripts/install/graph/install.json",
		"extension/staged/scripts/install/graph/install-main.json",
	} {
		if !strings.Contains(wf, required) {
			t.Errorf("the lane does not check that the archive contains %s", required)
		}
	}
	if !strings.Contains(wf, "the package records no build commit") {
		t.Error("an EMPTY build commit passes a presence check; package.sh writes \"\" when git cannot\n" +
			"read the checkout, and a package stamped with nothing cannot report skew")
	}
}
