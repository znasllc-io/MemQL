package memql

import "testing"

func TestEmptyBuiltinReplyContainsNoPhantomRow(t *testing.T) {
	// A builtin returns this typed node set. Serializing its empty value as
	// data:[{}] made Fleet show one unnamed, offline model on an empty fleet.
	result, err := builtinProbeEngine(t, nil).Execute(userCtx("alice"), rowAuthzBuiltinProbeCall)
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := result.ToAPIResult()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("empty builtin emitted %d data rows; want none: %v", len(data), data)
	}
}

func TestEmptyLogicObjectRemainsAValue(t *testing.T) {
	// Empty objects are meaningful logic outputs; their shape cannot tell a
	// client whether they were an empty builtin node map.
	_, data, err := NewResultWithOutput(map[string]any{}).ToAPIResult()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 || data[0].GetStructValue() == nil || len(data[0].GetStructValue().GetFields()) != 0 {
		t.Fatalf("empty object was lost: %v", data)
	}
}
