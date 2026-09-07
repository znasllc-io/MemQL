package envregistry

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const osModulesPath = "../../clients/os/src/system/modules.ts"

var osModulesTuple = regexp.MustCompile(`(?m)^\s*export\s+const\s+READINESS_MODULES\s*=\s*\[([^\]]*)\]\s*as\s+const\s*;`)
var quotedModuleId = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9]*)"`)

// The shell's module ids are a copy of the manifest's, and this is the gate
// that keeps them one list.
//
// BOTH DIRECTIONS matter and they fail differently. A module the engine
// declares that the shell does not know cannot be required by any app, so the
// feature it gates silently never gates. A module the shell names that the
// engine never declares is worse: its verdict is `unreported` forever, so an
// app requiring it shows the setup surface permanently, pointing at a module
// nobody can configure because it does not exist.
func TestOSModuleIdsMatchTheManifest(t *testing.T) {
	raw, err := os.ReadFile(osModulesPath)
	if err != nil {
		t.Fatalf("the shell's module list is unreadable at %s: %v", osModulesPath, err)
	}
	m := osModulesTuple.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s does not export READINESS_MODULES as a one-line `as const` tuple; this gate parses it by regexp rather than executing TypeScript, so the shape is load-bearing", osModulesPath)
	}
	var shell []string
	for _, q := range quotedModuleId.FindAllSubmatch(m[1], -1) {
		shell = append(shell, string(q[1]))
	}
	if len(shell) == 0 {
		t.Fatalf("%s exports READINESS_MODULES with no ids in it", osModulesPath)
	}

	manifest, err := LoadManifestFromBytes(embeddedManifest, "embedded")
	if err != nil {
		t.Fatal(err)
	}
	var engine []string
	for _, mod := range manifest.Modules {
		engine = append(engine, mod.Name)
	}
	if len(engine) == 0 {
		t.Fatal("the manifest declares no modules; this gate would compare against nothing")
	}

	sort.Strings(shell)
	sort.Strings(engine)
	if strings.Join(shell, ",") != strings.Join(engine, ",") {
		t.Fatalf("module ids disagree\nshell:  %v\nengine: %v\nThe shell's list is %s; the engine's is the `modules:` block of scripts/secrets/manifest.yaml (then `make env-registry-sync`).", shell, engine, osModulesPath)
	}
}
