package worker

import (
	"strings"
	"testing"
)

func TestBothConsentsAreRequired(t *testing.T) {
	// The whole of D6. Either half alone is not consent, and the table is
	// written out rather than looped because each row is a different decision
	// somebody made about a different thing.
	cases := []struct {
		name    string
		owner   string
		cockpit string
		want    bool
	}{
		{"neither", SharingModeOwner, InferenceServeOwner, false},
		{"owner only", SharingModeCluster, InferenceServeOwner, false},
		{"cockpit only", SharingModeOwner, InferenceServeCluster, false},
		{"both", SharingModeCluster, InferenceServeCluster, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ServesTheCluster(tc.owner, tc.cockpit); got != tc.want {
				t.Fatalf("owner=%q cockpit=%q -> %v, want %v", tc.owner, tc.cockpit, got, tc.want)
			}
		})
	}
}

func TestAnAbsentCockpitConsentIsNotAConsent(t *testing.T) {
	// A cockpit that predates the field has said nothing, and silence is not
	// agreement to run other people's work on somebody's laptop. This is the
	// one place in the epic where the reading of silence is the SAFE value
	// rather than merely the honest one -- and the two coincide.
	if ServesTheCluster(SharingModeCluster, "") {
		t.Fatal("an unset inferenceServe must not count as consent")
	}
}

func TestAnythingThatIsNotClusterIsOwner(t *testing.T) {
	// A typo, a value from a future engine, a half-written row: all read as NOT
	// shared, because the failure direction here is a stranger's prompt running
	// on somebody's machine.
	for _, mode := range []string{"", "Cluster", "CLUSTER", "shared", "true", "cluster "} {
		got := SharingFromRow(map[string]any{"mode": mode})
		if mode == "cluster " {
			// Trimmed by the reader, so this one IS cluster -- worth pinning,
			// because the trim is what stops a stray space silently revoking a
			// machine somebody deliberately shared.
			if got.Mode != SharingModeCluster {
				t.Fatalf("a trailing space must not revoke a consent, got %q", got.Mode)
			}
			continue
		}
		if got.Mode != SharingModeOwner {
			t.Fatalf("mode %q must read as owner, got %q", mode, got.Mode)
		}
	}
}

func TestAMissingSharingObjectIsOwner(t *testing.T) {
	// Every registration written before this field existed. The default has to
	// be the one that changes nothing about how those machines behave.
	if SharingFromRow(nil).Mode != SharingModeOwner {
		t.Fatal("a row with no sharing object must read as owner")
	}
	if SharingFromRow(map[string]any{}).Mode != SharingModeOwner {
		t.Fatal("an empty sharing object must read as owner")
	}
}

func TestTheRefusalNamesWhichConsentIsMissing(t *testing.T) {
	// THE REASON THIS IS A FUNCTION AND NOT A BOOLEAN. The two repairs are in
	// different places and are performed by different people: the owner's is an
	// act on a web page, the cockpit's is a line in a file on that machine's own
	// disk. A single "not shared" sentence sends half the operators to the wrong
	// machine, and the laptop's owner goes looking on a page for a setting that
	// is not there.
	if got := SharingRefusal(SharingModeCluster, InferenceServeCluster); got != "" {
		t.Fatalf("a shared machine has no refusal, got %q", got)
	}

	ownerMissing := SharingRefusal(SharingModeOwner, InferenceServeCluster)
	if !strings.Contains(ownerMissing, "Fleet") {
		t.Fatalf("the owner's half must point at the Fleet page, got %q", ownerMissing)
	}
	if strings.Contains(ownerMissing, "policy.yaml") {
		t.Fatalf("the owner's half must not send them to a file on the machine, got %q", ownerMissing)
	}

	cockpitMissing := SharingRefusal(SharingModeCluster, InferenceServeOwner)
	if !strings.Contains(cockpitMissing, "policy.yaml") {
		t.Fatalf("the cockpit's half must name the file, got %q", cockpitMissing)
	}
	if strings.Contains(cockpitMissing, "Fleet") {
		t.Fatalf("the cockpit's half must not send them to a web page, got %q", cockpitMissing)
	}

	neither := SharingRefusal(SharingModeOwner, InferenceServeOwner)
	if !strings.Contains(neither, "Both") {
		t.Fatalf("with neither given, the sentence must say both are needed, got %q", neither)
	}
}

func TestSharingRoundTripsThroughTheStoredShape(t *testing.T) {
	s := Sharing{Mode: SharingModeCluster, SharedAt: "2026-09-07T12:00:00Z", SharedBy: "v1:identity:user:u1"}
	back := SharingFromRow(s.Row())
	if back != s {
		t.Fatalf("%+v vs %+v", back, s)
	}
}

func TestAMalformedInferenceServeRefusesTheRegistration(t *testing.T) {
	// A consent value the engine does not understand must not be silently read
	// as either answer. Refusing at register is the same direction the hardware
	// inventory takes, and for a sharper reason: the two readings differ by
	// whether a stranger's prompt runs on this machine.
	_, err := ParseCapabilityDescriptor(`{"platform":"darwin","displayServer":"quartz","inferenceServe":"everyone","schemaVersion":1}`)
	if err == nil {
		t.Fatal("an unknown inferenceServe must refuse the registration")
	}
	if !strings.Contains(err.Error(), "everyone") {
		t.Fatalf("the error must name the offending value, got %q", err)
	}
}
