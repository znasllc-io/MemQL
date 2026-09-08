package customdomain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/frontdoor"
	langparser "github.com/znasllc-io/memql/component/language/parser"
)

// The account front door's state machine, end to end, with no database, no
// cluster and no network.
//
// The fake engine PARSES every call for reconcile_test.go's reason: a suite
// that only records the strings a writer produced proves the writer called
// something, not that the engine could execute it. A mutation whose argument
// list does not parse fails here rather than as a warning in a log on a
// cluster nobody is watching.

type fakeDoorEngine struct {
	t *testing.T
	// doors answers the door reads; reservations answers the account read.
	doors        []map[string]any
	reservations []map[string]any
	calls        []string
	err          error
}

func (f *fakeDoorEngine) Execute(_ context.Context, query string) (any, error) {
	f.calls = append(f.calls, query)
	if _, perr := langparser.ParseExpression(query); perr != nil {
		f.t.Fatalf("the engine was handed a call it cannot parse:\n  %s\n  %v", query, perr)
	}
	if f.err != nil {
		return nil, f.err
	}
	switch {
	case strings.HasPrefix(query, "query accountsHoldingAReservedName"):
		return f.reservations, nil
	case strings.HasPrefix(query, "query accountFrontDoor"):
		// Every door read answers the same fixture, so a test can hand the
		// sweep a `live` row and assert it is skipped by the work loop while
		// still being seen by the reservation comparison. Narrowing the way
		// the real queries do would make that unmeasurable.
		return f.doors, nil
	default:
		return []map[string]any{}, nil
	}
}

func (f *fakeDoorEngine) wrote(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, "mutation "+prefix) {
			return true
		}
	}
	return false
}

// stubDoorResolver answers CheckPointing per host from a fixture.
type stubDoorResolver struct {
	// pointing maps a hostname to whether its CNAME reaches the edge host.
	pointing map[string]bool
}

func (s stubDoorResolver) LookupTXT(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (s stubDoorResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	if s.pointing[host] {
		return doorEdgeHost, nil
	}
	return "somewhere-else.example.", nil
}

func (s stubDoorResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if host == doorEdgeHost || s.pointing[host] {
		return []string{"203.0.113.10"}, nil
	}
	return []string{"198.51.100.7"}, nil
}

const (
	doorEdgeHost     = "os.memql.localhost"
	testDoorReserved = "memql.acme.com"
	testDoorAccount  = "acct-1"
)

type stubDoorProvisioner struct {
	outcome  Outcome
	err      error
	bound    int
	unbound  int
	gotPaths []string
}

func (p *stubDoorProvisioner) BindDoor(_ context.Context, _ DoorBindRequest, apiPaths []string) (Outcome, error) {
	p.bound++
	p.gotPaths = apiPaths
	return p.outcome, p.err
}

func (p *stubDoorProvisioner) UnbindDoor(_ context.Context, _ DoorBindRequest) (Outcome, error) {
	p.unbound++
	return p.outcome, p.err
}

func (p *stubDoorProvisioner) Describe() string { return "stub" }

func doorRow(status string) map[string]any {
	return map[string]any{
		"id":           "door-1",
		"accountId":    testDoorAccount,
		"reservedName": testDoorReserved,
		"status":       status,
	}
}

func newDoorReconciler(t *testing.T, eng *fakeDoorEngine, prov *stubDoorProvisioner, res Resolver) *DoorReconciler {
	t.Helper()
	r := NewDoorReconciler(NewDoorStore(eng), NewDoorAccountReader(eng), DoorConfig{
		EdgeHost:        doorEdgeHost,
		ACMEIssuer:      "letsencrypt-prod",
		Namespace:       "memql",
		IngressClass:    "nginx",
		EdgeService:     "edge",
		EdgePort:        8085,
		BFFHTTPService:  "bff-http",
		BFFHTTPPort:     8085,
		BFFGRPCService:  "bff",
		BFFGRPCPort:     50051,
		IdentityService: "identity",
		IdentityPort:    8085,
	}, prov, nil)
	r.resolver = res
	r.now = func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) }
	r.newID = func() string { return "door-new" }
	return r
}

// ===========================================================================
// Verification
// ===========================================================================

// ALL THREE HOSTS, OR NOTHING. Two pointing and one not must not advance.
func TestADoorWithOneHostMissingDoesNotIssue(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusPendingDNS)}}
	prov := &stubDoorProvisioner{}
	res := stubDoorResolver{pointing: map[string]bool{
		"app." + testDoorReserved: true,
		"api." + testDoorReserved: true,
		// id. is missing.
	}}

	out, err := newDoorReconciler(t, eng, prov, res).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Verified != 0 || prov.bound != 0 {
		t.Errorf("verified=%d bound=%d: two of three hosts is not a front door", out.Verified, prov.bound)
	}
	if !eng.wrote("recordAccountFrontDoorCheck") {
		t.Error("the pass recorded no check, so the rail cannot say which CNAME is wrong")
	}
}

// EVERY HOST IS CHECKED ON EVERY PASS, even after the first miss -- so the
// rail can hand an operator the whole list at once instead of one record every
// two minutes.
func TestEveryHostIsReportedEvenAfterTheFirstMiss(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusVerifying)}}
	res := stubDoorResolver{pointing: map[string]bool{}} // none of the three

	if _, err := newDoorReconciler(t, eng, &stubDoorProvisioner{}, res).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var check string
	for _, c := range eng.calls {
		if strings.HasPrefix(c, "mutation recordAccountFrontDoorCheck") {
			check = c
		}
	}
	if check == "" {
		t.Fatal("no check was recorded")
	}
	for _, role := range frontdoor.AccountRoles() {
		if !strings.Contains(check, `"`+string(role)+`"`) {
			t.Errorf("the recorded hostChecks does not mention the %q host:\n  %s", role, check)
		}
	}
}

func TestAllThreeHostsPointingReachesIssuing(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusPendingDNS)}}
	res := stubDoorResolver{pointing: map[string]bool{
		"app." + testDoorReserved: true,
		"api." + testDoorReserved: true,
		"id." + testDoorReserved:  true,
	}}
	prov := &stubDoorProvisioner{}

	out, err := newDoorReconciler(t, eng, prov, res).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Verified != 1 {
		t.Errorf("verified = %d, want 1", out.Verified)
	}
	if !eng.wrote("markAccountFrontDoorVerified") {
		t.Error("the door did not reach issuing")
	}
	// ONE STATE PER PASS: verifying must not also dispatch the bind.
	if prov.bound != 0 {
		t.Errorf("the bind was dispatched %d time(s) in the same pass that verified; the row is the state machine, not a local variable", prov.bound)
	}
}

// ===========================================================================
// Issuance
// ===========================================================================

func TestACertificateThatIsNotReadyIsNotAFailure(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusIssuing)}}
	prov := &stubDoorProvisioner{outcome: Outcome{Applied: true, Note: "Waiting for http-01 challenge propagation"}}

	if _, err := newDoorReconciler(t, eng, prov, stubDoorResolver{}).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !eng.wrote("recordAccountFrontDoorIssuingProgress") {
		t.Error("an unready certificate was not recorded as progress")
	}
	if eng.wrote("recordAccountFrontDoorIssuanceFailure") {
		t.Error("waiting for a three-name ACME order was recorded as a failure, which makes an ordinary wait look like something to fix")
	}
}

func TestAReadyCertificateGoesLive(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusIssuing)}}
	prov := &stubDoorProvisioner{outcome: Outcome{Applied: true, CertificateReady: true}}

	out, err := newDoorReconciler(t, eng, prov, stubDoorResolver{}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Issued != 1 || !eng.wrote("markAccountFrontDoorLive") {
		t.Errorf("issued = %d, live written = %v", out.Issued, eng.wrote("markAccountFrontDoorLive"))
	}
}

// A cluster with no ACME issuer sits at `issuing` carrying the typed reason.
// That is the correct local answer (custom domains D7), not a stuck state.
func TestNoAcmeIssuerIsRecordedAndNotPromoted(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusIssuing)}}
	prov := &stubDoorProvisioner{outcome: Outcome{Reason: ReasonNoACMEIssuer, Detail: "this cluster declares no ACME issuer"}}

	if _, err := newDoorReconciler(t, eng, prov, stubDoorResolver{}).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if eng.wrote("markAccountFrontDoorLive") {
		t.Fatal("a door went live with no certificate")
	}
	var failure string
	for _, c := range eng.calls {
		if strings.HasPrefix(c, "mutation recordAccountFrontDoorIssuanceFailure") {
			failure = c
		}
	}
	if !strings.Contains(failure, ReasonNoACMEIssuer) {
		t.Errorf("the typed reason did not reach the row:\n  %s", failure)
	}
}

// The provisioner is handed the GENERATED path list, not one of the
// reconciler's own. Two lists would mean the next HTTP route added to the bff
// reaches the cluster's api host and no client's.
func TestTheProvisionerIsHandedTheGeneratedPathList(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusIssuing)}}
	prov := &stubDoorProvisioner{outcome: Outcome{Applied: true, CertificateReady: true}}

	if _, err := newDoorReconciler(t, eng, prov, stubDoorResolver{}).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.gotPaths) == 0 {
		t.Fatal("the provisioner was handed no paths, so a door's api host would route nothing over HTTP")
	}
	if len(prov.gotPaths) != len(frontdoor.BFFHTTPPaths) {
		t.Errorf("the provisioner got %d paths, component/frontdoor holds %d", len(prov.gotPaths), len(frontdoor.BFFHTTPPaths))
	}
}

// ===========================================================================
// D9 -- the withdrawn reservation
// ===========================================================================

// THE ONLY ARROW OUT OF `live`. A door whose account no longer holds the name
// is the cluster serving three hostnames whose ownership proof was discarded.
func TestAWithdrawnReservationTearsALiveDoorDown(t *testing.T) {
	eng := &fakeDoorEngine{
		t:            t,
		doors:        []map[string]any{doorRow(StatusLive)},
		reservations: []map[string]any{}, // the account holds nothing any more
	}
	prov := &stubDoorProvisioner{}

	if _, err := newDoorReconciler(t, eng, prov, stubDoorResolver{}).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var removal string
	for _, c := range eng.calls {
		if strings.HasPrefix(c, "mutation requestAccountFrontDoorRemoval") {
			removal = c
		}
	}
	if removal == "" {
		t.Fatal("a live door whose reservation is gone was left serving")
	}
	if !strings.Contains(removal, ReasonReservationGone) {
		t.Errorf("the teardown carries no typed reason, so the rail cannot say why:\n  %s", removal)
	}
}

// A reservation that MOVED is the same conclusion for the same reason: proof
// of one name is not proof of another.
func TestAChangedReservationTearsTheOldDoorDown(t *testing.T) {
	eng := &fakeDoorEngine{
		t:     t,
		doors: []map[string]any{doorRow(StatusLive)},
		reservations: []map[string]any{{
			"id":              testDoorAccount,
			"memqlDomain":     "memql.newname.com",
			"memqlReservedAt": "2026-09-08T00:00:00Z",
		}},
	}

	if _, err := newDoorReconciler(t, eng, &stubDoorProvisioner{}, stubDoorResolver{}).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !eng.wrote("requestAccountFrontDoorRemoval") {
		t.Error("the door for the OLD name was left serving after the reservation moved")
	}
	// And it must NOT open the new door in the same pass: the old one is still
	// holding the certificate's secret name.
	if eng.wrote("createAccountFrontDoor") {
		t.Error("a door for the new name was opened while the old one was still up")
	}
}

// A door already coming down must not be asked to come down again on every
// pass -- which would rewrite the row every two minutes and make the rail
// flicker for a door that had not moved.
func TestADoorAlreadyRemovingIsNotAskedAgain(t *testing.T) {
	eng := &fakeDoorEngine{t: t, doors: []map[string]any{doorRow(StatusRemoving)}, reservations: []map[string]any{}}
	prov := &stubDoorProvisioner{outcome: Outcome{Applied: true}}

	out, err := newDoorReconciler(t, eng, prov, stubDoorResolver{}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if eng.wrote("requestAccountFrontDoorRemoval") {
		t.Error("a door already at `removing` was asked to remove again")
	}
	if out.Removed != 1 || prov.unbound != 1 {
		t.Errorf("removed=%d unbound=%d: the removing door should have been unbound and closed", out.Removed, prov.unbound)
	}
}

// ===========================================================================
// Opening
// ===========================================================================

func TestAHeldReservationWithNoDoorOpensOne(t *testing.T) {
	eng := &fakeDoorEngine{
		t:     t,
		doors: []map[string]any{},
		reservations: []map[string]any{{
			"id":              testDoorAccount,
			"memqlDomain":     testDoorReserved,
			"memqlReservedAt": "2026-09-08T00:00:00Z",
		}},
	}

	out, err := newDoorReconciler(t, eng, &stubDoorProvisioner{}, stubDoorResolver{}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Opened != 1 || !eng.wrote("createAccountFrontDoor") {
		t.Errorf("opened = %d, create written = %v", out.Opened, eng.wrote("createAccountFrontDoor"))
	}
}

// A NAME WITH NO memqlReservedAt IS NOT HELD (record A, D10): the stamp lands
// in the same write that verifies the domain, and only when the guardrails
// passed. Opening a door for an unstamped name would request a certificate for
// a host whose ownership is unproven.
func TestAnUnstampedNameOpensNothing(t *testing.T) {
	eng := &fakeDoorEngine{
		t:     t,
		doors: []map[string]any{},
		reservations: []map[string]any{{
			"id":          testDoorAccount,
			"memqlDomain": testDoorReserved,
			// no memqlReservedAt
		}},
	}

	out, err := newDoorReconciler(t, eng, &stubDoorProvisioner{}, stubDoorResolver{}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Opened != 0 || eng.wrote("createAccountFrontDoor") {
		t.Error("a door was opened for a name this cluster has not agreed to serve")
	}
}

func TestAReservationThatAlreadyHasADoorOpensNothing(t *testing.T) {
	eng := &fakeDoorEngine{
		t:     t,
		doors: []map[string]any{doorRow(StatusLive)},
		reservations: []map[string]any{{
			"id":              testDoorAccount,
			"memqlDomain":     testDoorReserved,
			"memqlReservedAt": "2026-09-08T00:00:00Z",
		}},
	}

	if _, err := newDoorReconciler(t, eng, &stubDoorProvisioner{}, stubDoorResolver{}).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if eng.wrote("createAccountFrontDoor") {
		t.Error("a second door was opened for an account that already has one -- a second certificate order for the same three names")
	}
	if eng.wrote("requestAccountFrontDoorRemoval") {
		t.Error("a door matching its own reservation was asked to tear down")
	}
}

// ===========================================================================
// Object naming
// ===========================================================================

// The Go provisioner and both scripts must name the same objects, or a bind
// and its unbind act on different things and leave Ingresses serving a name
// the cluster no longer claims. The script half is pinned in
// scripts/deploy/account_front_door_test.go against these same literals.
func TestTheGoProvisionerNamesTheSameObjectsTheScriptDoes(t *testing.T) {
	if got, want := doorObjectName("acct-test"), "account-front-door-acct-test"; got != want {
		t.Errorf("doorObjectName = %q, want %q", got, want)
	}
	req := DoorBindRequest{AccountID: "acct-test", ReservedName: testDoorReserved, Namespace: "memql", Issuer: "le", IngressClass: "nginx"}

	names := map[string]bool{}
	for _, obj := range doorIngressObjects(req, []string{"/healthz"}) {
		meta, _ := obj["metadata"].(map[string]any)
		name, _ := meta["name"].(string)
		names[name] = true
	}
	for _, suffix := range doorIngressSuffixes {
		if !names["account-front-door-acct-test"+suffix] {
			t.Errorf("no Ingress named with the %q suffix, but the unbind deletes one", suffix)
		}
	}
}

// The certificate names all three hosts, which is what makes activation
// all-or-nothing without a rule for the reconciler to police.
func TestTheCertificateNamesEveryHost(t *testing.T) {
	obj := DoorCertificateObject(DoorBindRequest{AccountID: "acct-test", ReservedName: testDoorReserved, Issuer: "le"})
	spec, _ := obj["spec"].(map[string]any)
	sans, _ := spec["dnsNames"].([]any)
	if len(sans) != len(frontdoor.AccountRoles()) {
		t.Fatalf("the certificate names %d hosts, the door serves %d", len(sans), len(frontdoor.AccountRoles()))
	}
	for i, want := range frontdoor.AccountCertificateSANs(testDoorReserved) {
		if got, _ := sans[i].(string); got != want {
			t.Errorf("dnsName %d = %q, want %q", i, got, want)
		}
	}
}

// The api. host gets TWO Ingresses, and only one of them is the gRPC one.
// Backend protocol is a per-Service annotation, so they cannot share an object.
func TestTheApiHostGetsBothBackends(t *testing.T) {
	req := DoorBindRequest{AccountID: "a", ReservedName: testDoorReserved, IngressClass: "nginx",
		BFFHTTPService: "bff-http", BFFHTTPPort: 8085, BFFGRPCService: "bff", BFFGRPCPort: 50051}

	var grpc, http int
	apiHost := frontdoor.AccountRoleHost(frontdoor.AccountRoleAPI, testDoorReserved)
	for _, obj := range doorIngressObjects(req, []string{"/healthz", "/memql/ws"}) {
		spec, _ := obj["spec"].(map[string]any)
		rules, _ := spec["rules"].([]any)
		rule, _ := rules[0].(map[string]any)
		if rule["host"] != apiHost {
			continue
		}
		meta, _ := obj["metadata"].(map[string]any)
		ann, _ := meta["annotations"].(map[string]any)
		if ann["nginx.ingress.kubernetes.io/backend-protocol"] == "GRPC" {
			grpc++
		} else {
			http++
		}
	}
	if grpc != 1 || http != 1 {
		t.Errorf("the api host has %d gRPC and %d HTTP Ingress(es), want one of each", grpc, http)
	}
}

// No paths must produce a door WITHOUT an api HTTP Ingress, not one carrying
// an empty rule list -- which the API server rejects, taking the other three
// hosts down with it.
func TestNoPathsOmitsTheHttpIngressRatherThanEmptyingIt(t *testing.T) {
	req := DoorBindRequest{AccountID: "a", ReservedName: testDoorReserved, IngressClass: "nginx"}
	objs := doorIngressObjects(req, nil)
	if len(objs) != 3 {
		t.Fatalf("got %d Ingresses for a door with no HTTP paths, want 3", len(objs))
	}
	for _, obj := range objs {
		spec, _ := obj["spec"].(map[string]any)
		rules, _ := spec["rules"].([]any)
		rule, _ := rules[0].(map[string]any)
		httpBlock, _ := rule["http"].(map[string]any)
		paths, _ := httpBlock["paths"].([]any)
		if len(paths) == 0 {
			meta, _ := obj["metadata"].(map[string]any)
			t.Errorf("Ingress %v carries a zero-length paths list, which the API server rejects", meta["name"])
		}
	}
}
