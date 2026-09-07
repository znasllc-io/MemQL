package dslconformance

// THE SHIPPED POLICIES TRY THE DOORS IN COST ORDER (epic memql#5096, task
// memql#5101, design D4).
//
// This gate replaces the one memql#4676 wrote, and the property it checks
// changed with the design rather than being relaxed. That gate said: a seeded
// local-first policy must author NO cloud fallback, because a chain reaching
// the cloud starts billing the moment a laptop closes -- silently, since
// nothing about a working reply says which vendor produced it.
//
// It was the right rule when the only alternative to a laptop was a metered
// key. There is now a middle step that costs nothing extra -- a Claude Code or
// Codex subscription the person already pays for -- and the federation hop,
// the one that does cost money, asks the cost ceiling before it is taken
// (component/router's doors_test.go). So the default falls through, and what
// this gate protects is the ORDER: local, then an app, then anybody's money.
//
// IT IS A CORPUS SCAN rather than an engine assertion, for the reason the
// previous version gave about itself: the property is about what the .memql
// files SAY, and the edit worth catching is an author moving a vendor entry to
// the front of a shipped chain "so it does not park". The failure message
// therefore names the alternative -- an explicitly authored policy for that
// purpose -- rather than only saying no.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// shippedPolicies is every policy this repository seeds. A policy added here
// opts into the ordering gate; a policy added to the .memql file and NOT here
// is caught by TestEveryShippedPolicyIsCovered below, so the list cannot
// silently fall behind the tree.
var shippedPolicies = []string{
	"balancedChat",
	"cheapestCapable",
	"fastCoding",
	"strongReasoning",
	"backgroundExecution",
	"backgroundEscalation",
}

// toolCallingLanes are the shipped policies whose turns carry TOOLS. They
// deliberately omit the `app:*` step: on a tool turn MemQL is driving, and an
// app door is an agent that drives itself -- it reaches MemQL's tools through
// MCP, in the other direction (design D3). An `app:*` entry there would be one
// the router skips on every call, which reads to a later author like a door
// that is always shut.
var toolCallingLanes = map[string]bool{
	"backgroundExecution":  true,
	"backgroundEscalation": true,
}

const (
	fleetWildcardRef = "fleet:*"
	appWildcardRef   = "app:*"
	fleetRefPrefix   = "fleet:"
	appRefPrefix     = "app:"
)

var (
	policyDeclRe = regexp.MustCompile(`(?m)^policy\s+([A-Za-z0-9_]+)\s*\{`)
	annotationRe = regexp.MustCompile(`^@(primary|fallback)\("([^"]*)"\)`)
)

// policyChains returns each policy's provider chain in try order: the
// @primary first, then each @fallback.
//
// The annotations are collected by walking BACKWARD from the declaration and
// then REVERSED, because the backward walk yields them bottom-up and a chain
// read in the wrong order would make every assertion below check the opposite
// of what it says. Stopping at the first non-annotation, non-comment line is
// what keeps one policy's annotations from being read as another's.
func policyChains(t *testing.T) map[string][]string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "dsl", "policies", "policies.memql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")

	out := map[string][]string{}
	for i, line := range lines {
		m := policyDeclRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var reversed []string
		for j := i - 1; j >= 0; j-- {
			trimmed := strings.TrimSpace(lines[j])
			if strings.HasPrefix(trimmed, "@") {
				if a := annotationRe.FindStringSubmatch(trimmed); a != nil {
					reversed = append(reversed, a[2])
				}
				continue
			}
			if trimmed == "" || strings.HasPrefix(trimmed, "///") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			break
		}
		chain := make([]string, 0, len(reversed))
		for k := len(reversed) - 1; k >= 0; k-- {
			chain = append(chain, reversed[k])
		}
		out[m[1]] = chain
	}
	return out
}

func TestEveryShippedPolicyStartsAtTheCheapestDoor(t *testing.T) {
	chains := policyChains(t)
	if len(chains) == 0 {
		t.Fatal("no policies parsed -- a gate over nothing passes for the wrong reason")
	}

	for _, name := range shippedPolicies {
		chain, ok := chains[name]
		if !ok {
			t.Errorf("shipped policy %q is not declared in dsl/policies/policies.memql. "+
				"If it was renamed, rename it in shippedPolicies too -- an entry that resolves "+
				"to nothing is a gate that passes because it found nothing to check.", name)
			continue
		}
		if len(chain) == 0 {
			t.Errorf("policy %q declares no @primary at all", name)
			continue
		}

		if chain[0] != fleetWildcardRef {
			t.Errorf("policy %q has @primary(%q). Every shipped policy starts at %q: the local "+
				"door costs electricity, the app door costs a subscription the person already "+
				"pays for, and only the vendor entries cost money (design D4).\n\n"+
				"If this purpose genuinely must reach a vendor first, that is a legitimate "+
				"operator decision -- but it belongs in an explicitly authored policy for that "+
				"purpose, not in the shipped default nobody chose.",
				name, chain[0], fleetWildcardRef)
			continue
		}

		if toolCallingLanes[name] {
			if policyNames(chain, appWildcardRef) {
				t.Errorf("policy %q is a tool-calling lane and must NOT name %q: an app door does "+
					"not serve tool turns (design D3), so the entry is one the router skips on "+
					"every call -- which reads to a later author like a door that is always shut.",
					name, appWildcardRef)
			}
		} else if len(chain) < 2 || chain[1] != appWildcardRef {
			t.Errorf("policy %q must try %q immediately after the local door; chain is %v. "+
				"A subscription the person already pays for sits between their own hardware "+
				"and anybody's money.", name, appWildcardRef, chain)
		}

		// NO LOCAL ENTRY AFTER A VENDOR ONE. A chain that goes cloud, then
		// local, spends money it did not have to and then offers the free
		// option -- which is the ordering bug this gate exists to catch, and
		// the one an author makes by appending rather than inserting.
		sawVendor := false
		for _, entry := range chain {
			local := strings.HasPrefix(entry, fleetRefPrefix) || strings.HasPrefix(entry, appRefPrefix)
			if !local {
				sawVendor = true
				continue
			}
			if sawVendor {
				t.Errorf("policy %q names the local entry %q AFTER a vendor entry; chain is %v. "+
					"The chain is tried in order, so this spends money before offering the "+
					"free door.", name, entry, chain)
			}
		}
	}
}

// The list above must not fall behind the file. A shipped policy added to the
// tree and not to `shippedPolicies` would be exempt from the ordering gate
// while looking covered -- the failure mode every allowlist has.
func TestEveryShippedPolicyIsCovered(t *testing.T) {
	chains := policyChains(t)
	for name := range chains {
		if !policyNames(shippedPolicies, name) {
			t.Errorf("policy %q is declared in dsl/policies/policies.memql but is not in "+
				"shippedPolicies, so nothing checks that it tries the doors in cost order. "+
				"Add it to the list, or say in a comment there why it is exempt.", name)
		}
	}
}

// The four local-first policies memql#4676 seeded are GONE, and their absence
// is asserted rather than assumed: design D6 required each wired to a purpose
// or deleted, and a policy that came back without a consumer would be exactly
// the decoration that rule removed.
func TestTheSeededLocalOnlyPoliciesAreGone(t *testing.T) {
	chains := policyChains(t)
	for _, name := range []string{"localPlanner", "localConductor", "localSuggest", "localEmbeddings"} {
		if _, ok := chains[name]; ok {
			t.Errorf("policy %q is back. It was deleted because nothing named it and the "+
				"purposes it stood for are gone or covered by the default chain (design D6); "+
				"if it has a consumer now, say so here and give it one.", name)
		}
	}
}

// The vendor entries must still EXIST inside the chains. Local-first is an
// ORDER, not a removal: a turn the fleet and the apps cannot serve still has to
// reach a model, and a chain with nothing after the local doors parks work that
// a configured cluster could have done.
func TestTheCloudEntriesRemainInEveryChain(t *testing.T) {
	chains := policyChains(t)
	for _, name := range shippedPolicies {
		chain := chains[name]
		vendor := false
		for _, entry := range chain {
			if !strings.HasPrefix(entry, fleetRefPrefix) && !strings.HasPrefix(entry, appRefPrefix) {
				vendor = true
			}
		}
		if !vendor {
			t.Errorf("policy %q has no vendor entry; chain is %v. Local-first is an order, not a "+
				"removal -- a chain that ends at the local doors parks every turn a configured "+
				"cluster could have served.", name, chain)
		}
	}
}

func policyNames(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
