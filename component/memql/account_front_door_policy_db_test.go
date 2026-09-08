package memql

import (
	"context"
	"strings"
	"testing"
)

// account_front_door_policy_db_test.go -- epic memql#5168, design D7/H.
//
// The guard asks whether anything on this cluster already answers on one of
// the three hosts a reserved name would serve, which needs a read -- so it
// cannot be a unit test, and the two probes it uses read WITHOUT row-authz
// narrowing, which cannot be observed without rows.
//
// WHAT THIS DOES NOT YET PROVE, and what has to be added the moment record A
// (memql#5165) is on main: that `executeWrite` actually CALLS this guard on an
// account write. Every assertion below would keep passing if the call site were
// never added -- the same gap the neighbouring custom-domain file's header
// describes, and the reason that file drives its guards through `eng.Execute`
// rather than directly. The call site lands in record A's
// `validateAccountDomainPolicy`, which does not exist on this branch yet, so
// the mutation-path half of this file is deliberately absent rather than
// faked. See the epic's post-rebase plan.
//
// Postgres-gated like its neighbours. CI's db-tests lane runs this package with
// MEMQL_REQUIRE_DB=1, so a skip there is a failure rather than a green.

// afdSuffix keys the fixtures on the TEST's own name, cdSuffix's reasoning: the
// engine is a package-level fixture, so isolation has to come from the ids --
// and on this machine the throwaway Postgres is shared between sessions, so a
// fixture named by anything less specific collides with somebody else's run.
func afdSuffix(t *testing.T) string {
	t.Helper()
	return uniqueSuffix("afd-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")))
}

// A reserved name whose `app.` host is already a live deployable's own hostname
// must be refused. The edge asks `siteByHostname` FIRST, so a door on that name
// would be dead on arrival -- it would resolve to the deployable forever and
// the operator would have no way to see why.
func TestAReservedNameCollidingWithALiveSiteIsRefused(t *testing.T) {
	eng, _, _ := sharedReadMergeEngine(t)
	suffix := afdSuffix(t)

	reserved := "memql-" + suffix + ".acme.example"
	taken := "app." + reserved

	if _, err := createSiteRaw(t, systemSiteCtx(), eng, map[string]any{
		"siteId":    "site-" + suffix,
		"hostname":  taken,
		"bundleRef": "blob://sites/site-" + suffix + "/v1/",
		"status":    "live",
	}); err != nil {
		t.Fatalf("could not seed the deployable that takes the host: %v", err)
	}

	err := eng.validateAccountFrontDoorHosts(context.Background(), map[string]any{"memqlDomain": reserved})
	if err == nil {
		t.Fatalf("reserving %q was admitted, but %q is already a live deployable's hostname", reserved, taken)
	}
	if !strings.Contains(err.Error(), accountNameCollidesWithSiteCode) {
		t.Errorf("refusal does not carry the typed code %q, so the rail cannot key on it: %v",
			accountNameCollidesWithSiteCode, err)
	}
	if !strings.Contains(err.Error(), taken) {
		t.Errorf("refusal does not name WHICH of the three hosts collided, which is the only actionable part: %v", err)
	}
}

// The same for a live custom domain. A reserved name is checked against BOTH
// concepts because the edge resolves against both.
func TestAReservedNameCollidingWithALiveCustomDomainIsRefused(t *testing.T) {
	eng, _, _ := sharedReadMergeEngine(t)
	t.Setenv(memqlDomainEnv, customDomainTestDomain)
	suffix := afdSuffix(t)

	siteId := seedCustomDomainSite(t, eng, suffix)
	reserved := "memql-" + suffix + ".acme.example"
	taken := "id." + reserved

	if err := createCustomDomainRaw(t, eng, "cd-"+suffix, siteId, taken); err != nil {
		t.Fatalf("could not seed the custom domain that takes the host: %v", err)
	}
	// A binding is only a collision once it is LIVE -- the edge's own filter
	// says so -- and createCustomDomainRaw lands it at pending_dns, so walk it.
	if _, err := runSiteMutation(t, ownerCustomDomainCtx(), eng, "markCustomDomainLive", map[string]any{
		"domainId":      "cd-" + suffix,
		"issuedAt":      "2026-09-08T00:00:00Z",
		"lastCheckedAt": "2026-09-08T00:00:00Z",
	}); err != nil {
		t.Fatalf("could not walk the binding to live: %v", err)
	}

	err := eng.validateAccountFrontDoorHosts(context.Background(), map[string]any{"memqlDomain": reserved})
	if err == nil {
		t.Fatalf("reserving %q was admitted, but %q is already a live custom domain", reserved, taken)
	}
	if !strings.Contains(err.Error(), accountNameCollidesWithCustomDomainCode) {
		t.Errorf("refusal does not carry the typed code %q: %v", accountNameCollidesWithCustomDomainCode, err)
	}
}

// THE CONTROL, and without it every assertion above is satisfied by a guard
// that refuses everything. A name whose three hosts nobody holds is admitted.
func TestAFreeReservedNameIsAdmitted(t *testing.T) {
	eng, _, _ := sharedReadMergeEngine(t)
	suffix := afdSuffix(t)

	if err := eng.validateAccountFrontDoorHosts(context.Background(), map[string]any{
		"memqlDomain": "memql-" + suffix + ".nobody-holds-this.example",
	}); err != nil {
		t.Errorf("a reserved name nothing else answers on was refused: %v", err)
	}
}

// An account with no reserved name is not a collision. The field is optional
// and empty is its ordinary state, so a guard that refused it would refuse
// every account created before a domain was recorded.
func TestNoReservedNameIsNotACollision(t *testing.T) {
	eng, _, _ := sharedReadMergeEngine(t)
	for _, payload := range []map[string]any{
		{},
		{"memqlDomain": ""},
		{"memqlDomain": "   "},
	} {
		if err := eng.validateAccountFrontDoorHosts(context.Background(), payload); err != nil {
			t.Errorf("payload %v was refused: %v", payload, err)
		}
	}
}

// THE COLLISION IS CHECKED ON ALL THREE HOSTS, not just the first. A guard
// that stopped at `app.` would admit a name whose `api.` or `id.` host is
// taken -- and that door would then go live with one of its three hosts
// resolving to somebody else's deployable.
func TestEveryDerivedHostIsChecked(t *testing.T) {
	eng, _, _ := sharedReadMergeEngine(t)

	for _, label := range []string{"app", "api", "id"} {
		t.Run(label, func(t *testing.T) {
			suffix := afdSuffix(t)
			reserved := "memql-" + suffix + ".acme.example"
			taken := label + "." + reserved

			if _, err := createSiteRaw(t, systemSiteCtx(), eng, map[string]any{
				"siteId":    "site-" + suffix,
				"hostname":  taken,
				"bundleRef": "blob://sites/site-" + suffix + "/v1/",
				"status":    "live",
			}); err != nil {
				t.Fatalf("could not seed the deployable taking %q: %v", taken, err)
			}

			err := eng.validateAccountFrontDoorHosts(context.Background(), map[string]any{"memqlDomain": reserved})
			if err == nil {
				t.Fatalf("%q was admitted with its %s host already taken by a deployable", reserved, label)
			}
			if !strings.Contains(err.Error(), taken) {
				t.Errorf("refusal does not name the %s host: %v", label, err)
			}
		})
	}
}
