package memql

import (
	"sort"
	"strconv"
	"strings"
)

// THE SHARING LEDGER (epic memql#5146, design D6).
//
// ===========================================================================
// A SHARED MACHINE'S OWNER NEVER SEES ANOTHER PERSON'S PROMPTS
// ===========================================================================
// Somebody who offers their Mac Studio to the team is entitled to know it is
// being used, and NOT entitled to read what it was used for. Those two are in
// tension and the resolution is a shape rather than a policy: the ledger is
// COUNTS AND LEVELS, and there is no field on it that could hold a prompt, a
// completion, a tool argument or a file path.
//
// That is enforced by construction and by a gate. LedgerEntry has four fields,
// every one a count or a closed-vocabulary word; the fold takes decision-record
// rows and reads exactly five keys off them, named in one place. A field added
// here that could carry text fails TestTheLedgerCarriesNoPromptContent, which
// walks the rendered row rather than the struct -- so a nested map smuggled in
// through an `any` is caught too.
//
// WHY A FOLD AND NOT A QUERY. A query returning the rows and letting the page
// pick what to show would put the promise in a renderer, where the next person
// adding a column would have no reason to think twice. Folding first means the
// content never reaches the surface at all, so the promise holds even if the
// page is rewritten by somebody who never read this file.
//
// WHY IT LIVES IN component/memql RATHER THAN BESIDE THE SHARING CONSENT. The
// consent is component/worker's, and that package cannot be imported by this
// one -- worker reaches identity, which reaches here. The fold has to be
// readable by the SURFACE, which is served by a bff, so it sits on the side of
// the edge both can reach. There is one implementation, and the alternative
// was two: one for the page and one for the wire, differing in exactly the way
// this file exists to prevent.

// LedgerCall is one decision record, narrowed to what the ledger may see.
//
// It is a deliberately POOR type: it cannot express a prompt, so a fold built
// on it cannot leak one. The narrowing happens at the row read, which is the
// last point where the full record is in hand and the first where the promise
// can be kept.
type LedgerCall struct {
	// MachineId is the machine that served it.
	MachineId string
	// ActingUserId is WHO the call was for. It is used to count PEOPLE and is
	// never rendered: "served 41 calls for 3 people" is the sentence, and
	// naming the three would tell the machine's owner who is using it for what.
	ActingUserId string
	// Level is the closed four-value vocabulary. It is the only thing the
	// ledger says about the SHAPE of the work, and it says nothing about the
	// content: "reasoning" is a size, not a subject.
	Level string
	// Week is the ISO week, so a ledger entry answers "this week" rather than
	// "ever".
	Week string
}

// LedgerEntry is what a machine's owner sees.
type LedgerEntry struct {
	MachineId string
	Week      string
	// Calls served for anybody, including the owner.
	Calls int
	// People is the number of DISTINCT callers, counted and not named.
	People int
	// ByLevel is calls per level, over the closed four. A map of counts is the
	// most detail this ledger will ever carry.
	ByLevel map[string]int
}

// ledgerRowKeys are the ONLY keys the fold reads off a decision record.
//
// Named here, once, so the allowlist is a value the gate can walk rather than a
// habit spread across a function. Everything else on the record -- and there is
// a great deal, including anything a future field might carry -- is invisible
// to this package by construction.
var ledgerRowKeys = []string{"machineId", "actingUserId", "level", "week"}

// LedgerRowKeys returns the allowlist, for the gate.
func LedgerRowKeys() []string {
	out := make([]string, len(ledgerRowKeys))
	copy(out, ledgerRowKeys)
	return out
}

// LedgerCallFromRow narrows a decision record to what the ledger may see.
//
// It reads FOUR KEYS and ignores the rest of the row entirely. A field added to
// the decision record later -- a prompt name, an error message, a completion
// preview -- is not merely unrendered here; it is unread.
func LedgerCallFromRow(row map[string]any) LedgerCall {
	return LedgerCall{
		MachineId:    rowLedgerString(row, "machineId"),
		ActingUserId: rowLedgerString(row, "actingUserId"),
		Level:        rowLedgerString(row, "level"),
		Week:         rowLedgerString(row, "week"),
	}
}

// FoldLedger counts one machine's week.
//
// PEOPLE ARE COUNTED, NOT NAMED. The distinct-caller set exists inside this
// function and does not leave it: the owner of a shared machine learns that
// three people used it, and learns nothing about which three or what for.
// Naming them would turn a usage figure into a surveillance surface, on
// hardware somebody volunteered.
//
// The OWNER's own calls are counted in `Calls` and in `People` alongside
// everybody else's, because the figure answers "how busy has this machine
// been" rather than "how much have I lent it out". A ledger that excluded the
// owner would show zero on a machine they use constantly.
func FoldLedger(machineId, week string, calls []LedgerCall) LedgerEntry {
	entry := LedgerEntry{MachineId: machineId, Week: week, ByLevel: map[string]int{}}
	people := map[string]struct{}{}
	for _, c := range calls {
		if c.MachineId != machineId || (week != "" && c.Week != week) {
			continue
		}
		entry.Calls++
		if id := strings.TrimSpace(c.ActingUserId); id != "" {
			people[id] = struct{}{}
		}
		if level := strings.TrimSpace(c.Level); level != "" {
			entry.ByLevel[level]++
		}
	}
	entry.People = len(people)
	return entry
}

// Row renders a ledger entry for a surface.
//
// It is the ONLY way a ledger reaches a client, and it emits exactly the four
// counts. `byLevel` is sorted so two replicas render the same order.
func (e LedgerEntry) Row() map[string]any {
	levels := make([]string, 0, len(e.ByLevel))
	for level := range e.ByLevel {
		levels = append(levels, level)
	}
	sort.Strings(levels)
	byLevel := make([]any, 0, len(levels))
	for _, level := range levels {
		byLevel = append(byLevel, map[string]any{"level": level, "calls": e.ByLevel[level]})
	}
	return map[string]any{
		"machineId": e.MachineId,
		"week":      e.Week,
		"calls":     e.Calls,
		"people":    e.People,
		"byLevel":   byLevel,
	}
}

// Sentence is the ledger as the machine page says it.
//
// One line, in words, because the figure is for a person deciding whether to
// keep sharing rather than for a dashboard. It says the plural correctly and it
// says NOTHING about who or what -- and the "you" case is separated, because
// "served 41 calls for 1 person" when that person is you reads as a stranger.
func (e LedgerEntry) Sentence() string {
	if e.Calls == 0 {
		return "No calls have run on this machine this week."
	}
	calls := "calls"
	if e.Calls == 1 {
		calls = "call"
	}
	switch e.People {
	case 0, 1:
		return "Served " + strconv.Itoa(e.Calls) + " " + calls + " this week."
	default:
		return "Served " + strconv.Itoa(e.Calls) + " " + calls + " for " + strconv.Itoa(e.People) + " people this week."
	}
}

func rowLedgerString(row map[string]any, key string) string {
	s, _ := row[key].(string)
	return strings.TrimSpace(s)
}
