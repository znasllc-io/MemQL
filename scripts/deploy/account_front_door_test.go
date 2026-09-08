// Tests for scripts/deploy/bind-account-front-door.sh (capability
// frontdoor.bind) and scripts/deploy/unbind-account-front-door.sh
// (frontdoor.unbind), epic memql#5168.
//
// These drive the scripts as PROGRAMS -- argv in, one JSON envelope out, an
// exit code -- for custom_domain_test.go's reason: that is the whole of their
// contract, and what is proved here is that the envelope the engine-side tests
// assume is the envelope these scripts actually emit.
//
// The helpers (envelopeFrom, aksScript, asExitError) are that file's, shared
// across the package rather than duplicated.
package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/deploycontrol"
	"github.com/znasllc-io/memql/component/frontdoor"
)

// resultOf decodes the envelope's result object. env.Result is raw JSON so a
// script may return any shape; every assertion below reads it as a map.
func resultOf(t *testing.T, env deploycontrol.CapabilityResult) map[string]any {
	t.Helper()
	var out map[string]any
	if len(env.Result) == 0 {
		return out
	}
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("decoding the envelope result: %v", err)
	}
	return out
}

const (
	bindDoorScript   = "bind-account-front-door.sh"
	unbindDoorScript = "unbind-account-front-door.sh"

	testAccountID    = "acct-test"
	testReservedName = "memql.acme.com"
	testObjectName   = "account-front-door-acct-test"
)

// A cluster with no ACME issuer must REFUSE rather than apply a Certificate
// with an empty issuerRef, which the API server accepts and then leaves
// Pending forever. Exit 3, and the typed reason on the result -- the exit code
// says "refused" and not WHICH refusal, and the row and the rail both key on
// the reason.
func TestBindDoorRefusesWithNoIssuer(t *testing.T) {
	env, code := envelopeFrom(t, bindDoorScript,
		"--accountId="+testAccountID, "--reservedName="+testReservedName, "--dryRun=true")

	if code != 3 {
		t.Errorf("exit code = %d, want 3 (refused)", code)
	}
	if env.OK {
		t.Error("ok = true for a refusal")
	}
	if got := fmt.Sprint(resultOf(t, env)["reason"]); got != "no_acme_issuer" {
		t.Errorf("result.reason = %q, want %q -- the row and the rail key on this, not on the exit code", got, "no_acme_issuer")
	}
}

func TestBindDoorRefusesBadParameters(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no accountId", []string{"--reservedName=" + testReservedName, "--issuer=x"}},
		{"no reservedName", []string{"--accountId=" + testAccountID, "--issuer=x"}},
		{"single label", []string{"--accountId=" + testAccountID, "--reservedName=localhost", "--issuer=x"}},
		{"wildcard", []string{"--accountId=" + testAccountID, "--reservedName=*.acme.com", "--issuer=x"}},
		{"non-numeric port", []string{"--accountId=" + testAccountID, "--reservedName=" + testReservedName, "--issuer=x", "--edgePort=eighty"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, code := envelopeFrom(t, bindDoorScript, append(tc.args, "--dryRun=true")...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2 (bad parameter)", code)
			}
		})
	}
}

// The dry run reaches no cluster, and must still render every object: one
// Certificate naming all three hosts, and four Ingresses.
func TestBindDoorDryRunRendersEveryObject(t *testing.T) {
	env, code := envelopeFrom(t, bindDoorScript,
		"--accountId="+testAccountID, "--reservedName="+testReservedName,
		"--issuer=letsencrypt-prod", "--apiPaths=/healthz,/memql/ws,/artifacts",
		"--dryRun=true")

	if code != 0 || !env.OK {
		t.Fatalf("dry run: exit %d ok=%v -- it must not need a cluster", code, env.OK)
	}
	res := resultOf(t, env)
	for key, want := range map[string]string{
		"appHost":    frontdoor.AccountRoleHost(frontdoor.AccountRoleApp, testReservedName),
		"apiHost":    frontdoor.AccountRoleHost(frontdoor.AccountRoleAPI, testReservedName),
		"idHost":     frontdoor.AccountRoleHost(frontdoor.AccountRoleID, testReservedName),
		"objectName": testObjectName,
	} {
		if got := fmt.Sprint(res[key]); got != want {
			t.Errorf("result.%s = %q, want %q", key, got, want)
		}
	}
	if got := fmt.Sprint(res["apiPathCount"]); got != "3" {
		t.Errorf("result.apiPathCount = %q, want 3", got)
	}
	if got := fmt.Sprint(res["certificateReady"]); got != "false" {
		t.Errorf("result.certificateReady = %q on a dry run: nothing was requested, so nothing can be Ready", got)
	}
}

// THE LAST PATH IS THE ONE THAT GETS DROPPED. `tr` leaves the final field
// without a trailing newline, so a bare `while read` loop silently omits it --
// which this test caught in the first version of the script. A route that
// reaches no rule is an HTTP/1.1 request handed to an h2c backend, which fails
// naming no path, no host and no generator, so a count taken only from the
// INPUT could never have noticed.
func TestBindDoorRoutesEveryPathItIsGiven(t *testing.T) {
	for _, paths := range []string{"/healthz", "/healthz,/memql/ws", "/a,/b,/c,/d,/e"} {
		t.Run(paths, func(t *testing.T) {
			env, code := envelopeFrom(t, bindDoorScript,
				"--accountId="+testAccountID, "--reservedName="+testReservedName,
				"--issuer=letsencrypt-prod", "--apiPaths="+paths, "--dryRun=true")
			if code != 0 || !env.OK {
				t.Fatalf("exit %d ok=%v: the render disagreed with the path count it was given", code, env.OK)
			}
			want := fmt.Sprint(strings.Count(paths, ",") + 1)
			if got := fmt.Sprint(resultOf(t, env)["apiPathCount"]); got != want {
				t.Errorf("apiPathCount = %q, want %q", got, want)
			}
		})
	}
}

// No paths at all must still produce a front door -- three hosts, four
// documents, no api HTTP Ingress. An Ingress whose rule carries a zero-length
// paths list is rejected by the API server, and emitting one would take down
// the three hosts that have nothing to do with it.
func TestBindDoorWithNoPathsStillOpensTheDoor(t *testing.T) {
	env, code := envelopeFrom(t, bindDoorScript,
		"--accountId="+testAccountID, "--reservedName="+testReservedName,
		"--issuer=letsencrypt-prod", "--dryRun=true")
	if code != 0 || !env.OK {
		t.Fatalf("exit %d ok=%v: a door with no HTTP paths is a door missing its HTTP routes, not one that failed to come up", code, env.OK)
	}
	if got := fmt.Sprint(resultOf(t, env)["apiPathCount"]); got != "0" {
		t.Errorf("apiPathCount = %q, want 0", got)
	}
}

func TestUnbindDoorRequiresTheAccountId(t *testing.T) {
	_, code := envelopeFrom(t, unbindDoorScript, "--reservedName="+testReservedName, "--dryRun=true")
	if code != 2 {
		t.Errorf("exit code = %d, want 2: every object is named after the account id, so there is nothing to remove without one", code)
	}
}

// THE OBJECT NAME IS SPELLED IN THREE PLACES -- both scripts and
// integrations/customdomain's provisioner -- and they must agree or a bind and
// its unbind act on different objects, leaving Ingresses behind that still
// serve a name the cluster no longer claims.
func TestBothDoorScriptsAgreeOnTheObjectName(t *testing.T) {
	bind, _ := envelopeFrom(t, bindDoorScript,
		"--accountId="+testAccountID, "--reservedName="+testReservedName,
		"--issuer=letsencrypt-prod", "--dryRun=true")
	unbind, _ := envelopeFrom(t, unbindDoorScript,
		"--accountId="+testAccountID, "--reservedName="+testReservedName, "--dryRun=true")

	got, want := fmt.Sprint(resultOf(t, bind)["objectName"]), fmt.Sprint(resultOf(t, unbind)["objectName"])
	if got != want {
		t.Fatalf("bind names %q and unbind names %q", got, want)
	}
	if got != testObjectName {
		t.Errorf("object name = %q, want %q (integrations/customdomain's objectName must produce the same)", got, testObjectName)
	}
}

// The scripts compose the three hosts themselves, and component/frontdoor's
// AccountHosts is meant to be the ONE place those labels are spelled. This
// reads the labels out of the shell source rather than restating them, so a
// change to AccountRoles that the scripts do not follow fails here rather than
// in a cluster.
func TestTheScriptsUseTheSameThreeLabelsAsFrontdoor(t *testing.T) {
	raw, err := os.ReadFile(aksScript(t, bindDoorScript))
	if err != nil {
		t.Fatalf("reading the bind script: %v", err)
	}
	src := string(raw)

	for _, role := range frontdoor.AccountRoles() {
		// The script composes each host as `<label>.${RESERVED_NAME}`.
		want := fmt.Sprintf("%s.${RESERVED_NAME}", role)
		if !strings.Contains(src, want) {
			t.Errorf("the bind script does not compose %q -- component/frontdoor declares the %q role and the script must serve it", want, role)
		}
	}

	// And nothing else: a fourth composed host would be a name with no SAN
	// behind it, since AccountCertificateSANs is derived from AccountRoles.
	if got, want := strings.Count(src, ".${RESERVED_NAME}\""), len(frontdoor.AccountRoles()); got != want {
		t.Errorf("the bind script composes %d hosts, but component/frontdoor declares %d roles", got, want)
	}
}
