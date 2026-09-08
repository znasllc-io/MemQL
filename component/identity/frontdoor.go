package identity

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/frontdoor"
	"github.com/znasllc-io/memql/component/memql"
)

// frontdoor.go -- resolving an account's reserved front door on the identity
// node (epic memql#5168, design G).
//
// # WHAT MOVES PER DOOR, AND WHAT DOES NOT
//
// The TOKEN does not move. It still says `iss = https://identity.<cluster
// domain>` whatever host minted it: one identity service, one keyset, one
// issuer, so component/identity/verifier is untouched and no token minted
// before doors existed stops verifying. Design D3 rejected an issuer per
// reserved name precisely because it would buy no enforcement -- authorization
// is per-row through the account grant and `sub` is cluster-wide either way --
// while costing the verifier a list of issuers and a JWKS fetch per name.
//
// What moves is the BROWSER-FACING surface, because a browser's rules are
// origin rules:
//
//   - the WebAuthn RP id, which must be a registrable-domain suffix of the
//     origin the ceremony runs on;
//   - the OAuth redirect URI, which must name the host the OS is served at.
//
// The `memql_ml` cookie needs nothing: it is host-only at `Path=/auth`
// already, which IS the per-host device binding.
//
// # THE HOST HEADER IS A CLAIM; THE ROW IS THE FACT
//
// This is the whole security property of the file, and it is the same one
// webauthn.RelyingParty's doc comment states for the cluster: a value derived
// from an attacker-supplied Host would let anyone reaching this service under
// a name they control have credentials minted for that name. So nothing here
// trusts `r.Host`. The host is STRIPPED to a candidate reserved name and then
// looked up against a LIVE v1:platform:accountFrontDoor row; only a row that
// comes back makes it real, and a miss falls back to the cluster's own values.
//
// A door is `live` only after all three of its hosts pointed at this cluster
// and its certificate came back Ready -- so a name reaching here as a live door
// is one somebody proved ownership of and this cluster agreed to serve.

// doorCacheTTL bounds how stale the live-door set may be.
//
// SIXTY SECONDS, matched to nothing in particular and deliberately not to the
// reconciler's two-minute schedule: a door becomes live on the sweep's clock,
// so the worst case here is a client's people waiting one more minute after
// their certificate was issued. The cost of a shorter TTL is a database read
// on the sign-in path, which is the one path that must not get slower.
const doorCacheTTL = time.Minute

// systemFrontDoorActor is the synthetic cluster-owner identity this read runs
// under. v1:platform:accountFrontDoor is clusterOwner tier and the identity
// service is a service rather than a person -- the precedent, and the
// reasoning, are component/edge's systemEdgeActor and
// integrations/customdomain's systemCustomDomainActor.
const systemFrontDoorActor = "system:accountFrontDoor"

// DoorResolver answers "is this host an account's live front door, and under
// what reserved name".
type DoorResolver struct {
	engine EngineExecutor

	mu     sync.RWMutex
	names  map[string]bool
	loaded time.Time
}

// NewDoorResolver wraps an engine. A nil engine resolves nothing, which is the
// correct behaviour for a node with no graph handle: every host falls back to
// the cluster's own values.
func NewDoorResolver(engine EngineExecutor) *DoorResolver {
	return &DoorResolver{engine: engine, names: map[string]bool{}}
}

// ReservedNameFor returns the reserved name a request Host belongs to, and
// whether it is a live door.
//
// It accepts EITHER of the two hosts a browser can reach the identity service
// at under a door -- `id.<reservedName>` (where the ceremony runs) and
// `app.<reservedName>` (where the OS is served, and where a redirect lands).
// Neither is trusted: both are stripped to the same candidate name and the
// name is what is looked up.
func (d *DoorResolver) ReservedNameFor(ctx context.Context, host string) (string, bool) {
	if d == nil {
		return "", false
	}
	name, ok := reservedNameFromDoorHost(host)
	if !ok {
		return "", false
	}
	if !d.isLive(ctx, name) {
		return "", false
	}
	return name, true
}

// reservedNameFromDoorHost strips a known door label off a host.
//
// ONLY `id.` AND `app.`. The `api.` host serves the bff and never reaches the
// identity binary, so accepting it here would be recognising a name on a path
// that cannot produce one -- and every label this function accepts is a label
// an attacker can put in front of a domain they own.
func reservedNameFromDoorHost(host string) (string, bool) {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return "", false
	}
	// Strip a port: a Host header carries one and a reserved name never does.
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	h = strings.TrimSuffix(h, ".")

	for _, role := range []frontdoor.AccountRole{frontdoor.AccountRoleID, frontdoor.AccountRoleApp} {
		rest, ok := strings.CutPrefix(h, string(role)+".")
		if !ok || rest == "" {
			continue
		}
		// A reserved name is a client's own multi-label domain.
		if !strings.Contains(rest, ".") {
			continue
		}
		return rest, true
	}
	return "", false
}

// isLive answers from the cache, refreshing the whole live set when it is
// stale.
//
// THE WHOLE SET, not the one name. A per-name read would be a database round
// trip on every sign-in attempt for every host, including the ones that are
// not doors at all -- which is a lookup an unauthenticated caller can drive by
// choosing a Host header. One read of a set bounded by the number of accounts
// an operator typed, once a minute, cannot be driven by anybody.
func (d *DoorResolver) isLive(ctx context.Context, name string) bool {
	d.mu.RLock()
	fresh := time.Since(d.loaded) < doorCacheTTL
	live := d.names[name]
	d.mu.RUnlock()
	if fresh {
		return live
	}
	d.refresh(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.names[name]
}

func (d *DoorResolver) refresh(ctx context.Context) {
	if d.engine == nil {
		return
	}
	res, err := d.engine.Execute(doorActorContext(ctx), "query accountFrontDoorsOpen()")
	if err != nil {
		// A FAILED REFRESH KEEPS THE LAST GOOD SET AND DOES NOT EXTEND ITS
		// LIFETIME. Clearing it would take every client's sign-in down for as
		// long as the database was unreachable; marking it fresh would hide a
		// permanent failure behind a set that never changes again. Keeping the
		// values and leaving `loaded` alone means the next request tries
		// again, which is what a transient failure deserves.
		return
	}
	names := map[string]bool{}
	for _, row := range memql.MaterializeRows(res) {
		status, _ := row["status"].(string)
		if status != "live" {
			continue
		}
		if n, _ := row["reservedName"].(string); n != "" {
			names[n] = true
		}
	}
	d.mu.Lock()
	d.names = names
	d.loaded = time.Now()
	d.mu.Unlock()
}

func doorActorContext(ctx context.Context) context.Context {
	claims := map[string]any{"sub": systemFrontDoorActor, "role": "owner"}
	ctx = auth.ContextWithClaims(ctx, claims)
	ctx = auth.ContextWithToken(ctx, auth.BuildTokenInfo(claims))
	ctx = auth.ContextWithAccess(ctx, &auth.AccessContext{
		UserId: systemFrontDoorActor,
		Role:   auth.RoleOwner,
	})
	return auth.ContextWithInternalOrigin(ctx)
}

// DoorRedirectURI is the one callback URI a live door adds: the OS, under that
// door's `app.` host.
func DoorRedirectURI(reservedName string) string {
	return "https://" + frontdoor.AccountRoleHost(frontdoor.AccountRoleApp, reservedName) + "/auth/callback"
}

// osClientIDFor finds the client id that owns the OS's own callback on this
// cluster -- the client a door's redirect URI is added to.
//
// DERIVED RATHER THAN HARDCODED as "os". The registered-clients list comes from
// component/envregistry's domain derivation, and its client ids are that
// derivation's business; a literal here would be a second spelling that goes
// wrong the first time it changes. Returns "" when no registered client owns
// the OS callback, which fails CLOSED: no client, no door redirect.
func osClientIDFor(cfg Config) string {
	domain := clusterDomainOf(cfg)
	if domain == "" {
		return ""
	}
	want := "https://" + frontdoor.OsHost(domain) + "/auth/callback"
	for _, c := range cfg.RegisteredClients {
		for _, uri := range c.RedirectURIs {
			if strings.EqualFold(strings.TrimSpace(uri), want) {
				return c.ClientId
			}
		}
	}
	return ""
}

// clusterDomainOf is this cluster's own domain, from the two places the
// identity service already carries it.
//
// Bootstrap.Domain first because it IS the domain, verbatim, from
// MEMQL_IDENTITY_BOOTSTRAP_DOMAIN. BaseURL second, with its leading
// `identity.` label stripped, because both come from the SAME envregistry
// derivation and so cannot disagree -- reading both means neither one being
// unset takes the door redirect down. Empty when neither is usable, which
// fails closed.
func clusterDomainOf(cfg Config) string {
	if d := strings.TrimSpace(cfg.Bootstrap.Domain); d != "" {
		return d
	}
	base := strings.TrimSpace(cfg.BaseURL)
	base = strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	host, _, _ := strings.Cut(base, "/")
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	rest, ok := strings.CutPrefix(host, string(frontdoor.RoleIdentity)+".")
	if !ok || !strings.Contains(rest, ".") {
		return ""
	}
	return rest
}

// DoorAllowsRedirectURI reports whether `uri` is the callback of a LIVE
// account front door, for the client that owns the OS's own callback.
//
// This is the fourth source ClientAllowsRedirectURI consults, and it is
// deliberately the narrowest of the four: it admits exactly one URI per live
// door, for exactly one client id, and only while that door is serving. It
// cannot widen any other client, cannot admit any other path, and stops
// admitting the moment the door leaves `live` -- which is the same write that
// stops the edge resolving the host.
func DoorAllowsRedirectURI(ctx context.Context, cfg Config, doors *DoorResolver, clientId, uri string) bool {
	if doors == nil || clientId == "" {
		return false
	}
	osClient := osClientIDFor(cfg)
	if osClient == "" || !strings.EqualFold(clientId, osClient) {
		return false
	}
	name, ok := reservedNameFromURI(uri)
	if !ok {
		return false
	}
	if !doors.isLive(ctx, name) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(uri), DoorRedirectURI(name))
}

// reservedNameFromURI extracts the candidate reserved name from a callback
// URI, without trusting it -- the caller looks it up.
func reservedNameFromURI(uri string) (string, bool) {
	trimmed := strings.TrimSpace(uri)
	rest, ok := strings.CutPrefix(strings.ToLower(trimmed), "https://")
	if !ok {
		return "", false
	}
	host, _, found := strings.Cut(rest, "/")
	if !found || host == "" {
		return "", false
	}
	return reservedNameFromDoorHost(host)
}

// String is here so a DoorResolver in a log line says something useful rather
// than a pointer.
func (d *DoorResolver) String() string {
	if d == nil {
		return "account front doors: none"
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return fmt.Sprintf("account front doors: %d live", len(d.names))
}

// ---------------------------------------------------------------------------
// The installed resolver
// ---------------------------------------------------------------------------

// installedDoors is the one DoorResolver this process uses.
//
// # WHY A PACKAGE-LEVEL VALUE RATHER THAN A THREADED DEPENDENCY
//
// Six call sites decide whether a redirect URI is registered
// (ClientAllowsRedirectURI), and they live in four packages -- http, web,
// magiclink's issuer and its verifier -- each with its own constructor and its
// own Config copy. Threading a resolver through all four would add a field to
// four types and a wiring line to four call sites in app/, for a value that is
// one per process and never changes after boot.
//
// The precedent is the RBAC catalog (memql#5166), which is installed into
// component/auth as the ONE resolver every Capable call reads, for the same
// reason: it is infrastructure the whole process shares, and a copy per
// consumer is a copy that can disagree.
//
// UNSET IS THE SAFE STATE. A process that never installs one -- a test, a node
// with no graph handle -- resolves no doors, so every redirect falls back to
// the statically registered set and every ceremony to the cluster's own
// relying party. That is exactly the behaviour before doors existed.
var installedDoors struct {
	mu sync.RWMutex
	d  *DoorResolver
}

// InstallDoorResolver installs the process-wide resolver. Called once from
// app/ when the identity node has an engine.
func InstallDoorResolver(d *DoorResolver) {
	installedDoors.mu.Lock()
	installedDoors.d = d
	installedDoors.mu.Unlock()
}

// Doors returns the installed resolver, or nil.
func Doors() *DoorResolver {
	installedDoors.mu.RLock()
	defer installedDoors.mu.RUnlock()
	return installedDoors.d
}
