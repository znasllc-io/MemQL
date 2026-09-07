package memql

import (
	"strings"
	"testing"
	"time"
)

func pullNow() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) }

func onlineMachine() modelPullMachine {
	return modelPullMachine{
		RegistrationId:  "reg-1",
		OwnerUserId:     "v1:identity:user:alice",
		DisplayName:     "Studio",
		ConnectedNodeId: "agent-0",
		LastSeenAt:      pullNow().Add(-5 * time.Second).Format(time.RFC3339),
	}
}

// A machine that is not the caller's is refused with a code the surface can
// act on -- and refused BEFORE any row is written, so a probe leaves no trace
// on somebody else's machine.
func TestModelPullRefusesAMachineTheCallerDoesNotOwn(t *testing.T) {
	m := onlineMachine()
	m.OwnerUserId = "v1:identity:user:mallory"

	_, err := planModelPull(m, "v1:identity:user:alice", "llama3.1:8b", pullNow())
	if err == nil {
		t.Fatal("planModelPull accepted somebody else's machine")
	}
	if code := modelPullErrorCode(err); code != "not_your_machine" {
		t.Fatalf("error code = %q, want not_your_machine (%v)", code, err)
	}
}

// A revoked machine is refused for the same reason and with its own code: the
// registration row survives revocation as audit history, so it still resolves
// and would otherwise look like a machine you could pull to.
func TestModelPullRefusesARevokedMachine(t *testing.T) {
	m := onlineMachine()
	m.RevokedAt = pullNow().Add(-time.Hour).Format(time.RFC3339)

	_, err := planModelPull(m, m.OwnerUserId, "llama3.1:8b", pullNow())
	if code := modelPullErrorCode(err); code != "machine_revoked" {
		t.Fatalf("error code = %q, want machine_revoked (%v)", code, err)
	}
}

// ===========================================================================
// OFFLINE IS REFUSED HERE, NOT DISCOVERED LATER
// ===========================================================================
// A pull names ONE machine, so there is nothing to fall through to. Accepting
// the act against a machine no replica holds would write a row that sits at
// `requested` until a sweep fails it -- minutes of a spinner, ending in a
// failure whose cause was knowable at the moment of the press.
func TestModelPullRefusesAMachineNoReplicaHolds(t *testing.T) {
	m := onlineMachine()
	m.ConnectedNodeId = ""

	_, err := planModelPull(m, m.OwnerUserId, "llama3.1:8b", pullNow())
	if code := modelPullErrorCode(err); code != "machine_offline" {
		t.Fatalf("error code = %q, want machine_offline (%v)", code, err)
	}
}

// A STALE HEARTBEAT ALONE DOES NOT REFUSE, and that is deliberate rather than
// an oversight. `connectedNodeId` is blanked the instant a stream closes while
// `lastSeenAt` is left as stale as it truly is, so the node id is the fresher
// and more relevant signal -- and re-deriving the online window here would be a
// THIRD copy of a rule that has exactly two, in a package that cannot even
// import the first. This pins that reading so a later "completeness" edit has
// to argue with it.
func TestModelPullAcceptsAConnectedMachineWithAStaleHeartbeat(t *testing.T) {
	m := onlineMachine()
	m.LastSeenAt = pullNow().Add(-10 * time.Minute).Format(time.RFC3339)

	if _, err := planModelPull(m, m.OwnerUserId, "llama3.1:8b", pullNow()); err != nil {
		t.Fatalf("a connected machine with an old beat was refused: %v", err)
	}
}

// The model id crosses VERBATIM. The router selects on this exact string, so
// any normalisation here would leave a pulled model unreachable under the name
// it was pulled with.
func TestModelPullCarriesTheModelIdVerbatim(t *testing.T) {
	m := onlineMachine()
	const hf = "hf.co/TheBloke/Llama-2-70B-GGUF:Q4_K_M"

	plan, err := planModelPull(m, m.OwnerUserId, "  "+hf+"  ", pullNow())
	if err != nil {
		t.Fatalf("planModelPull: %v", err)
	}
	// Surrounding whitespace is a paste artefact and is trimmed; nothing
	// INSIDE the id is touched.
	if plan.Model != hf {
		t.Fatalf("model = %q, want %q", plan.Model, hf)
	}
}

func TestModelPullRefusesABlankModel(t *testing.T) {
	if _, err := planModelPull(onlineMachine(), "v1:identity:user:alice", "   ", pullNow()); err == nil {
		t.Fatal("planModelPull accepted a blank model")
	}
}

// THE CLAIM IS THE REPLICA THE MACHINE IS CONNECTED TO. Exactly one agent
// replica acts on the row, the one whose own node id this names, which is what
// makes the claim deterministic and lock-free.
func TestModelPullClaimsTheReplicaHoldingTheStream(t *testing.T) {
	m := onlineMachine()
	m.ConnectedNodeId = "agent-7"

	plan, err := planModelPull(m, m.OwnerUserId, "llama3.1:8b", pullNow())
	if err != nil {
		t.Fatalf("planModelPull: %v", err)
	}
	if plan.TargetNodeId != "agent-7" {
		t.Fatalf("targetNodeId = %q, want agent-7", plan.TargetNodeId)
	}
	if plan.PullId == "" {
		t.Fatal("a plan must carry the id its row will be written at")
	}
	if plan.RequestedAt != pullNow().UTC().Format(time.RFC3339) {
		t.Fatalf("requestedAt = %q, want the supplied clock", plan.RequestedAt)
	}
}

// The rendered call must be parseable MemQL with every value quoted through
// the lexer's own escaping. A model id is operator-typed -- pasted from a model
// card or a terminal -- so a stray control byte is an ordinary event, and Go's
// own quoting emits four escapes the MemQL lexer REJECTS.
func TestModelPullRendersAQuotedCall(t *testing.T) {
	plan := modelPullPlan{
		PullId:       "pull-1",
		WorkerId:     "reg-1",
		Model:        "llama3.1:8b",
		TargetNodeId: "agent-0",
		RequestedAt:  pullNow().UTC().Format(time.RFC3339),
	}
	call := renderCreateModelPullCall(plan)
	for _, want := range []string{
		`createModelPull(`,
		`model: "llama3.1:8b"`,
		`pullId: "pull-1"`,
		`targetNodeId: "agent-0"`,
		`workerId: "reg-1"`,
	} {
		if !strings.Contains(call, want) {
			t.Fatalf("rendered call missing %q:\n%s", want, call)
		}
	}
}
