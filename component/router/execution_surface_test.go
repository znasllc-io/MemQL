package router

import (
	"testing"
	"time"
)

func zeroTime() time.Time { return time.Time{} }

// A provider that ran the call on somebody's machine.
type surfaceSayingProvider struct{ surface string }

func (p surfaceSayingProvider) ExecutionSurface() string { return p.surface }

// A provider that ran the call on MemQL's own path, like every vendor one.
type plainProvider struct{}

// The record has to carry WHERE the call ran, and the reason this test exists
// is that for the whole life of the field it did not.
//
// `CallRecord.ExecutionSurface` and the `executionSurface` column on
// v1:router:call both predate epic memql#5146. `buildRecord` never assigned the
// field and `fleetProvider.LastCall()` -- the accessor written for this exact
// handoff -- had no caller anywhere. Every decision row carried "".
//
// Nothing failed, because an empty string is a value. It surfaced only when the
// sharing ledger needed to fold the calls that ran on ONE machine and found
// nothing on any row to fold on: the ledger would have reported "No calls have
// run on this machine this week" to the person who lent the hardware, forever,
// in the same words it uses for an honest zero.
func TestTheRecordCarriesWhereTheCallRan(t *testing.T) {
	rec := buildRecord(ResolveRequest{}, Resolved{}, surfaceSayingProvider{surface: "fleet:laptop"},
		0, 0, 0, zeroTime(), zeroTime(), zeroTime(), false, nil, nil)
	if rec.ExecutionSurface != "fleet:laptop" {
		t.Fatalf("ExecutionSurface = %q, want %q.\n"+
			"The decision row is the only place that says which machine served a call, and the "+
			"sharing ledger folds on it. An empty value here is not a missing feature -- it is a "+
			"ledger that reports zero to the one person entitled to a true answer.",
			rec.ExecutionSurface, "fleet:laptop")
	}
}

// The empty string is a REAL answer here and must stay one.
//
// A call to a vendor ran on nobody's machine. Reporting "" for it is correct,
// and the ledger reads it correctly too: a row whose surface does not carry the
// fleet prefix is not this machine's call and is skipped. Treating the absence
// as an error, or defaulting it to something, would put vendor calls into
// somebody's machine ledger.
func TestAProviderThatRanOnNobodysMachineSaysSo(t *testing.T) {
	rec := buildRecord(ResolveRequest{}, Resolved{}, plainProvider{},
		0, 0, 0, zeroTime(), zeroTime(), zeroTime(), false, nil, nil)
	if rec.ExecutionSurface != "" {
		t.Fatalf("ExecutionSurface = %q, want empty -- a vendor call ran on nobody's machine",
			rec.ExecutionSurface)
	}
}

// And a nil provider must not panic the recorder. The record is written on the
// failure paths too, where there may be no provider to ask.
func TestANilProviderIsNotAskedWhereItRan(t *testing.T) {
	rec := buildRecord(ResolveRequest{}, Resolved{}, nil,
		0, 0, 0, zeroTime(), zeroTime(), zeroTime(), false, nil, nil)
	if rec.ExecutionSurface != "" {
		t.Fatalf("ExecutionSurface = %q, want empty", rec.ExecutionSurface)
	}
}
