package memql

import "testing"

// fleet_machine_facts_5195_test.go -- memql#5195.
//
// The catalog's machine-class floor could never be checked, because the memory a
// machine reports never reached the code that would compare it: nothing on a
// fleetModel row's machine entries carried memory or platform, so `joinCatalog`
// guarded the comparison on a fleet size that was never reported.
//
// These tests pin the two facts the wire now carries and the one vocabulary they
// are carried in. The client half lives in clients/os/test/fleet/.

// TestUsableGigabytesIsTheFigureTheLadderCompares: exported so the reader that
// SHOWS a figure to an operator prints the number the engine DECIDES on. Two
// implementations would be two answers, and they would disagree in the direction
// that promises somebody a model their machine then refuses.
func TestUsableGigabytesIsTheFigureTheLadderCompares(t *testing.T) {
	// 48 GB of unified memory: 75 percent is 36, not 48, and the class is 32.
	unified := MachineHardware{
		Chip:        "Apple M4 Max",
		MemoryBytes: 48 << 30,
		Gpu:         MachineGpu{Backend: GpuBackendMetal},
	}
	if got := UsableGigabytes(unified); got != 36 {
		t.Errorf("UsableGigabytes(48 GB unified) = %d, want 36 (75 percent of the pool)", got)
	}
	if got := MachineClass(unified); got != "32" {
		t.Errorf("MachineClass = %q, want %q -- the figure and the rung must come from one arithmetic", got, "32")
	}

	// A discrete card is its VRAM, not the system RAM behind it.
	discrete := MachineHardware{
		Chip:        "Threadripper",
		MemoryBytes: 128 << 30,
		Gpu:         MachineGpu{Name: "RTX 4080", VramBytes: 16376 << 20, Backend: GpuBackendCuda},
	}
	if got := UsableGigabytes(discrete); got != 16 {
		t.Errorf("UsableGigabytes(16 GB card behind 128 GB of RAM) = %d, want 16", got)
	}

	// ABSENT IS ZERO AND ZERO IS NOT "NO MEMORY". Every reader downstream takes
	// it as "has not said"; a reader that took it as a measurement would tell an
	// operator their machine is too small when nobody has asked it yet.
	if got := UsableGigabytes(MachineHardware{}); got != 0 {
		t.Errorf("UsableGigabytes(absent inventory) = %d, want 0", got)
	}
}

// TestNormalizePlatformSpeaksTheCatalogsVocabulary: the machine says `darwin`,
// the catalog declares `macos`, and nothing normalized between them.
func TestNormalizePlatformSpeaksTheCatalogsVocabulary(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"darwin", "macos"},
		{"Darwin", "macos"},
		{" darwin ", "macos"},
		{"linux", "linux"},
		{"", ""},
		// An unknown GOOS passes through rather than being blanked: an empty
		// platform is PERMISSIVE, so blanking a word we have no catalog spelling
		// for would silently widen what that machine is offered.
		{"windows", "windows"},
	} {
		if got := NormalizePlatform(tc.in); got != tc.want {
			t.Errorf("NormalizePlatform(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestAMacOsOnlyProfileIsOfferedOnAMac is the control that was missing, and its
// absence is the whole reason the vocabulary bug survived.
//
// Every existing offeredOn test uses OfferedOn: ["linux"] against os "darwin",
// which passes whether or not the two vocabularies agree -- it is blocked either
// way. Only the pair that must MATCH can tell a working comparison from a broken
// one, and the catalog ships exactly such a profile.
func TestAMacOsOnlyProfileIsOfferedOnAMac(t *testing.T) {
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		profile("maconly:7b", "fast", "ollama", "16", 7_000_000_000, func(p *CatalogProfile) {
			p.OfferedOn = []string{"macos"}
		}),
	})
	entry, ok := find(set, "fast")
	if !ok {
		t.Fatal("a macOS-only profile produced no recommendation at all on a Mac")
	}
	if !entry.Pullable() {
		t.Fatalf("a macOS-only profile must be offered on a Mac; it was blocked with %q. "+
			"The machine reports Go's GOOS (`darwin`) and the catalog declares `macos` -- "+
			"without NormalizePlatform this compares two spellings of one platform and answers false", entry.Blocked)
	}
}
