package probe

import "encoding/json"

// The figure set as JSON, for the one hop that cannot carry a discriminated
// union (epic memql#5146).
//
// ===========================================================================
// WHY JSON AND NOT A MESSAGE
// ===========================================================================
// Protobuf cannot express "a stat XOR a reason". A message with both halves
// present -- a `measured` bool beside a populated `median` -- is exactly the
// shape that invites the reader who forgets to check the flag, which is the one
// defect this whole type exists to prevent. The WorkerService pair pays that
// cost deliberately, because the cockpit is another codebase and a wire type is
// the contract it codes against; the NODE hop between two replicas of the same
// binary does not have to, so it carries the row shape verbatim.
//
// The row shape is the SAME shape v1:platform:modelMeasurement stores, so the
// hop and the row cannot disagree about what a figure is.

type figuresWire struct {
	StructuredValidity  map[string]any `json:"structuredValidity"`
	ToolCallCorrectness map[string]any `json:"toolCallCorrectness"`
	ThroughputTps       map[string]any `json:"throughputTps"`
	TimeToFirstTokenMs  map[string]any `json:"ttftMs"`
}

// FiguresToJSON renders a figure set for the node hop.
//
// It never fails: every branch of Figure.Row() produces a marshalable map, and
// returning an error a caller would have to invent a figure for would be worse
// than the empty string, which FiguresFromJSON reads as four named absences.
func FiguresToJSON(f Figures) string {
	raw, err := json.Marshal(figuresWire{
		StructuredValidity:  f.StructuredValidity.Row(),
		ToolCallCorrectness: f.ToolCallCorrectness.Row(),
		ThroughputTps:       f.ThroughputTps.Row(),
		TimeToFirstTokenMs:  f.TimeToFirstTokenMs.Row(),
	})
	if err != nil {
		return ""
	}
	return string(raw)
}

// FiguresFromJSON reads a figure set back.
//
// AN EMPTY OR MALFORMED PAYLOAD IS FOUR NAMED ABSENCES, never four zeroes. A
// peer that could not say what it measured has not measured zero, and the
// difference is what the receiving replica writes onto the measurement row.
func FiguresFromJSON(raw string) Figures {
	absent := Figures{
		StructuredValidity:  Absent(AbsentUnmeasured, ""),
		ToolCallCorrectness: Absent(AbsentUnmeasured, ""),
		ThroughputTps:       Absent(AbsentUnmeasured, ""),
		TimeToFirstTokenMs:  Absent(AbsentUnmeasured, ""),
	}
	if raw == "" {
		return absent
	}
	var w figuresWire
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		return absent
	}
	return Figures{
		StructuredValidity:  FigureFromRow(anyMap(w.StructuredValidity)),
		ToolCallCorrectness: FigureFromRow(anyMap(w.ToolCallCorrectness)),
		ThroughputTps:       FigureFromRow(anyMap(w.ThroughputTps)),
		TimeToFirstTokenMs:  FigureFromRow(anyMap(w.TimeToFirstTokenMs)),
	}
}

// anyMap adapts a decoded object for FigureFromRow, which takes `any` because
// it also reads rows off the graph.
func anyMap(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}
