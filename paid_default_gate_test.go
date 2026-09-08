package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestNoPaidDefault: paid inference is never what the platform reaches for
// unless somebody said so (epic memql#5137, design D3).
//
// Every concrete provider record in dsl/providers/providers.memql is a paid
// vendor model, and since the vendor keys went (epic memql#5088) none of them
// is reachable from a local cluster at all. The default was therefore a name
// that resolved to nothing, and the fallback was whichever available entry Go's
// map iteration happened to yield first. This gate is what keeps the three ways
// back to that state closed:
//
//  1. A PROMPT PINNED to a federated provider with @defaultProvider. The
//     prompt's @level is its routing now (epic memql#5127); a pin bypasses the
//     rules entirely, so the shipped local-first rule never runs.
//
//  2. THE REGISTRY DECLARING A @default. A default provider is a routing
//     decision made in a data file that no decision record can explain,
//     because nothing decided it.
//
//  3. A FEDERATED PROVIDER RECORD NAMED BY STRING LITERAL IN GO. This is the
//     arm that reads as the most pedantic and is the one that matters. A
//     literal "chat54Mini" in an integration is a paid default wearing an
//     integration's name: it routes around every rule the router applies, it
//     appears in no policy, and the decision record for the call it causes says
//     "explicit pin" with nothing naming who pinned it.
//
// THE LIST OF FEDERATED NAMES IS DERIVED, NEVER WRITTEN DOWN. It is read out of
// dsl/providers/providers.memql at test time: every provider whose @extends
// names a federated base. A hardcoded list would go stale the day a record is
// added and would then pass over the exact name it exists to catch -- and it
// would keep passing, because a ban-list gate guards nothing once the name it
// bans is retired.
func TestNoPaidDefault(t *testing.T) {
	federated := federatedProviderNames(t)
	if len(federated) == 0 {
		t.Fatal("no federated provider records found in dsl/providers/providers.memql: this gate would pass over nothing")
	}

	var failures []string
	failures = append(failures, findFederatedPromptPins(t, federated)...)
	failures = append(failures, findRegistryDefault(t)...)
	failures = append(failures, findFederatedGoLiterals(t, federated)...)

	if len(failures) == 0 {
		return
	}
	sort.Strings(failures)

	var b strings.Builder
	b.WriteString("paid inference has a way back into the default path.\n\n")
	b.WriteString("A call declares a LEVEL, never a model (epic memql#5127), and the shipped rules " +
		"put paid inference last. A pin, a registry default, or a provider name in Go routes around " +
		"all of it -- and the decision record for the resulting call cannot say who chose.\n\n")
	for _, f := range failures {
		b.WriteString("  " + f + "\n")
	}
	b.WriteString("\nTHE FIX is a @level on the prompt, or a rule, or a policy -- never a name. " +
		"See docs/public/operate/ai-routing.md.\n")
	t.Fatal(b.String())
}

// providersFile is the one file that may name a federated provider freely: it
// is where they are declared.
const providersFile = "dsl/providers/providers.memql"

var (
	providerDeclRe = regexp.MustCompile(`^provider\s+([A-Za-z0-9_]+)\s*[{\s]`)
	extendsRe      = regexp.MustCompile(`^@extends\("([^"]+)"\)`)
	baseRe         = regexp.MustCompile(`^@base\b`)
	typeRe         = regexp.MustCompile(`^@type\("([^"]+)"\)`)
	defaultRe      = regexp.MustCompile(`^@default\b`)
	defaultProvRe  = regexp.MustCompile(`@defaultProvider\("([^"]+)"\)`)
)

// federatedBases are the two vendor bases the engine reaches by workload
// identity federation. `fleet` and `app` are the other two bases and are
// deliberately absent: a fleet model runs on a machine the user owns and an app
// session spends a subscription they already pay for, so neither is the paid
// door this gate is about.
var federatedBases = map[string]bool{"openai": true, "anthropic": true}

// federatedProviderNames reads the provider records out of the DSL and returns
// the concrete ones that extend a federated base. Bases themselves are excluded
// -- "openai" is a word, and banning it in Go would fail on every comment about
// the vendor.
func federatedProviderNames(t *testing.T) map[string]bool {
	t.Helper()

	data, err := os.ReadFile(providersFile)
	if err != nil {
		t.Fatalf("read %s: %v", providersFile, err)
	}

	names := map[string]bool{}
	var pendingExtends string
	var pendingBase bool
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case baseRe.MatchString(trimmed):
			pendingBase = true
		case extendsRe.MatchString(trimmed):
			pendingExtends = extendsRe.FindStringSubmatch(trimmed)[1]
		case providerDeclRe.MatchString(trimmed):
			name := providerDeclRe.FindStringSubmatch(trimmed)[1]
			if !pendingBase && federatedBases[pendingExtends] {
				names[name] = true
			}
			pendingExtends, pendingBase = "", false
		}
	}
	return names
}

// findFederatedPromptPins is arm 1.
func findFederatedPromptPins(t *testing.T, federated map[string]bool) []string {
	t.Helper()

	var out []string
	for _, path := range trackedFiles(t) {
		if !strings.HasSuffix(path, ".memql") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			m := defaultProvRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			// A doc comment DISCUSSING the annotation is not a pin. The
			// annotation only binds when it opens the line.
			if !strings.HasPrefix(strings.TrimSpace(line), "@defaultProvider(") {
				continue
			}
			if !federated[m[1]] {
				continue
			}
			out = append(out, path+":"+itoa(i+1)+"  @defaultProvider(\""+m[1]+"\") pins a prompt to a federated provider; give the prompt an @level instead")
		}
	}
	return out
}

// findRegistryDefault is arm 2.
func findRegistryDefault(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(providersFile)
	if err != nil {
		t.Fatalf("read %s: %v", providersFile, err)
	}
	var out []string
	for i, line := range strings.Split(string(data), "\n") {
		if defaultRe.MatchString(strings.TrimSpace(line)) {
			out = append(out, providersFile+":"+itoa(i+1)+"  @default declares a registry default provider; the shipped `default` rule is the default")
		}
	}
	return out
}

// goLiteralAllowlist names the Go files that may carry a federated provider
// name as a string literal.
//
// It is TWO ENTRIES and should stay that way. The registry loader has to name
// the records it loads, and this gate has to name what it is looking for.
// Anything else naming one is the failure this arm exists to catch.
var goLiteralAllowlist = map[string]bool{
	"component/memql/provider_loader.go": true,
	"paid_default_gate_test.go":          true,
}

// findFederatedGoLiterals is arm 3.
func findFederatedGoLiterals(t *testing.T, federated map[string]bool) []string {
	t.Helper()

	// Sorted so the longest names match first and the report names the right
	// one when two records share a prefix (stream54 and stream54Mini).
	names := make([]string, 0, len(federated))
	for n := range federated {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })

	var out []string
	for _, path := range trackedFiles(t) {
		if filepath.Ext(path) != ".go" || goLiteralAllowlist[path] {
			continue
		}
		// A design record is prose; neither is a pin.
		if strings.HasPrefix(path, "docs/") {
			continue
		}
		// TEST FILES ARE EXCLUDED, and the reason is that they are a different
		// kind of use rather than a concession.
		//
		// A test that builds a provider registry has to NAME the records it
		// puts in it -- that is a fixture, and the parser and router tests are
		// full of them legitimately. A production file naming one is a pin: it
		// decides, at a call site, which vendor model answers, and no rule or
		// policy gets a say.
		//
		// Including them would have forced ~40 unrelated tests to rename their
		// fixtures, which teaches exactly the wrong lesson -- the fix a
		// developer reaches for under a gate like that is a renamed string, not
		// a removed default. The hole this leaves is a non-test helper naming a
		// provider, and that is what the allow-list above is for: it has two
		// entries and a third should be argued for, not added.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			// A provider name in a COMMENT is documentation, not a pin -- a
			// struct field documenting `Provider string // e.g. "claudeSonnet"`
			// pins nothing. Strip the comment before matching rather than
			// skipping whole comment lines, because the trailing form is the
			// one this arm kept flagging, and "rewrite the doc example" is the
			// wrong lesson for a gate about defaults to teach.
			code := stripLineComment(line)
			for _, name := range names {
				if !strings.Contains(code, `"`+name+`"`) {
					continue
				}
				out = append(out, path+":"+itoa(i+1)+"  names the federated provider \""+name+"\" as a string literal; resolve through the router at a level instead")
				break
			}
		}
	}
	return out
}

// stripLineComment returns the code half of a Go source line.
//
// It counts quotes rather than parsing, which is enough here and wrong in
// exactly one direction that matters: a `//` inside a string literal (a URL)
// leaves the line intact, so the scan sees MORE than it strictly should rather
// than less. A gate that under-scans passes over the thing it exists to catch;
// one that over-scans reports something a reader can dismiss.
func stripLineComment(line string) string {
	quotes := 0
	for i := 0; i+1 < len(line); i++ {
		switch {
		case line[i] == '"' && (i == 0 || line[i-1] != '\\'):
			quotes++
		case line[i] == '/' && line[i+1] == '/' && quotes%2 == 0:
			return line[:i]
		}
	}
	return line
}

func trackedFiles(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var paths []string
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		t.Fatal("git ls-files listed nothing: this gate would pass over an empty tree")
	}
	return paths
}

// The three fixtures. Each drives one arm against a synthetic input, so a
// refactor that quietly stops an arm from detecting anything fails HERE rather
// than passing silently over the real tree -- which is the failure mode a gate
// like this has, and the reason "TestNoPaidDefault passes" is not on its own
// evidence of anything.

func TestNoPaidDefaultFailsOnAFederatedPin(t *testing.T) {
	federated := map[string]bool{"chat54Mini": true}
	line := `@defaultProvider("chat54Mini")`
	if !defaultProvRe.MatchString(line) {
		t.Fatal("arm 1's detector no longer recognises a @defaultProvider pin")
	}
	m := defaultProvRe.FindStringSubmatch(line)
	if !federated[m[1]] {
		t.Fatal("arm 1 would not flag a pin naming a federated provider")
	}
}

func TestNoPaidDefaultFailsOnARegistryDefault(t *testing.T) {
	if !defaultRe.MatchString("@default") {
		t.Fatal("arm 2's detector no longer recognises a bare @default")
	}
	// @defaultProvider must NOT read as a registry @default -- they are
	// different annotations and conflating them would make arm 2 fire on every
	// prompt pin, which reads as arm 2 working when it is arm 1's finding.
	if defaultRe.MatchString("@defaultProvider(\"x\")") {
		t.Fatal("arm 2 matches @defaultProvider; it must match only a bare @default")
	}
}

func TestNoPaidDefaultFailsOnAGoStringLiteral(t *testing.T) {
	federated := federatedProviderNames(t)
	if len(federated) == 0 {
		t.Fatal("no federated names parsed out of the providers file: arm 3 would scan for nothing")
	}
	// The parse is the half that can rot silently: if the providers file's shape
	// changes, this returns an empty set and arm 3 passes over every file in the
	// tree without matching anything.
	if federated["openai"] || federated["anthropic"] || federated["fleet"] || federated["app"] {
		t.Error("a BASE provider was parsed as a concrete federated record; arm 3 would flag every comment naming the vendor")
	}
	if !federated["chat54Mini"] {
		t.Error("chat54Mini is not in the parsed federated set; if it was deliberately renamed, update this fixture -- otherwise the parse is broken and arm 3 is inert")
	}
}
