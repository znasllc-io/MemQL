package memql

// The `fleetRecommended` READ (epic memql#5146, design D2).
//
// The act's answer without the act: what the catalog recommends for one
// machine, why anything is blocked, and the class the recommendation rests on.
//
// ===========================================================================
// THE PAGE AND THE ACT READ THE SAME FUNCTION
// ===========================================================================
// The machine page could compute this itself -- it has the catalog and it has
// the machine -- and that would be a second implementation of `RecommendedSet`
// in a second language. The two would drift, and the drift would present as a
// page offering to pull a model the act then refuses, or listing a set the act
// does not take. So the page asks the engine, and there is one answer.

import (
	"context"
	"fmt"
	"strings"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
)

// RecommendedSetConcept is the canonical id of the read's answer.
const RecommendedSetConcept = "v1:worker:recommendedSet"

// evaluateFleetRecommendedExpression serves the `fleetRecommended` builtin.
func (e *MemQLEngine) evaluateFleetRecommendedExpression(ctx context.Context, args map[string]any) ([]memorynodes.MemoryNode, error) {
	if e == nil {
		return nil, fmt.Errorf("engine is nil")
	}
	registrationId := strings.TrimSpace(stringArg(args, "registrationId"))
	hardware, platformOs, err := e.machineHardwareFor(ctx, registrationId)
	if err != nil {
		return nil, err
	}
	class := MachineClass(hardware)

	// A machine that has not reported gets the CLASS ("") and an empty set,
	// with no blocked entries. There is nothing to recommend and nothing to
	// explain -- a page listing every profile as blocked would read as a
	// machine that failed rather than one that has not spoken.
	var entries []any
	var gap []any
	if class != ClassUnknown {
		catalog, err := e.catalogProfiles(ctx)
		if err != nil {
			return nil, err
		}
		set := RecommendedSet(class, hardware, platformOs, catalog)
		entries = make([]any, 0, len(set))
		for _, r := range set {
			entries = append(entries, map[string]any{
				"modelId":  r.Profile.ModelId,
				"level":    r.Level,
				"runtime":  r.Profile.Runtime,
				"category": r.Profile.Category,
				"params":   r.Profile.Params,
				"size":     r.Profile.SizeBytes,
				"notes":    r.Profile.Notes,
				// The SENTENCE, not a flag. The page renders it verbatim; a
				// boolean would put the wording in the renderer, where the next
				// person adding a case has no reason to look here for the
				// reasoning that produced the others.
				"blocked":  r.Blocked,
				"pullable": r.Pullable(),
			})
		}
		for _, name := range RuntimeGap(set, hardware) {
			gap = append(gap, name)
		}
	}

	return singleVirtualRow(RecommendedSetConcept, registrationId, map[string]any{
		"workerId":     registrationId,
		"machineClass": class,
		"usableBytes":  UsableBytes(hardware),
		"reported":     hardware.Present(),
		"entries":      entries,
		// The runtimes the set NEEDS and the machine does not have, so the page
		// can say "install Kokoro" once rather than once per blocked row.
		"runtimeGap": gap,
	})
}
