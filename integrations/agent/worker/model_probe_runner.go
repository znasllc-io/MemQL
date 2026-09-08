//go:build agent

package worker

// The MODEL PROBE RUNNER (epic memql#5146, design D3).
//
// ===========================================================================
// THE PULL RUNNER'S SHAPE, AND WHERE IT DIFFERS
// ===========================================================================
// Same claim, same hop, same borrowed authority, same "the row write never
// happens on the caller's goroutine" rule. What is different is small and worth
// naming, because each difference is a decision rather than an omission:
//
//   - PROGRESS IS NOT THROTTLED. A pull emits several observations a second and
//     each is a row version, so it needs a throttle. A suite emits TEN in total,
//     and every one answers the question a person watching actually has --
//     which case is slow -- so all ten are written.
//
//   - AN OBSERVATION IS NEVER DROPPED. The pull drops under pressure because a
//     progress bar's whole value is being current and the next observation
//     carries the same question a moment later. A probe's observations are
//     DISTINCT: dropping "structured.symptom failed" loses the only record that
//     it did.
//
//   - THE TERMINAL WRITE IS TWO ROWS, not one. The figures land on
//     v1:platform:modelMeasurement and the probe row points at them. A probe
//     that crashed writes the failed probe row and NO measurement, which is
//     exactly right: nothing was measured, and a measurement row full of
//     absences would be a fifth way of saying so.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/events"
	langparser "github.com/znasllc-io/memql/component/language/parser"
	workerservice "github.com/znasllc-io/memql/component/worker"
	"github.com/znasllc-io/memql/component/worker/probe"
	"github.com/znasllc-io/memql/core/id"
)

// ModelProbeTopic is the create event the runner claims on.
const ModelProbeTopic = "graph.node.created.v1:worker:modelProbe"

// modelProbeWriteBuffer bounds the progress channel. Sized to the suite so an
// ordinary run never blocks and never drops -- see the header for why dropping
// is wrong here where it is right for a pull.
const modelProbeWriteBuffer = 32

// modelProbeRow is the part of a v1:worker:modelProbe row the runner acts on.
type modelProbeRow struct {
	ProbeId      string
	OwnerUserId  string
	WorkerId     string
	Model        string
	SuiteVersion string
	Status       string
	TargetNodeId string
}

func modelProbeRowFromEvent(ev events.Event) (modelProbeRow, bool) {
	if ev.Payload == nil {
		return modelProbeRow{}, false
	}
	fields, _ := ev.Payload["payload"].(map[string]any)
	if fields == nil {
		// Some producers flatten. Reading both is cheaper than deciding which
		// path an event came through, and the alternative fails silently -- the
		// reasoning modelPullRowFromEvent already records.
		fields = ev.Payload
	}
	row := modelProbeRow{
		ProbeId:      eventString(ev.Payload, "id"),
		OwnerUserId:  eventString(fields, "ownerUserId"),
		WorkerId:     eventString(fields, "workerId"),
		Model:        eventString(fields, "model"),
		SuiteVersion: eventString(fields, "suiteVersion"),
		Status:       eventString(fields, "status"),
		TargetNodeId: eventString(fields, "targetNodeId"),
	}
	if row.ProbeId == "" {
		row.ProbeId = eventString(fields, "id")
	}
	if row.ProbeId == "" || row.WorkerId == "" || row.Model == "" || row.OwnerUserId == "" {
		return modelProbeRow{}, false
	}
	return row, true
}

// probeClaimedBy is the deterministic claim: exactly one replica acts, the one
// whose own id the row names.
//
// An EMPTY targetNodeId is claimed by NOBODY rather than by everybody. The
// alternative -- treating it as "any replica may take this" -- turns a row
// written before the machine's node was known into a race between replicas, and
// the stale sweep is the honest way to close it instead.
func probeClaimedBy(row modelProbeRow, selfNodeId string) bool {
	target := strings.TrimSpace(row.TargetNodeId)
	return target != "" && target == strings.TrimSpace(selfNodeId)
}

// ModelProbeForwarder is the hop, kept as an interface so the runner can be
// tested against a fake peer without a mesh.
type ModelProbeForwarder interface {
	ForwardModelProbe(
		ctx context.Context,
		nodeId string,
		registrationId string,
		ownerUserId string,
		model string,
		suiteVersion string,
		timeout time.Duration,
		onProgress func(workerservice.ModelProbeProgress),
	) (ModelProbeOutcome, error)
}

// ModelProbeOutcome is the terminal answer of a forwarded probe.
type ModelProbeOutcome struct {
	Model        string
	Ok           bool
	SuiteVersion string
	ErrorCode    string
	ErrorMessage string
	Figures      probe.Figures
}

// ModelProbeRunner drives this replica's model probes.
type ModelProbeRunner struct {
	dispatcher *Dispatcher
	forwarder  ModelProbeForwarder
	selfNodeId string
	clock      func() time.Time

	mu      sync.Mutex
	running map[string]struct{}
}

// NewModelProbeRunner builds the runner for one agent replica.
func NewModelProbeRunner(d *Dispatcher, forwarder ModelProbeForwarder, selfNodeId string) *ModelProbeRunner {
	if d == nil {
		return nil
	}
	return &ModelProbeRunner{
		dispatcher: d,
		forwarder:  forwarder,
		selfNodeId: selfNodeId,
		clock:      time.Now,
		running:    map[string]struct{}{},
	}
}

// Subscribe registers the handler on the event bus and returns the unsubscribe.
func (r *ModelProbeRunner) Subscribe(bus *events.Bus) func() {
	if r == nil || bus == nil {
		return func() {}
	}
	return bus.Subscribe(ModelProbeTopic, r.HandleModelProbeCreated,
		events.WithSubscriberName("agentworker:model-probe"))
}

// HandleModelProbeCreated claims a requested probe for this replica and drives it.
func (r *ModelProbeRunner) HandleModelProbeCreated(ev events.Event) {
	if r == nil {
		return
	}
	row, ok := modelProbeRowFromEvent(ev)
	if !ok || row.Status != "requested" {
		return
	}
	if !probeClaimedBy(row, r.selfNodeId) {
		return
	}
	if !r.begin(row.ProbeId) {
		return
	}
	go func() {
		defer r.end(row.ProbeId)
		// The probe's own context, NOT the event's: an event handler's context
		// is scoped to delivering the event, and a suite outlives it by
		// minutes.
		ctx, cancel := context.WithTimeout(context.Background(), workerservice.DefaultModelProbeTimeout)
		defer cancel()
		r.drive(ctx, row)
	}()
}

func (r *ModelProbeRunner) begin(probeId string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, live := r.running[probeId]; live {
		return false
	}
	r.running[probeId] = struct{}{}
	return true
}

func (r *ModelProbeRunner) end(probeId string) {
	r.mu.Lock()
	delete(r.running, probeId)
	r.mu.Unlock()
}

// drive runs one probe to its end.
//
// THE ROW WRITE NEVER HAPPENS ON THE CALLER'S GOROUTINE, the pull runner's
// hard-won rule: `onProgress` is reached inline from a peer connection's
// receive callback on the forwarded path, so a synchronous engine.Execute there
// stalls every inbound message from that peer for the length of a graph
// mutation.
//
// Unlike the pull, the send BLOCKS rather than dropping when the writer is
// behind, and the buffer is sized to the suite so it does not in practice. A
// probe's observations are distinct facts -- "structured.symptom failed" is the
// only record that it did -- where a pull's are successive readings of one
// number.
func (r *ModelProbeRunner) drive(ctx context.Context, row modelProbeRow) {
	writes := make(chan workerservice.ModelProbeProgress, modelProbeWriteBuffer)
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for p := range writes {
			r.recordProgress(ctx, row, p)
		}
	}()

	onProgress := func(p workerservice.ModelProbeProgress) {
		select {
		case writes <- p:
		case <-ctx.Done():
		}
	}

	outcome, err := r.probe(ctx, row, onProgress)
	close(writes)
	writer.Wait()

	switch {
	case err != nil:
		r.finish(ctx, row, "failed", "", err.Error())
	case !outcome.Ok && !anyFigureMeasured(outcome.Figures):
		// Nothing was measured. There is no measurement row to write, and
		// writing one full of absences would be a fifth way of saying what the
		// failed probe row already says.
		message := strings.TrimSpace(outcome.ErrorMessage)
		if message == "" {
			message = "The machine reported that the probe did not finish, and said no more than that."
		}
		r.finish(ctx, row, "failed", "", message)
	default:
		// SOME CASES RAN. That is true whether `ok` is set or not, and the
		// measurement is written either way: figures that exist are worth
		// keeping, and each one says for itself whether it was measured. The
		// probe row still records the failure sentence beside the measurement
		// it produced, which is the pair a person needs to read.
		measurementId, writeErr := r.recordMeasurement(ctx, row, outcome)
		status := "succeeded"
		message := strings.TrimSpace(outcome.ErrorMessage)
		if !outcome.Ok {
			status = "failed"
		}
		if writeErr != nil {
			status = "failed"
			measurementId = ""
			message = "The suite ran but its figures could not be stored: " + writeErr.Error()
		}
		r.finish(ctx, row, status, measurementId, message)
	}
}

// anyFigureMeasured reports whether the machine returned anything at all worth
// storing.
func anyFigureMeasured(f probe.Figures) bool {
	for _, fig := range []probe.Figure{f.StructuredValidity, f.ToolCallCorrectness, f.ThroughputTps, f.TimeToFirstTokenMs} {
		if fig.IsMeasured() {
			return true
		}
	}
	return false
}

// probe reaches the machine, locally or across the hop.
func (r *ModelProbeRunner) probe(
	ctx context.Context,
	row modelProbeRow,
	onProgress func(workerservice.ModelProbeProgress),
) (ModelProbeOutcome, error) {
	if w := r.dispatcher.registry.WorkerById(row.WorkerId); w != nil {
		return r.probeLocally(ctx, w, row, onProgress)
	}

	ownerCtx := r.probeOwnerActor(ctx, row.OwnerUserId)
	target, err := r.currentNodeForProbe(ownerCtx, row)
	if err != nil {
		return ModelProbeOutcome{Model: row.Model}, err
	}
	if target == "" {
		return ModelProbeOutcome{Model: row.Model}, fmt.Errorf(
			"the machine disconnected before the probe could start; it has to be connected for a probe to run")
	}
	if target == strings.TrimSpace(r.selfNodeId) {
		// The graph says this replica holds it and the registry says otherwise.
		// The registry is the ground truth about a live stream, so this is a
		// stale row rather than a routing question, and forwarding to ourselves
		// would loop.
		return ModelProbeOutcome{Model: row.Model}, fmt.Errorf(
			"the machine's connection ended on this node; it has to reconnect before a probe can start")
	}
	if r.forwarder == nil {
		return ModelProbeOutcome{Model: row.Model}, fmt.Errorf(
			"the machine moved to another node (%s) and this one cannot forward to it", target)
	}
	authorityCtx, err := r.probeBorrowedAuthority(ctx, row.OwnerUserId)
	if err != nil {
		return ModelProbeOutcome{Model: row.Model}, err
	}
	return r.forwarder.ForwardModelProbe(
		authorityCtx, target, row.WorkerId, row.OwnerUserId, row.Model, row.SuiteVersion,
		workerservice.DefaultModelProbeTimeout, onProgress)
}

func (r *ModelProbeRunner) probeLocally(
	ctx context.Context,
	w *workerservice.Worker,
	row modelProbeRow,
	onProgress func(workerservice.ModelProbeProgress),
) (ModelProbeOutcome, error) {
	handle, err := w.StartModelProbe(ctx, workerservice.ModelProbeRequest{
		RequestId:      row.ProbeId,
		RegistrationId: row.WorkerId,
		Model:          row.Model,
		SuiteVersion:   row.SuiteVersion,
	})
	if err != nil {
		return ModelProbeOutcome{Model: row.Model}, err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for p := range handle.Progress() {
			onProgress(p)
		}
	}()
	out, waitErr := handle.Wait(ctx)
	wg.Wait()
	if waitErr != nil {
		return ModelProbeOutcome{Model: row.Model}, waitErr
	}
	return ModelProbeOutcome{
		Model:        out.Model,
		Ok:           out.Ok,
		SuiteVersion: out.SuiteVersion,
		ErrorMessage: out.Error,
		Figures:      out.Figures,
	}, nil
}

func (r *ModelProbeRunner) currentNodeForProbe(ctx context.Context, row modelProbeRow) (string, error) {
	machines, err := r.dispatcher.store.WorkersForOwner(ctx, row.OwnerUserId)
	if err != nil {
		return "", fmt.Errorf("could not re-read where the machine is connected: %w", err)
	}
	for _, m := range machines {
		if m.RegistrationId != row.WorkerId {
			continue
		}
		if !m.RevokedAt.IsZero() {
			return "", fmt.Errorf("the machine was revoked, so its worker token no longer works")
		}
		return strings.TrimSpace(m.ConnectedNodeId), nil
	}
	return "", fmt.Errorf("the machine is no longer registered to this owner")
}

// probeOwnerActor is the borrowed-authority context every row read and write
// here runs under. Both v1:worker:modelProbe and v1:platform:modelMeasurement
// are owner-tiered, so an unstamped write is refused and an unstamped read
// returns zero rows -- which would present as a probe that silently never
// updates, on a surface whose whole job is showing that it does.
func (r *ModelProbeRunner) probeOwnerActor(ctx context.Context, ownerUserId string) context.Context {
	return auth.ContextWithInternalOrigin(auth.ContextWithUserActor(ctx, ownerUserId))
}

func (r *ModelProbeRunner) probeBorrowedAuthority(ctx context.Context, ownerUserId string) (context.Context, error) {
	access := &auth.AccessContext{UserId: ownerUserId, Role: auth.RoleWriter}
	authority, err := auth.ForwardedAuthorityForUser(access, "", "", time.Time{}, r.clock())
	if err != nil {
		return nil, fmt.Errorf("could not assert the machine owner's authority for the hop: %w", err)
	}
	verified, err := auth.VerifyForwardedAuthority(authority, r.clock())
	if err != nil {
		return nil, fmt.Errorf("could not assert the machine owner's authority for the hop: %w", err)
	}
	return auth.BindForwardedContext(ctx, authority.Principal().Claims, verified, authority), nil
}

func (r *ModelProbeRunner) recordProgress(ctx context.Context, row modelProbeRow, p workerservice.ModelProbeProgress) {
	call, err := langparser.RenderCall("recordModelProbeProgress", map[string]any{
		"probeId":        row.ProbeId,
		"completedCases": int64(p.Completed),
		"totalCases":     int64(p.Total),
		"currentCase":    p.CaseId,
		"updatedAt":      r.clock().UTC().Format(time.RFC3339),
	})
	if err != nil {
		r.dispatcher.logger.Warn("model probe: render progress", "probe_id", row.ProbeId, "error", err)
		return
	}
	if _, err := r.dispatcher.engine.Execute(r.probeOwnerActor(ctx, row.OwnerUserId), call); err != nil {
		r.dispatcher.logger.Debug("model probe: record progress", "probe_id", row.ProbeId, "error", err)
	}
}

// recordMeasurement writes the figures and returns the row id.
//
// THE ID IS DERIVED FROM (machine, model, suite), not minted fresh, so a
// re-probe under the same suite is a new VERSION of one logical row rather than
// a second row -- which is what makes "what did this machine measure for this
// model" a single readable history instead of a pile.
func (r *ModelProbeRunner) recordMeasurement(ctx context.Context, row modelProbeRow, outcome ModelProbeOutcome) (string, error) {
	suite := strings.TrimSpace(outcome.SuiteVersion)
	if suite == "" {
		// The machine did not echo the version. Filing under the version we
		// ASKED for would claim the machine confirmed something it did not, so
		// the request's version is used and the absence is invisible only
		// because the engine refused any other version before the wire.
		suite = row.SuiteVersion
	}
	measurementId := MeasurementId(row.WorkerId, row.Model, suite)
	call, err := langparser.RenderCall("recordModelMeasurement", map[string]any{
		"measurementId":       measurementId,
		"machineId":           row.WorkerId,
		"modelId":             row.Model,
		"suiteVersion":        suite,
		"measuredAt":          r.clock().UTC().Format(time.RFC3339),
		"structuredValidity":  outcome.Figures.StructuredValidity.Row(),
		"toolCallCorrectness": outcome.Figures.ToolCallCorrectness.Row(),
		"throughputTps":       outcome.Figures.ThroughputTps.Row(),
		"ttftMs":              outcome.Figures.TimeToFirstTokenMs.Row(),
		"probeError":          strings.TrimSpace(outcome.ErrorMessage),
	})
	if err != nil {
		return "", fmt.Errorf("render the measurement: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := r.dispatcher.engine.Execute(r.probeOwnerActor(writeCtx, row.OwnerUserId), call); err != nil {
		return "", fmt.Errorf("store the measurement: %w", err)
	}
	return measurementId, nil
}

// MeasurementId derives the stable row id for one (machine, model, suite).
//
// A DIGEST rather than a concatenation, because a model id carries ':' and '/'
// and a row id is `{concept}:{shortId}` -- a concatenation would produce an id
// the engine cannot parse, and truncating one would collide two models whose
// tags share a prefix.
//
// Through core/id rather than crypto/sha256 directly, which is what
// TestNoSHA256InIntegrations asks for and is the better call on its own terms:
// `MustFromMap` marshals with SORTED KEYS, so the three parts are named rather
// than positional. The hand-rolled version separated them with a NUL to keep
// ("a", "b\x00c") from colliding with ("a\x00b", "c") -- a real hazard,
// handled correctly there, and one that simply does not arise once the parts
// are map keys instead of a concatenation.
//
// SUITE VERSION IS PART OF THE KEY, deliberately. A reading from a different
// suite is not comparable to one from this suite, so it belongs in a different
// row rather than overwriting the old one -- which is also why the page prints
// the suite beside every figure.
func MeasurementId(machineId, modelId, suiteVersion string) string {
	return "v1:platform:modelMeasurement:" + string(id.New().MustFromMap(map[string]any{
		"machineId":    strings.TrimSpace(machineId),
		"modelId":      strings.TrimSpace(modelId),
		"suiteVersion": strings.TrimSpace(suiteVersion),
	}))
}

func (r *ModelProbeRunner) finish(ctx context.Context, row modelProbeRow, status, measurementId, message string) {
	call, err := langparser.RenderCall("finishModelProbe", map[string]any{
		"probeId":       row.ProbeId,
		"status":        status,
		"measurementId": measurementId,
		"errorMessage":  message,
		"endedAt":       r.clock().UTC().Format(time.RFC3339),
	})
	if err != nil {
		r.dispatcher.logger.Warn("model probe: render finish", "probe_id", row.ProbeId, "error", err)
		return
	}
	// The finishing write does NOT ride the probe's context: that context may be
	// the one that just expired, and a terminal status written under a dead
	// context is a probe that stays `running` forever with nothing to explain
	// it.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := r.dispatcher.engine.Execute(r.probeOwnerActor(writeCtx, row.OwnerUserId), call); err != nil {
		r.dispatcher.logger.Warn("model probe: close the probe record",
			"probe_id", row.ProbeId, "status", status, "error", err)
	}
}
