package identity

import (
	"context"
	"testing"

	memqlengine "github.com/znasllc-io/memql/component/memql"
)

// doorEngine answers `query accountFrontDoorsOpen()` from a fixture.
type doorEngine struct {
	rows  []map[string]any
	calls int
	err   error
}

// Execute answers through memql.NewResultWithOutput -- the shape
// MemQLEngine.Execute really returns for a SHAPED query, which
// accountFrontDoorsOpen is. A bare &ExecuteResult{} has a nil OutputPayload
// and would read as zero rows, which is the false green this stub exists to
// avoid.
func (e *doorEngine) Execute(_ context.Context, _ string) (*memqlengine.ExecuteResult, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	out := make([]any, 0, len(e.rows))
	for _, r := range e.rows {
		out = append(out, r)
	}
	return memqlengine.NewResultWithOutput(out), nil
}

func liveDoorRows(names ...string) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"reservedName": n, "status": "live"})
	}
	return out
}

// ===========================================================================
// The Host header is a claim; the row is the fact
// ===========================================================================

// THE HEADLINE PROPERTY. A Host nobody has a live door for resolves to
// nothing, so nothing downstream is scoped to it -- which is the whole reason
// this resolver exists rather than a string operation on r.Host.
func TestAHostWithNoLiveDoorResolvesToNothing(t *testing.T) {
	d := NewDoorResolver(&doorEngine{rows: liveDoorRows("memql.acme.com")})

	for _, host := range []string{
		"id.memql.attacker.example",  // a domain the attacker owns
		"app.memql.attacker.example", // ...and its sibling
		"id.memql.acme.com.evil.net", // a name that merely contains a real one
	} {
		if name, ok := d.ReservedNameFor(context.Background(), host); ok {
			t.Errorf("%q resolved to the door %q; only a LIVE row may make a name real", host, name)
		}
	}
}

func TestALiveDoorResolvesFromEitherBrowserFacingHost(t *testing.T) {
	d := NewDoorResolver(&doorEngine{rows: liveDoorRows("memql.acme.com")})

	for _, host := range []string{
		"id.memql.acme.com",     // where the ceremony runs
		"app.memql.acme.com",    // where the OS is served
		"ID.MEMQL.ACME.COM",     // case
		"id.memql.acme.com:443", // a Host header carries a port
		"id.memql.acme.com.",    // a trailing dot is legal in a Host
	} {
		name, ok := d.ReservedNameFor(context.Background(), host)
		if !ok || name != "memql.acme.com" {
			t.Errorf("%q resolved to (%q, %v), want (memql.acme.com, true)", host, name, ok)
		}
	}
}

// The api. host serves the bff and never reaches this binary. Recognising it
// here would be admitting a label on a path that cannot produce one -- and
// every label this accepts is one an attacker can put in front of a domain
// they own.
func TestTheApiLabelIsNotADoorHostHere(t *testing.T) {
	d := NewDoorResolver(&doorEngine{rows: liveDoorRows("memql.acme.com")})
	if name, ok := d.ReservedNameFor(context.Background(), "api.memql.acme.com"); ok {
		t.Errorf("api.memql.acme.com resolved to %q; only id. and app. reach the identity service", name)
	}
}

// A door that is not `live` is not a door. `live` is reached only after all
// three hosts pointed at this cluster AND the certificate came back Ready.
func TestOnlyLiveDoorsResolve(t *testing.T) {
	rows := []map[string]any{
		{"reservedName": "memql.acme.com", "status": "issuing"},
		{"reservedName": "memql.other.com", "status": "removing"},
	}
	d := NewDoorResolver(&doorEngine{rows: rows})
	for _, host := range []string{"id.memql.acme.com", "id.memql.other.com"} {
		if _, ok := d.ReservedNameFor(context.Background(), host); ok {
			t.Errorf("%q resolved through a door that is not serving", host)
		}
	}
}

// The set is read WHOLE and cached. A per-name read would be a database round
// trip driven by an unauthenticated caller's choice of Host header.
func TestTheDoorSetIsReadOnceAndCached(t *testing.T) {
	eng := &doorEngine{rows: liveDoorRows("memql.acme.com")}
	d := NewDoorResolver(eng)

	for range 5 {
		d.ReservedNameFor(context.Background(), "id.memql.acme.com")
		d.ReservedNameFor(context.Background(), "id.nobody.example")
	}
	if eng.calls != 1 {
		t.Errorf("the door set was read %d times for 10 lookups; a Host-driven database read is a lookup a stranger can drive", eng.calls)
	}
}

// A FAILED REFRESH KEEPS THE LAST GOOD SET. Clearing it would take every
// client's sign-in down for as long as the database was unreachable.
func TestAFailedRefreshKeepsTheLastGoodSet(t *testing.T) {
	eng := &doorEngine{rows: liveDoorRows("memql.acme.com")}
	d := NewDoorResolver(eng)
	if _, ok := d.ReservedNameFor(context.Background(), "id.memql.acme.com"); !ok {
		t.Fatal("the door did not resolve before the failure")
	}

	eng.err = context.DeadlineExceeded
	d.loaded = d.loaded.Add(-2 * doorCacheTTL) // force the refresh

	if _, ok := d.ReservedNameFor(context.Background(), "id.memql.acme.com"); !ok {
		t.Error("a live door stopped resolving because a refresh failed; the last good set must survive a transient outage")
	}
}

// ===========================================================================
// The redirect URI
// ===========================================================================

func doorTestConfig() Config {
	cfg := Config{}
	cfg.Bootstrap.Domain = "memql.localhost"
	cfg.RegisteredClients = []RegisteredClient{
		{ClientId: "os", RedirectURIs: []string{"https://os.memql.localhost/auth/callback"}},
		{ClientId: "cockpit", RedirectURIs: []string{"http://127.0.0.1/cockpit/callback"}},
	}
	return cfg
}

func TestALiveDoorAddsExactlyOneRedirectUri(t *testing.T) {
	cfg := doorTestConfig()
	d := NewDoorResolver(&doorEngine{rows: liveDoorRows("memql.acme.com")})

	if !DoorAllowsRedirectURI(context.Background(), cfg, d, "os", "https://app.memql.acme.com/auth/callback") {
		t.Error("the door's own callback was refused for the OS client")
	}

	for name, uri := range map[string]string{
		"another path":        "https://app.memql.acme.com/anything",
		"the id host":         "https://id.memql.acme.com/auth/callback",
		"the api host":        "https://api.memql.acme.com/auth/callback",
		"a name with no door": "https://app.memql.nobody.example/auth/callback",
		"plain http":          "http://app.memql.acme.com/auth/callback",
	} {
		t.Run(name, func(t *testing.T) {
			if DoorAllowsRedirectURI(context.Background(), cfg, d, "os", uri) {
				t.Errorf("%q was admitted; a door adds exactly one URI", uri)
			}
		})
	}
}

// A door widens ONE client -- the one that owns the OS's own callback on this
// cluster. Admitting it for any other would let a door's host become a
// redirect target for software that has nothing to do with it.
func TestADoorWidensOnlyTheOsClient(t *testing.T) {
	cfg := doorTestConfig()
	d := NewDoorResolver(&doorEngine{rows: liveDoorRows("memql.acme.com")})
	uri := "https://app.memql.acme.com/auth/callback"

	for _, clientId := range []string{"cockpit", "memql-vscode", "", "some-self-registered-client"} {
		if DoorAllowsRedirectURI(context.Background(), cfg, d, clientId, uri) {
			t.Errorf("the door's callback was admitted for client %q", clientId)
		}
	}
}

// FAILS CLOSED. If no registered client owns the OS callback, there is no
// client to widen and no door redirect is legal.
func TestNoOsClientMeansNoDoorRedirect(t *testing.T) {
	cfg := doorTestConfig()
	cfg.RegisteredClients = []RegisteredClient{{ClientId: "cockpit", RedirectURIs: []string{"http://127.0.0.1/cockpit/callback"}}}
	d := NewDoorResolver(&doorEngine{rows: liveDoorRows("memql.acme.com")})

	if DoorAllowsRedirectURI(context.Background(), cfg, d, "os", "https://app.memql.acme.com/auth/callback") {
		t.Error("a door redirect was admitted on a cluster where no registered client owns the OS callback")
	}
}

// The OS client id is DERIVED from the registered set rather than hardcoded,
// so a cluster that names it something else still works.
func TestTheOsClientIdIsDerivedNotAssumed(t *testing.T) {
	cfg := doorTestConfig()
	cfg.RegisteredClients[0].ClientId = "shell"
	d := NewDoorResolver(&doorEngine{rows: liveDoorRows("memql.acme.com")})

	if !DoorAllowsRedirectURI(context.Background(), cfg, d, "shell", "https://app.memql.acme.com/auth/callback") {
		t.Error("the OS client was not found under a non-default id; it is derived from the registered set, not assumed to be \"os\"")
	}
}

// A process with NO installed resolver behaves exactly as it did before doors
// existed.
func TestNoInstalledResolverAdmitsNoDoor(t *testing.T) {
	if DoorAllowsRedirectURI(context.Background(), doorTestConfig(), nil, "os", "https://app.memql.acme.com/auth/callback") {
		t.Error("a door redirect was admitted with no resolver installed")
	}
}
