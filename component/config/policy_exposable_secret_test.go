package config

import (
	"strings"
	"testing"

	busv1 "github.com/znasllc-io/memql/component/bus/gen"
)

// policy_exposable_secret_test.go is the regression guard for memql#3188.
//
// The `go/clear-text-logging` CodeQL family on this repo reached 492 open
// alerts on main, and ALL 492 traced to one three-step root:
//
//	policy_exposable.go  "selection of SiOpenaiApiKey"
//	  >> policy_exposable.go  "call to readConfigField"
//	  >> policy_exposable.go  "v"            (the `case bool` branch)
//
// readConfigField merged six differently-typed fields behind one `any`
// return, so static analysis could not refine the dynamic type and admitted a
// `case bool` branch that is provably unreachable for a field declared
// `string`. From that single false step, field-insensitive taint carried the
// key into every downstream logging call in the engine -- a median 84-hop
// flow, 103 distinct logged expressions, not one of them a credential.
//
// The fix reads Sensitive entries through readSensitivePresence, which
// returns bool, so the raw value has no path into the ctx map at all.
//
// THE FIELD THAT PROMPTED ALL OF THIS IS GONE. SiOpenaiApiKey was the one
// Sensitive entry, and epic memql#5088 removed every manually entered vendor
// key from the product, the proto field included. So the tests below now
// range over an EMPTY set, and a test that ranges over an empty set passes
// without doing anything -- exactly the shape that reads as coverage while
// proving nothing.
//
// Rather than delete them or leave them quietly vacuous, the vacuity is
// ASSERTED: TestThereIsCurrentlyNoSensitiveEntry states the fact out loud and
// fails the moment it stops being true. When the next Sensitive entry is
// added, that test fails first and tells its author that the rest of this file
// has just become live -- a better introduction to the mechanism than
// rediscovering it from a CodeQL report.
const probeKey = "sk-live-CONFIG-MUST-NOT-EXPOSE-0123456789"

// sensitiveEntries returns the Sensitive half of the allow-list.
func sensitiveEntries() []PolicyConfigField {
	var out []PolicyConfigField
	for _, f := range PolicyExposableConfig {
		if f.Sensitive {
			out = append(out, f)
		}
	}
	return out
}

// TestThereIsCurrentlyNoSensitiveEntry makes this file's vacuity visible.
//
// It is not asserting that a Sensitive entry would be WRONG -- the mechanism
// exists precisely so one can be added safely. It asserts that the count is
// what the rest of this file assumes, so nobody reads a green run here as
// evidence that a newly added Sensitive field is covered.
func TestThereIsCurrentlyNoSensitiveEntry(t *testing.T) {
	if got := len(sensitiveEntries()); got != 0 {
		t.Fatalf("PolicyExposableConfig now carries %d Sensitive entry/entries.\n\n"+
			"That is fine, and it means the other tests in this file have just "+
			"stopped being vacuous. Do three things:\n"+
			"  1. add a case for the new FieldName to readSensitivePresence;\n"+
			"  2. do NOT add one to readConfigField (memql#3188 -- its `any` "+
			"return is the taint source);\n"+
			"  3. update this test's expected count, and give the presence test "+
			"below a snapshot that sets the field.",
			got)
	}
}

// TestReadConfigFieldCannotReturnSensitiveValues is the structural half of the
// fix: the presence collapse is correct because of the reader's TYPE, not
// because of a runtime type switch a static analyser cannot follow.
//
// Vacuous while there are no Sensitive entries; see the file header.
func TestReadConfigFieldCannotReturnSensitiveValues(t *testing.T) {
	snapshot := &busv1.ConfigSnapshot{SiDefaultProvider: "chat54Mini"}

	for _, f := range sensitiveEntries() {
		if got := readConfigField(snapshot, f.FieldName); got != nil {
			t.Errorf("readConfigField(%q) returned %T(%v), want nil.\n\n"+
				"Sensitive fields must be absent from readConfigField's switch "+
				"so its `any` return can never carry credential material. That "+
				"merged `any` is what let CodeQL admit an unreachable branch and "+
				"report 492 alerts (memql#3188).", f.FieldName, got, got)
		}
		if !readSensitivePresence(snapshot, f.FieldName) {
			t.Errorf("readSensitivePresence(%q) = false for a set field -- the "+
				"Sensitive entry has no reader, so its ctx key is stuck at false",
				f.FieldName)
		}
	}
}

// TestNonSensitiveConfigStillCarriesItsValue is the REACHABLE POSITIVE for the
// file's central claim.
//
// Without it, "no credential appears in the ctx map" would be satisfied by a
// BuildPolicyConfigCtx that returned nothing at all -- and with the one
// Sensitive entry now removed, that failure mode is not hypothetical: every
// remaining absence asserted here is over an empty set. This proves the
// builder is still doing work.
func TestNonSensitiveConfigStillCarriesItsValue(t *testing.T) {
	ctx := BuildPolicyConfigCtx(&busv1.ConfigSnapshot{SiDefaultProvider: "chat54Mini"})

	if len(ctx) == 0 {
		t.Fatal("BuildPolicyConfigCtx returned an empty map, so every absence " +
			"asserted in this file is trivially satisfied")
	}
	if ctx["defaultProvider"] != "chat54Mini" {
		t.Errorf("defaultProvider = %v, want \"chat54Mini\" -- non-sensitive "+
			"config must still expose its value", ctx["defaultProvider"])
	}

	// Every allow-listed key must be PRESENT whatever its value, so a policy
	// body can reference it unconditionally.
	for _, f := range PolicyExposableConfig {
		if _, ok := ctx[f.Key]; !ok {
			t.Errorf("allow-listed key %q is missing from the ctx map", f.Key)
		}
	}
}

// TestNoConfigValueReachesAnUnrelatedKey is the file's surviving broad guard.
//
// It cannot watch SiOpenaiApiKey any more, so it seeds a credential-shaped
// string into a snapshot field that still takes free text and asserts the
// builder does not launder it into a key nobody expected. That is weaker than
// the original -- the original watched a field declared Sensitive -- and it is
// what remains checkable without one.
func TestNoConfigValueReachesAnUnrelatedKey(t *testing.T) {
	ctx := BuildPolicyConfigCtx(&busv1.ConfigSnapshot{SiDefaultProvider: probeKey})

	for key, value := range ctx {
		s, ok := value.(string)
		if !ok {
			continue
		}
		if key == "defaultProvider" {
			// The field deliberately seeded; its value is meant to appear.
			continue
		}
		if strings.Contains(s, probeKey) {
			t.Fatalf("a snapshot value reached the policy ctx under an unrelated key %q.\n\n"+
				"Each allow-list entry must read its OWN field; a reader that "+
				"falls through to another field's value is how a future "+
				"Sensitive entry would leak (memql#3188).", key)
		}
	}
}
