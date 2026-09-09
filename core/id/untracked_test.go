package id

import (
	"fmt"
	"sync"
	"testing"
)

func trackedCount(e *Engine) int {
	n := 0
	e.cache.Range(func(_, _ any) bool { n++; return true })
	return n
}

func TestUntrackedPreservesIDsAndBoundsRetainedHistory(t *testing.T) {
	tracked, untracked := New(), NewUntracked()
	for _, input := range [][]byte{nil, {}, []byte("hello"), []byte("café 世界"), {0, 1, 2, 127, 128, 255}} {
		if got, want := untracked.FromBytes(input), tracked.FromBytes(input); got != want {
			t.Fatalf("input %q: got %q want %q", input, got, want)
		}
	}
	for n := 0; n < 1000; n++ {
		input := map[string]any{"id": fmt.Sprintf("synthetic-%08d", n), "status": "running", "nested": map[string]any{"number": n, "list": []any{true, nil, "payload"}}}
		want, err := tracked.FromMap(input)
		if err != nil {
			t.Fatal(err)
		}
		got, err := untracked.FromMap(input)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("input %d changed ID", n)
		}
		if !tracked.Exists(want) {
			t.Fatal("tracking constructor lost Exists history")
		}
		if untracked.Exists(got) {
			t.Fatal("untracked engine retained a generated ID")
		}
	}
	if n := trackedCount(untracked); n != 2 {
		t.Fatalf("untracked retained %d entries, want only two base IDs", n)
	}
	if n := trackedCount(tracked); n <= 1000 {
		t.Fatalf("control failed to retain intermediate history: %d", n)
	}
	for _, base := range []ID{"0", "1"} {
		if !untracked.Exists(base) {
			t.Fatalf("base %q absent", base)
		}
	}
	var zero Engine
	value := zero.FromString("zero-value compatibility")
	if !zero.Exists(value) {
		t.Fatal("zero-value engine lost tracking")
	}
}

func TestUntrackedConcurrentGenerationMatchesTracking(t *testing.T) {
	tracked, untracked := New(), NewUntracked()
	inputs := []string{"", "same input", "different input", "unicode λ", "repeated input"}
	wants := make([]ID, len(inputs))
	for i, input := range inputs {
		wants[i] = tracked.FromString(input)
	}
	var wg sync.WaitGroup
	for n := 0; n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, input := range inputs {
				if got := untracked.FromString(input); got != wants[i] {
					t.Errorf("got %q want %q", got, wants[i])
				}
			}
		}()
	}
	wg.Wait()
	if n := trackedCount(untracked); n != 2 {
		t.Fatalf("untracked retained %d entries", n)
	}
}
