// Static guard: the extension publish lane cannot drift into publishing
// something other than what it built (znasllc-io/memql#5075).
//
// # Why this file exists
//
// A published version is IMMUTABLE. The Marketplace and Open VSX both refuse to
// replace one, so every mistake this lane can make is a mistake that cannot be
// taken back -- and unlike every other workflow in this repository, there is no
// re-run that fixes it. That asymmetry is what earns a gate over a workflow that
// no pull request executes.
//
// # What it asserts, and what each one is protecting
//
//  1. The tag pattern and the version check agree on one prefix. They are
//     written in two languages in one file -- a glob in `on.push.tags` and a
//     shell `${GITHUB_REF_NAME#...}` -- and renaming one is invisible to the
//     other. The failure is a tag that triggers a run whose version check then
//     strips nothing and compares a whole tag name against a version.
//
//  2. Every matrix row's `target` is exactly what its `goos`/`goarch` produce.
//     `vsce package --target` MARKS an archive; it does not build one and does
//     not check the binary inside. So a row saying `darwin-arm64` while building
//     `linux/amd64` publishes a Linux binary to every Mac, and nothing at
//     package time notices.
//
//  3. Both registries publish the SAME path the package step wrote. Packaging
//     per registry is the shape a later edit naturally drifts into, and two
//     archives built from one commit can still differ -- leaving the registries
//     serving different bytes under one version.
//
//  4. The dry run guards EVERY publish step. A dry run that still published to
//     one registry would be the single worst outcome this lane has, because it
//     spends the version number while reporting that it did nothing.
package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The file under test, and the tag prefix it is expected to own.
const (
	publishWorkflowFile = "publish-vscode-extension.yml"
	publishTagPrefix    = "memql-vscode-v"
)

// Local helpers rather than shared ones, for the reason workflow_gate_test.go
// records at length: these files land as independent tasks, and a shared helper
// would turn that independence into a compile-level conflict.
func publishRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func publishWorkflow(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join(publishRepoRoot(t), ".github", "workflows", publishWorkflowFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var wf map[string]any
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return wf
}

// steps of the named job, each as a map.
func publishSteps(t *testing.T, wf map[string]any, job string) []map[string]any {
	t.Helper()
	jobs, ok := wf["jobs"].(map[string]any)
	if !ok {
		t.Fatal("workflow has no jobs mapping")
	}
	spec, ok := jobs[job].(map[string]any)
	if !ok {
		t.Fatalf("workflow has no %q job", job)
	}
	raw, ok := spec["steps"].([]any)
	if !ok {
		t.Fatalf("job %q has no steps", job)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, s := range raw {
		if m, ok := s.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stepText(step map[string]any, key string) string {
	if v, ok := step[key].(string); ok {
		return v
	}
	return ""
}

// --- 1. the tag pattern and the version check ------------------------------

func TestPublishTagPatternAndVersionCheckAgree(t *testing.T) {
	wf := publishWorkflow(t)

	// `on` is a YAML 1.1 boolean and some parsers key it as `true`; yaml.v3
	// follows the 1.2 core schema and keeps it a string. Accept either so this
	// gate does not depend on which one is in the module graph.
	on, ok := wf["on"].(map[string]any)
	if !ok {
		if on, ok = wf["true"].(map[string]any); !ok {
			t.Fatalf("workflow has no trigger mapping; top-level keys: %v", mapKeys(wf))
		}
	}
	push, ok := on["push"].(map[string]any)
	if !ok {
		t.Fatal("workflow does not trigger on a tag push")
	}
	tags, ok := push["tags"].([]any)
	if !ok || len(tags) == 0 {
		t.Fatal("on.push declares no tag patterns")
	}
	pattern, _ := tags[0].(string)
	if !strings.HasPrefix(pattern, publishTagPrefix) {
		t.Errorf(
			"the tag pattern %q does not start with %q.\n"+
				"The version check strips that prefix with a shell expansion; a pattern that\n"+
				"does not carry it triggers runs the check cannot parse (memql#5075).",
			pattern, publishTagPrefix,
		)
	}

	env, _ := wf["env"].(map[string]any)
	if got, _ := env["TAG_PREFIX"].(string); got != publishTagPrefix {
		t.Errorf("env.TAG_PREFIX is %q, want %q -- the glob and the strip must name one prefix", got, publishTagPrefix)
	}
}

// --- 2. the target marks what the row actually builds ----------------------

// nodePlatform mirrors scripts/vscode/package.sh's function of the same name:
// Go's GOOS to Node's process.platform, which is what the extension's
// resolveServerPath builds its lookup directory from.
func nodePlatform(goos string) string {
	if goos == "windows" {
		return "win32"
	}
	return goos
}

// nodeArch mirrors the same script's GOARCH -> process.arch mapping.
func nodeArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "arm64", nil
	case "386":
		return "ia32", nil
	}
	return "", fmt.Errorf("no process.arch for GOARCH %q", goarch)
}

func TestPublishMatrixTargetsMatchWhatTheyBuild(t *testing.T) {
	wf := publishWorkflow(t)
	jobs, _ := wf["jobs"].(map[string]any)
	pub, ok := jobs["publish"].(map[string]any)
	if !ok {
		t.Fatal("workflow has no publish job")
	}
	strategy, _ := pub["strategy"].(map[string]any)
	matrix, _ := strategy["matrix"].(map[string]any)
	include, ok := matrix["include"].([]any)
	if !ok || len(include) == 0 {
		t.Fatal("the publish job has no matrix.include rows")
	}

	seen := map[string]bool{}
	for _, row := range include {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		target, _ := m["target"].(string)
		goos, _ := m["goos"].(string)
		goarch, _ := m["goarch"].(string)

		arch, err := nodeArch(goarch)
		if err != nil {
			t.Errorf("row %q: %v -- package.sh could not name a bundle directory for it", target, err)
			continue
		}
		want := nodePlatform(goos) + "-" + arch
		if target != want {
			t.Errorf(
				"row builds %s/%s but is published as target %q; it should be %q.\n"+
					"`vsce package --target` only LABELS the archive -- it does not build it and\n"+
					"does not check the binary inside, so this row would ship a %s binary to every\n"+
					"%s user and nothing at package time would notice (memql#5075).",
				goos, goarch, target, want, goos, strings.SplitN(target, "-", 2)[0],
			)
		}
		if seen[target] {
			t.Errorf("target %q appears twice; the second publish would be refused as a duplicate", target)
		}
		seen[target] = true
	}
}

// --- 3. one archive, published twice ---------------------------------------

func TestPublishSendsBothRegistriesTheSameArchive(t *testing.T) {
	steps := publishSteps(t, publishWorkflow(t), "publish")

	var packaged, marketplace, openvsx string
	for _, s := range steps {
		run := stepText(s, "run")
		name := stepText(s, "name")
		switch {
		case strings.Contains(run, "scripts/vscode/package.sh"):
			packaged = run
		case strings.Contains(run, "vsce") && strings.Contains(run, "publish"):
			marketplace = run
		case strings.Contains(run, "ovsx") && strings.Contains(run, "publish"):
			openvsx = run
		}
		_ = name
	}
	if packaged == "" || marketplace == "" || openvsx == "" {
		t.Fatalf("expected a package step and two publish steps; got package=%v marketplace=%v openvsx=%v",
			packaged != "", marketplace != "", openvsx != "")
	}

	// The path the package step wrote, and the path each publish step reads.
	const want = `memql-${{ matrix.target }}.vsix`
	for label, run := range map[string]string{"package": packaged, "marketplace": marketplace, "open vsx": openvsx} {
		if !strings.Contains(run, want) {
			t.Errorf(
				"the %s step does not name %s.\n"+
					"All three must name ONE archive: packaging per registry lets two builds of\n"+
					"one commit differ, and then the registries serve different bytes under one\n"+
					"version with nothing to compare (memql#5075).",
				label, want,
			)
		}
	}
	// Neither publish step may package anything of its own.
	for label, run := range map[string]string{"marketplace": marketplace, "open vsx": openvsx} {
		if strings.Contains(run, "package.sh") || strings.Contains(run, "vsce package") {
			t.Errorf("the %s step packages its own archive; it must publish the one already built", label)
		}
	}
}

// --- 4. the dry run guards every publish -----------------------------------

func TestPublishDryRunGuardsEveryPublishStep(t *testing.T) {
	steps := publishSteps(t, publishWorkflow(t), "publish")

	found := 0
	for _, s := range steps {
		run := stepText(s, "run")
		if !strings.Contains(run, "publish") {
			continue
		}
		if !strings.Contains(run, "vsce") && !strings.Contains(run, "ovsx") {
			continue
		}
		found++
		cond := stepText(s, "if")
		if !strings.Contains(cond, "dryRun") {
			t.Errorf(
				"a publish step runs unconditionally:\n  %s\n"+
					"Every one must be guarded by dryRun. A dry run that still published to one\n"+
					"registry is the worst outcome this lane has: it spends a version number --\n"+
					"which cannot be reused -- while reporting that it did nothing (memql#5075).",
				strings.TrimSpace(run),
			)
		}
	}
	if found < 2 {
		t.Errorf("found %d publish steps, want 2 (Marketplace and Open VSX)", found)
	}
}
