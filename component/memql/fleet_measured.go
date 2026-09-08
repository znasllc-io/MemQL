package memql

// MEASURED ORDERING (epic memql#5146, design D4).
//
// ===========================================================================
// MEASUREMENT RANKS AND GATES NOTHING, THIS RELEASE
// ===========================================================================
// Eligibility is unchanged: the advertised flags decide, so a model that failed
// the structured probe is STILL eligible for a structured call and is reported
// as failing on the machine page, on the decision record and in the fleet
// models view. What changes is the ORDER -- a measured model outranks an
// unmeasured one, and among measured models the one that actually validates
// wins.
//
// Making a failed probe a hard eligibility gate is a later decision, after a
// release of measurements, and the reason is concrete rather than cautious: the
// class arithmetic in this same epic was wrong in one direction on the two most
// common GPUs until a real card reported a real number. A probe that refuses a
// working model on a bad run is worse than no probe.
//
// ===========================================================================
// THE BOOL IS READ BEFORE THE NUMBER
// ===========================================================================
// Both accessors answer `(value, ok)`, and every caller reads `ok` FIRST. Zero
// tokens per second and "nobody measured this" are different answers; returning
// `0, true` for an unmeasured model would sort it as the slowest MEASURED one,
// which is precisely the confusion the bool exists to prevent.
//
// A number computed from parameters and CALLED throughput would read on the
// decision record exactly like a measurement. That is why the hook these fill
// was left empty rather than filled with a proxy, and it is why nothing here
// falls back to one.

import "sort"

// Measured is one model's measured figures, folded across the machines behind
// it in the caller's catalog.
type Measured struct {
	// StructuredValidity is the fraction of the suite's structured cases whose
	// output validated. Present only when a probe measured it.
	StructuredValidity float64
	HasValidity        bool
	// ThroughputTps is tokens per second, the median over the suite's two
	// prompt sizes.
	ThroughputTps float64
	HasThroughput bool
	// SuiteVersion is the suite the winning figures were scored under. Two
	// figures from different suites are not comparable, so it travels with them
	// and the decision record carries it.
	SuiteVersion string
	// MachineId is which machine produced the winning figures. A measurement's
	// whole content is which hardware it came from, so a fold that lost it
	// would report a number about a fleet rather than about a machine.
	MachineId string
}

// Measurement is one v1:platform:modelMeasurement row, as the fold reads it.
type Measurement struct {
	MachineId          string
	ModelId            string
	SuiteVersion       string
	MeasuredAt         string
	StructuredValidity float64
	HasValidity        bool
	ThroughputTps      float64
	HasThroughput      bool
}

// ValidityOf is the measured-capability key for `fleet:strongest`.
//
// It is the FIRST key, above the caller's preference list, because a
// preference names models a person chose and this says which of them actually
// works here. An unmeasured model answers `ok=false` and sorts after every
// measured one; the decision record says `unmeasured` rather than inventing a
// number for it.
func ValidityOf(m FleetModel) (float64, bool) {
	if !m.Measured.HasValidity {
		return 0, false
	}
	return m.Measured.StructuredValidity, true
}

// ThroughputOf is the measured-capability key for `fleet:fastest`.
//
// Same shape and same rule. A model nobody has timed is not the slowest model;
// it is a model nobody has timed, and the two sort differently.
func ThroughputOf(m FleetModel) (float64, bool) {
	if !m.Measured.HasThroughput {
		return 0, false
	}
	return m.Measured.ThroughputTps, true
}

// AttachMeasurements folds measurement rows onto a catalog.
//
// ===========================================================================
// ONLY A MACHINE THAT STILL OFFERS THE MODEL MAY RANK IT
// ===========================================================================
// A measurement is keyed by (machine, model) and a catalog entry spans
// machines, so the fold has to pick. It picks the BEST figures among the
// machines CURRENTLY BEHIND the entry -- which is the same union reasoning
// FleetModel.Tools already uses: the question is whether this fleet can serve
// the turn on this model, and one machine that can is enough, because selection
// then picks that machine specifically.
//
// The filter is the part that is easy to leave out. A measurement from a
// machine that has since dropped the model, or been revoked, or gone offline,
// describes hardware that will not serve the call -- and letting it rank the
// entry would promote a model on the strength of a machine nobody can reach.
func AttachMeasurements(models []FleetModel, measurements []Measurement) []FleetModel {
	byModel := map[string][]Measurement{}
	for _, m := range measurements {
		if m.ModelId == "" || m.MachineId == "" {
			continue
		}
		byModel[m.ModelId] = append(byModel[m.ModelId], m)
	}

	out := make([]FleetModel, len(models))
	copy(out, models)
	for i := range out {
		rows := byModel[out[i].ModelId]
		if len(rows) == 0 {
			continue
		}
		behind := map[string]bool{}
		for _, machine := range out[i].Machines {
			behind[machine.RegistrationId] = true
		}
		out[i].Measured = bestMeasurement(rows, behind)
	}
	return out
}

// bestMeasurement picks the winning figures among the machines still behind an
// entry.
//
// VALIDITY WINS BEFORE THROUGHPUT, and the two are taken from the SAME
// measurement rather than maximised independently. A fold that took the best
// validity from one machine and the best throughput from another would describe
// a machine that does not exist, and the decision record would name it.
//
// The tiebreak is the machine id, so two replicas reading one catalog produce
// the same answer with no shared state.
func bestMeasurement(rows []Measurement, behind map[string]bool) Measured {
	eligible := make([]Measurement, 0, len(rows))
	for _, r := range rows {
		if behind[r.MachineId] {
			eligible = append(eligible, r)
		}
	}
	if len(eligible) == 0 {
		return Measured{}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.HasValidity != b.HasValidity {
			return a.HasValidity
		}
		if a.HasValidity && a.StructuredValidity != b.StructuredValidity {
			return a.StructuredValidity > b.StructuredValidity
		}
		if a.HasThroughput != b.HasThroughput {
			return a.HasThroughput
		}
		if a.HasThroughput && a.ThroughputTps != b.ThroughputTps {
			return a.ThroughputTps > b.ThroughputTps
		}
		return a.MachineId < b.MachineId
	})
	win := eligible[0]
	return Measured{
		StructuredValidity: win.StructuredValidity,
		HasValidity:        win.HasValidity,
		ThroughputTps:      win.ThroughputTps,
		HasThroughput:      win.HasThroughput,
		SuiteVersion:       win.SuiteVersion,
		MachineId:          win.MachineId,
	}
}

// measuredRow projects a fold onto the virtual fleetModel row.
//
// Each figure keeps the DISCRIMINATED shape the measurement row stores, so a
// surface reads `measured` and finds either a number or a reason, never both.
// Flattening it to two nullable numbers here would put the decision "what does
// a missing median mean" in every renderer instead of in one place.
func measuredRow(m Measured) map[string]any {
	validity := map[string]any{"measured": false, "absentReason": "unmeasured"}
	if m.HasValidity {
		validity = map[string]any{"measured": true, "median": m.StructuredValidity}
	}
	throughput := map[string]any{"measured": false, "absentReason": "unmeasured"}
	if m.HasThroughput {
		throughput = map[string]any{"measured": true, "median": m.ThroughputTps}
	}
	return map[string]any{
		"structuredValidity": validity,
		"throughputTps":      throughput,
		// Both travel with the figures rather than beside them: a measurement's
		// content is which hardware produced it under which suite, and a number
		// separated from either is one nobody can weigh.
		"suiteVersion": m.SuiteVersion,
		"machineId":    m.MachineId,
	}
}

// MeasurementFromRow reads one v1:platform:modelMeasurement row.
//
// A figure that is absent, malformed, or claims to be measured without a
// median reads as NOT MEASURED. That direction is the one that matters: a
// number is what gets ranked on, so a figure nobody can read must not become
// one -- the same rule component/worker/probe.FigureFromRow follows on the
// other side of the module edge.
func MeasurementFromRow(row map[string]any) Measurement {
	m := Measurement{
		MachineId:    mapString(row, "machineId"),
		ModelId:      mapString(row, "modelId"),
		SuiteVersion: mapString(row, "suiteVersion"),
		MeasuredAt:   mapString(row, "measuredAt"),
	}
	m.StructuredValidity, m.HasValidity = measuredFigureFromRow(row["structuredValidity"])
	m.ThroughputTps, m.HasThroughput = measuredFigureFromRow(row["throughputTps"])
	return m
}

// measuredFigureFromRow reads the median off one stored figure.
//
// It reads `measured` FIRST and returns on a false, so a row that carries a
// median beside `measured: false` -- which the writer never produces, but a
// hand-edited or partially-migrated row could -- is read as an absence rather
// than as the number sitting next to the flag.
func measuredFigureFromRow(v any) (float64, bool) {
	fig, ok := v.(map[string]any)
	if !ok || len(fig) == 0 {
		return 0, false
	}
	measured, _ := fig["measured"].(bool)
	if !measured {
		return 0, false
	}
	switch n := fig["median"].(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
