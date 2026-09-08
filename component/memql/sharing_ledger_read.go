package memql

// The `fleetSharingLedger` READ (epic memql#5146, design D6).
//
// What a shared machine has done this week, for the person who lent it.
//
// ===========================================================================
// THE NARROWING HAPPENS HERE, NOT ON THE PAGE
// ===========================================================================
// A decision record carries a great deal a machine's owner must not see. This
// read folds it to counts BEFORE anything leaves the engine, so the promise
// holds even if the page is rewritten by somebody who never read the fold. A
// query returning the rows and letting the surface pick what to show would put
// the promise in a renderer, which is where it would eventually be lost.

import (
	"context"
	"fmt"
	"strings"
	"time"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	langparser "github.com/znasllc-io/memql/component/language/parser"
)

// SharingLedgerConcept is the canonical id of the read's answer.
const SharingLedgerConcept = "v1:worker:sharingLedger"

// evaluateFleetSharingLedgerExpression serves the `fleetSharingLedger` builtin.
func (e *MemQLEngine) evaluateFleetSharingLedgerExpression(ctx context.Context, args map[string]any) ([]memorynodes.MemoryNode, error) {
	if e == nil {
		return nil, fmt.Errorf("engine is nil")
	}
	registrationId := strings.TrimSpace(stringArg(args, "registrationId"))
	// THE AUTHORIZED READ FIRST. A ledger is about somebody's own machine, and
	// resolving it through `workersForUser` means a machine that is not the
	// caller's is not in the answer -- this function does not have to be
	// trusted to check, exactly as the pull and the probe do not.
	if _, err := e.modelPullMachineFor(ctx, registrationId); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	week := isoWeekOf(now)
	since := startOfISOWeek(now)

	call, err := langparser.RenderCall("routerCallsInWindow", map[string]any{
		"since": since.Format(time.RFC3339),
		"until": now.Format(time.RFC3339),
	})
	if err != nil {
		return nil, fmt.Errorf("fleetSharingLedger: render the window read: %w", err)
	}
	res, err := e.Execute(ctx, call)
	if err != nil {
		// A LEDGER THAT CANNOT BE READ IS NOT AN EMPTY LEDGER. Answering zero
		// would tell somebody who lent their machine that nobody used it, which
		// is a specific and wrong claim; the row says the read failed instead.
		return singleVirtualRow(SharingLedgerConcept, registrationId, map[string]any{
			"machineId": registrationId,
			"week":      week,
			"readable":  false,
			"sentence":  "This week's usage could not be read. It is not that nothing ran -- nobody looked.",
		})
	}

	rows := modelPullRows(res.OutputPayload())
	calls := make([]LedgerCall, 0, len(rows))
	for _, row := range rows {
		// The surface a call ran on is `fleet:<registrationId>`, so the machine
		// is derived rather than read: the decision record names WHERE a call
		// went, and this is that field's one consumer.
		surface := strings.TrimSpace(mapString(row, "executionSurface"))
		machineId := strings.TrimPrefix(surface, "fleet:")
		if surface == machineId || machineId == "" {
			continue
		}
		calls = append(calls, LedgerCall{
			MachineId:    machineId,
			ActingUserId: mapString(row, "userId"),
			Level:        mapString(row, "level"),
			Week:         week,
		})
	}

	entry := FoldLedger(trimConceptPrefix(registrationId), week, calls)
	// The machine id on the row is the CANONICAL one the caller asked with, so
	// the page does not have to know that the fold matched on the bare form.
	payload := entry.Row()
	payload["machineId"] = registrationId
	payload["readable"] = true
	payload["sentence"] = entry.Sentence()
	return singleVirtualRow(SharingLedgerConcept, registrationId, payload)
}

// isoWeekOf renders a time as YYYY-Www.
func isoWeekOf(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// startOfISOWeek is the Monday 00:00 UTC of t's ISO week.
//
// MONDAY, because that is what ISOWeek counts from, and a window that started
// on a different day would label calls with a week number they did not fall in.
func startOfISOWeek(t time.Time) time.Time {
	day := int(t.UTC().Weekday())
	if day == 0 {
		day = 7 // Sunday closes the ISO week rather than opening one.
	}
	monday := t.UTC().AddDate(0, 0, -(day - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}
