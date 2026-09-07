//go:build agent

package worker

// THE MODEL-PULL HOP, tested in process (epic memql#5103, design D3).
//
// Same construction as model_forward_hop_test.go and for the same reason: the
// real ForwardRouter wired to the real ForwardHandler through a link carrying
// what NodeService.Stream carries. A live-cluster version would be skipped on
// every CI lane and every developer machine, and a gate skipped by default
// cannot be what stands between a feature and the bug it prevents.
//
// The bug it prevents is worse here than for a model call, because a pull has
// no second machine to fall back to. A person presses Pull on ONE machine's
// page. At the default two replicas the machine's stream is on the other one
// half the time, and without the hop that press does nothing at all -- no
// download, no error a person could act on, just a button that appears not to
// work every other time it is pressed.
//
// TO CONFIRM THESE ARE LOAD-BEARING: make the handler skip its registration
// check, or have relayPullProgress fold the per-layer counters, and they fail.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	workerservice "github.com/znasllc-io/memql/component/worker"
)

const pullModel = "llama3.1:70b"

// pullServeFn is what the machine "runs": handed the request and a sink for
// observations, it returns the terminal outcome.
type pullServeFn func(ctx context.Context, req workerservice.ModelPullRequest, emit func(workerservice.ModelPullProgress)) workerservice.ModelPullOutcome

type pullHop struct {
	link  *meshLink
	store *fakeStore
	owner string
}

// newPullHop stands up both replicas plus the link, with a machine on node B
// that advertises NO models at all -- which is the honest fixture: a pull's
// whole purpose is to put a model on a machine that has not got it.
func newPullHop(t *testing.T, serve pullServeFn, mutate func(*Candidate, *workerservice.Worker)) *pullHop {
	t.Helper()
	owner := "v1:identity:user:alice"

	regB := workerservice.NewRegistry(testLogger(), fleetNow)
	w := &workerservice.Worker{
		RegistrationId: "laptop",
		OwnerUserId:    owner,
		Name:           "laptop",
		Capabilities:   []string{workerservice.CapabilityHeadless, workerservice.ModelCapability},
		Labels: map[string]string{
			workerservice.RuntimeLabelPrefix + "ollama": "1",
		},
		Concurrency: map[string]uint32{workerservice.ModelCapability: 2},
	}
	w.SetModelPullFunc(func(ctx context.Context, req workerservice.ModelPullRequest) (*workerservice.ModelPullHandle, error) {
		runCtx, stop := context.WithCancel(ctx)
		h, emit, finish := workerservice.NewModelPullLoopback(req, func(string) { stop() })
		go func() {
			defer stop()
			finish(serve(runCtx, req, emit))
		}()
		return h, nil
	})

	cand := machine("laptop")
	cand.ConnectedNodeId = nodeB
	cand.Capabilities = []string{workerservice.CapabilityHeadless, workerservice.ModelCapability}
	if mutate != nil {
		mutate(&cand, w)
	}
	regB.Add(w)

	store := &fakeStore{fakeFleet: &fakeFleet{machines: []Candidate{cand}, owner: owner}}
	link := &meshLink{t: t, reachable: true}
	link.handler = NewForwardHandler(regB, store, testLogger())
	link.router = newForwardRouter(link, func() (string, string) { return nodeA, "agent" }, testLogger())
	return &pullHop{link: link, store: store, owner: owner}
}

// --- the hop ----------------------------------------------------------------

func TestAModelPullReachesAMachineHeldByAnotherReplica(t *testing.T) {
	h := newPullHop(t, func(_ context.Context, _ workerservice.ModelPullRequest, emit func(workerservice.ModelPullProgress)) workerservice.ModelPullOutcome {
		emit(workerservice.ModelPullProgress{Layer: "sha256:aaa", CompletedBytes: 500, TotalBytes: 1000, Status: "pulling aaa"})
		emit(workerservice.ModelPullProgress{Layer: "sha256:aaa", CompletedBytes: 1000, TotalBytes: 1000, Status: "pulling aaa"})
		return workerservice.ModelPullOutcome{Ok: true, Model: pullModel, Readvertised: true}
	}, nil)

	var mu sync.Mutex
	var seen []workerservice.ModelPullProgress
	out, err := h.link.router.ForwardModelPull(
		authorityCtx(t, h.owner), nodeB, "laptop", h.owner, pullModel, time.Minute,
		func(p workerservice.ModelPullProgress) {
			mu.Lock()
			seen = append(seen, p)
			mu.Unlock()
		})
	h.link.wg.Wait()
	if err != nil {
		t.Fatalf("ForwardModelPull: %v", err)
	}
	if !out.Ok {
		t.Fatalf("outcome = %+v, want ok", out)
	}
	if !out.Readvertised {
		t.Fatalf("readvertised must cross the hop: %+v", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("expected 2 observations across the hop, got %d: %+v", len(seen), seen)
	}
	if seen[0].Status != "pulling aaa" || seen[0].CompletedBytes != 500 {
		t.Fatalf("first observation crossed as %+v", seen[0])
	}
}

// ===========================================================================
// THE COUNTERS RIDE THROUGH UNTOUCHED, INCLUDING WHEN THEY GO BACKWARDS
// ===========================================================================
// Ollama reports completed/total PER BLOB and restarts at zero for each. The
// hop must not fold, smooth or monotonise them: doing so would put a second,
// different progress model on the receiving side from the one every other
// reader sees, and the reader that could notice the reset would be looking at
// numbers this node invented.
func TestAForwardedPullRelaysPerLayerCountersWithoutSmoothingThem(t *testing.T) {
	h := newPullHop(t, func(_ context.Context, _ workerservice.ModelPullRequest, emit func(workerservice.ModelPullProgress)) workerservice.ModelPullOutcome {
		emit(workerservice.ModelPullProgress{Layer: "a", CompletedBytes: 990, TotalBytes: 1000, Status: "pulling a"})
		emit(workerservice.ModelPullProgress{Layer: "b", CompletedBytes: 5, TotalBytes: 900000, Status: "pulling b"})
		emit(workerservice.ModelPullProgress{Status: "verifying sha256 digest"})
		return workerservice.ModelPullOutcome{Ok: true, Model: pullModel}
	}, nil)

	var mu sync.Mutex
	var seen []workerservice.ModelPullProgress
	if _, err := h.link.router.ForwardModelPull(
		authorityCtx(t, h.owner), nodeB, "laptop", h.owner, pullModel, time.Minute,
		func(p workerservice.ModelPullProgress) {
			mu.Lock()
			seen = append(seen, p)
			mu.Unlock()
		}); err != nil {
		t.Fatalf("ForwardModelPull: %v", err)
	}
	h.link.wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("expected all 3 observations, got %d: %+v", len(seen), seen)
	}
	if seen[1].CompletedBytes != 5 || seen[1].TotalBytes != 900000 || seen[1].Layer != "b" {
		t.Fatalf("the backwards step was rewritten: %+v", seen[1])
	}
	if seen[2].Layer != "" || seen[2].Status != "verifying sha256 digest" {
		t.Fatalf("a non-download phase was altered: %+v", seen[2])
	}
}

// ===========================================================================
// THE RECEIVER RE-CHECKS OWNERSHIP AGAINST THE VERIFIED AUTHORITY
// ===========================================================================
// This is the ownership boundary holding across the hop rather than only on
// the sender -- and here it is the last gate before a side effect on hardware
// the cluster does not own: a pull writes gigabytes to somebody's disk and
// edits their policy.yaml.
func TestAForwardedPullIsRefusedForAMachineTheAuthorityDoesNotOwn(t *testing.T) {
	h := newPullHop(t, func(_ context.Context, _ workerservice.ModelPullRequest, _ func(workerservice.ModelPullProgress)) workerservice.ModelPullOutcome {
		t.Error("the machine must not be reached for a caller who does not own it")
		return workerservice.ModelPullOutcome{Ok: true}
	}, func(_ *Candidate, w *workerservice.Worker) {
		w.OwnerUserId = "v1:identity:user:mallory"
	})

	out, err := h.link.router.ForwardModelPull(
		authorityCtx(t, h.owner), nodeB, "laptop", h.owner, pullModel, time.Minute, nil)
	h.link.wg.Wait()
	if err != nil {
		t.Fatalf("ForwardModelPull: %v", err)
	}
	if out.Ok {
		t.Fatal("a pull for somebody else's machine was accepted")
	}
	if out.ErrorCode != "owner_mismatch" {
		t.Fatalf("error code = %q, want owner_mismatch (got %+v)", out.ErrorCode, out)
	}
}

// A pull forwarded with no verifiable assertion is refused rather than sent.
// The envelope's owner field is a hint for reading the fleet and can never be
// what decides: a node that trusted it would let any replica name any owner.
func TestAForwardedPullWithoutAnAuthorityIsRefusedBeforeItLeaves(t *testing.T) {
	h := newPullHop(t, func(_ context.Context, _ workerservice.ModelPullRequest, _ func(workerservice.ModelPullProgress)) workerservice.ModelPullOutcome {
		t.Error("nothing should have reached the machine")
		return workerservice.ModelPullOutcome{}
	}, nil)

	out, err := h.link.router.ForwardModelPull(
		context.Background(), nodeB, "laptop", h.owner, pullModel, time.Minute, nil)
	h.link.wg.Wait()
	if err != nil {
		t.Fatalf("ForwardModelPull: %v", err)
	}
	if out.ErrorCode != "no_forwarded_authority" {
		t.Fatalf("error code = %q, want no_forwarded_authority", out.ErrorCode)
	}
}

// A machine whose cockpit predates the feature has no pull hook. The refusal
// must NAME that, because the fix -- update the cockpit -- is one no other
// refusal on this path implies.
func TestAForwardedPullToACockpitThatCannotPullSaysSo(t *testing.T) {
	h := newPullHop(t, nil, func(_ *Candidate, w *workerservice.Worker) {
		w.SetModelPullFunc(nil)
	})

	out, err := h.link.router.ForwardModelPull(
		authorityCtx(t, h.owner), nodeB, "laptop", h.owner, pullModel, time.Minute, nil)
	h.link.wg.Wait()
	if err != nil {
		t.Fatalf("ForwardModelPull: %v", err)
	}
	if out.Ok {
		t.Fatal("a pull to a machine with no pull support reported success")
	}
	if out.ErrorCode != "model_pull_refused" || !strings.Contains(out.ErrorMessage, "does not support model pulls") {
		t.Fatalf("refusal did not name the cockpit's missing support: %+v", out)
	}
}

// A pull that fails INSIDE a successful stream -- the runtime reporting an
// error in the body of an HTTP 200 -- must cross the hop as an answer, not as
// a transport failure. Otherwise "no space left on device" is indistinguishable
// from the machine falling off the network.
func TestAForwardedPullRelaysAnInBandFailureAsAnAnswer(t *testing.T) {
	h := newPullHop(t, func(_ context.Context, _ workerservice.ModelPullRequest, emit func(workerservice.ModelPullProgress)) workerservice.ModelPullOutcome {
		emit(workerservice.ModelPullProgress{Layer: "a", CompletedBytes: 10, TotalBytes: 1000, Status: "pulling a"})
		return workerservice.ModelPullOutcome{Ok: false, Model: pullModel, Error: "write /root/.ollama: no space left on device"}
	}, nil)

	out, err := h.link.router.ForwardModelPull(
		authorityCtx(t, h.owner), nodeB, "laptop", h.owner, pullModel, time.Minute, nil)
	h.link.wg.Wait()
	if err != nil {
		t.Fatalf("an in-band failure surfaced as a transport error: %v", err)
	}
	if out.Ok {
		t.Fatalf("outcome = %+v, want not ok", out)
	}
	if !strings.Contains(out.ErrorMessage, "no space left on device") {
		t.Fatalf("the runtime's own words did not cross the hop: %+v", out)
	}
	// AND the failure is not dressed as a refusal. Nothing here is
	// re-pickable: the person named this machine.
	if out.ErrorCode != "model_pull_failed" {
		t.Fatalf("error code = %q, want model_pull_failed", out.ErrorCode)
	}
}

// An unreachable peer is reported rather than hung on. A pull names one
// machine, so there is nothing to fall through to -- which makes saying so
// promptly the whole of the remedy.
func TestAForwardedPullToAnUnreachablePeerIsReportedNotHung(t *testing.T) {
	h := newPullHop(t, nil, nil)
	h.link.mu.Lock()
	h.link.reachable = false
	h.link.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := h.link.router.ForwardModelPull(
			authorityCtx(t, h.owner), nodeB, "laptop", h.owner, pullModel, time.Minute, nil); err == nil {
			t.Error("an unreachable peer reported success")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ForwardModelPull hung on an unreachable peer")
	}
}
