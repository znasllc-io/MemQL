package fleetcatalog

import "testing"

func TestModelMaxConcurrentRejectsOutOfRangeValues(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  uint32
	}{
		{"max=1", 1},
		{"max=4294967295", 4294967295},
		{"max=4294967296", 0},
		{"max=4294967297", 0},
		{"max=18446744073709551616", 0},
		{"max=-1", 0},
		{"max=0", 0},
		{"max=invalid", 0},
		// Malformed attributes are ignored, preserving an earlier valid value.
		{"max=5,max=4294967296", 5},
		{"max=5,max=-1", 5},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if got := ParseModelAttributes(tc.label).MaxConcurrent; got != tc.want {
				t.Fatalf("MaxConcurrent=%d, want %d", got, tc.want)
			}
		})
	}
}
