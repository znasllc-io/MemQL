package work

// The park, as pure decisions (epic memql#5096, task memql#5101, design D9).

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// doorRefusal stands in for component/router's InferenceUnavailable, which
// this package cannot import (the dependency runs the other way -- that is
// exactly why DoorReporter is an interface).
type doorRefusal struct {
	code  string
	doors []DoorReport
}

func (d *doorRefusal) Error() string               { return d.code + ": no door is open" }
func (d *doorRefusal) RefusalCode() string         { return d.code }
func (d *doorRefusal) ReportedDoors() []DoorReport { return d.doors }

func TestInferenceRefusalCodeRecognisesEveryCode(t *testing.T) {
	for _, code := range []string{
		RefusalNoLocalModel, RefusalNoApp, RefusalEveryDoorShut, RefusalCeilingReached,
	} {
		got, ok := InferenceRefusalCode(code + ": something happened")
		if !ok || got != code {
			t.Errorf("InferenceRefusalCode(%q...) = (%q, %v), want %q", code, got, ok, code)
		}
	}

	// IT MATCHES ANYWHERE IN THE MESSAGE, not only at the front. The failure
	// travels through wrapping on the way to the executor ("automation step
	// 3: ..."), and a prefix match would silently stop recognising the
	// condition the first time a caller added context.
	if got, ok := InferenceRefusalCode("step 3 failed: " + RefusalEveryDoorShut + ": nothing open"); !ok || got != RefusalEveryDoorShut {
		t.Errorf("a wrapped message must still be recognised, got (%q, %v)", got, ok)
	}

	for _, msg := range []string{"", "connection refused", "permission denied", "the model said no"} {
		if _, ok := InferenceRefusalCode(msg); ok {
			t.Errorf("%q must not read as an inference refusal -- a run that failed for an "+
				"ordinary reason must FAIL, not park forever", msg)
		}
	}
}

func TestDoorsFromPrefersTheStructuredReportAndFallsBackToTheMessage(t *testing.T) {
	structured := &doorRefusal{
		code: RefusalEveryDoorShut,
		doors: []DoorReport{
			{Door: "local", Name: "fleet:*", Reason: "nothing online", Considered: map[string]string{"laptop": "offline"}},
			{Door: "app", Name: "app:*", Reason: "nobody signed in"},
		},
	}
	code, doors, ok := DoorsFrom(structured)
	if !ok || code != RefusalEveryDoorShut || len(doors) != 2 {
		t.Fatalf("DoorsFrom = (%q, %d doors, %v), want the structured report", code, len(doors), ok)
	}
	if doors[0].Considered["laptop"] != "offline" {
		t.Errorf("the machine-level detail must survive: %v", doors[0].Considered)
	}

	// A refusal that travelled as a STRING still parks the run, with an
	// EMPTY door list. That is the honest answer: it says the structure did
	// not reach here, where a synthetic entry would claim a door nobody
	// named. This is the resumed-from-checkpoint case, not a hypothetical.
	code, doors, ok = DoorsFrom(errors.New(RefusalNoLocalModel + ": laptop offline"))
	if !ok || code != RefusalNoLocalModel {
		t.Fatalf("a string refusal must still park: (%q, %v)", code, ok)
	}
	if len(doors) != 0 {
		t.Errorf("doors = %v, want none -- an invented door is worse than an absent one", doors)
	}

	if _, _, ok := DoorsFrom(nil); ok {
		t.Error("nil is not a refusal")
	}
	if _, _, ok := DoorsFrom(errors.New("disk full")); ok {
		t.Error("an ordinary failure must not park")
	}

	// Wrapped, which is how it actually arrives.
	wrapped := errors.Join(errors.New("automation step 3"), structured)
	if code, _, ok := DoorsFrom(wrapped); !ok || code != RefusalEveryDoorShut {
		t.Errorf("a wrapped refusal must still be found: (%q, %v)", code, ok)
	}
}

func TestTheParkApprovalCarriesEveryDoorAndItsReason(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	a := InferenceUnavailableApproval("v1:work:run:r1", "step3", RefusalEveryDoorShut, []DoorReport{
		{Door: "local", Name: "fleet:*", Reason: "nothing online", Considered: map[string]string{
			"laptop": "offline", "desktop": "does not offer llama3.1:8b",
		}},
		{Door: "app", Name: "app:*", Reason: "nobody signed in"},
	}, now, 24*time.Hour)

	if a.Kind != ApprovalKindInferenceUnavailable {
		t.Fatalf("kind = %q", a.Kind)
	}
	if a.RunId != "v1:work:run:r1" || a.StepKey != "step3" {
		t.Errorf("the approval must name its run and step: %+v", a)
	}
	doors, _ := a.Subject["doors"].([]any)
	if len(doors) != 2 {
		t.Fatalf("subject doors = %v, want both", a.Subject["doors"])
	}
	first, _ := doors[0].(map[string]any)
	considered, _ := first["considered"].([]map[string]any)
	if len(considered) != 2 {
		t.Fatalf("the machine-level detail must reach the subject: %v", first)
	}
	// SORTED, so two readers of one approval see the same list. A map would
	// render in whatever order the encoder chose that day.
	if considered[0]["subject"] != "desktop" || considered[1]["subject"] != "laptop" {
		t.Errorf("considered must be sorted: %v", considered)
	}
	if a.ArtifactHash == "" {
		t.Error("the approval needs a hash: approving one situation must not carry to another")
	}
	if a.ExpiresAt.IsZero() {
		t.Error("a pending park must lapse rather than waiting forever")
	}
	if len(a.Options) != 2 {
		t.Errorf("a person needs both answers offered: %v", a.Options)
	}
}

// The two questions are genuinely different asks. Collapsing them would put
// "spend money" in front of somebody whose actual decision is "spend MORE
// money", and the second needs the ceiling named.
func TestTheCeilingParkAsksADifferentQuestion(t *testing.T) {
	now := time.Now().UTC()
	shut := InferenceUnavailableApproval("r", "s", RefusalEveryDoorShut, nil, now, time.Hour)
	ceiling := InferenceUnavailableApproval("r", "s", RefusalCeilingReached, nil, now, time.Hour)

	if shut.Question == ceiling.Question {
		t.Fatalf("both asks are %q; they are different decisions", shut.Question)
	}
	if !strings.Contains(strings.ToLower(ceiling.Question), "ceiling") {
		t.Errorf("the ceiling ask must name the ceiling: %q", ceiling.Question)
	}
	if ceiling.Evidence.RuleId != "inference."+RefusalCeilingReached {
		t.Errorf("ruleId = %q, want the code named so a reader can trace it", ceiling.Evidence.RuleId)
	}
	if shut.Evidence.Source != EvidenceSourceRules {
		t.Errorf("source = %q: a condition, not a model verdict", shut.Evidence.Source)
	}
}

// The hash is over the SITUATION. Approving "these doors are shut, spend money
// anyway" must not carry to a later moment when a different set is shut.
func TestTheParkHashChangesWhenTheDoorsDo(t *testing.T) {
	now := time.Now().UTC()
	a := InferenceUnavailableApproval("r", "s", RefusalEveryDoorShut,
		[]DoorReport{{Door: "local", Name: "fleet:*", Reason: "offline"}}, now, time.Hour)
	b := InferenceUnavailableApproval("r", "s", RefusalEveryDoorShut,
		[]DoorReport{{Door: "local", Name: "fleet:*", Reason: "offline"},
			{Door: "app", Name: "app:*", Reason: "nobody signed in"}}, now, time.Hour)
	if a.ArtifactHash == b.ArtifactHash {
		t.Error("a different set of shut doors is a different situation and must hash differently")
	}
	again := InferenceUnavailableApproval("r", "s", RefusalEveryDoorShut,
		[]DoorReport{{Door: "local", Name: "fleet:*", Reason: "offline"}}, now.Add(time.Minute), time.Hour)
	if a.ArtifactHash != again.ArtifactHash {
		t.Error("the SAME situation raised a minute later must hash the same, or resume refuses every time")
	}
}

func TestTheRetryIntervalIsHumanScaled(t *testing.T) {
	// A guess about people, not machines: a lid opening, a sign-in and a
	// model pull all happen on this scale. Pinned so a later edit to a
	// smaller number has to say why it is polling faster than the world
	// changes.
	if InferenceRetryInterval < time.Minute || InferenceRetryInterval > 15*time.Minute {
		t.Errorf("InferenceRetryInterval = %s; outside the range the reasoning supports",
			InferenceRetryInterval)
	}
}
