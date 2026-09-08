package memql

import "testing"

// mib builds a byte count from mebibytes, which is the unit nvidia-smi reports
// in and therefore the unit the real measurements below are quoted in.
func mib(n uint64) uint64 { return n << 20 }

func discrete(vram uint64) MachineHardware {
	return MachineHardware{
		Chip:        "AMD Ryzen 9 7950X",
		MemoryBytes: 128 << 30,
		Gpu:         MachineGpu{Name: "card", VramBytes: vram, Backend: GpuBackendCuda},
		CpuCores:    16,
	}
}

func TestClassAbsorbsReservedGpuMemory(t *testing.T) {
	// THE DEFECT THIS FUNCTION WAS WRITTEN AROUND. nvidia-smi reports TOTAL
	// MINUS THE DRIVER'S RESERVATION, so a card's real figure sits just under
	// its nominal size. Flooring to whole gigabytes misclassifies almost every
	// discrete GPU a whole step low -- systematically, always in the same
	// direction, and the two that break are the two most common cards in a
	// fleet.
	//
	// The figures are measured, by `memql worker hardware` on real hardware,
	// and the cockpit asserts the same five in
	// internal/worker/hardware/class.go's TestClassAbsorbsReservedGPUMemory.
	//
	// The bound worth stating in review: rounding to nearest moves a figure by
	// at most half a gigabyte and every rung is a multiple of eight, so it
	// absorbs the reserved-memory slop and nothing else -- which is what the
	// last row pins.
	cases := []struct {
		name string
		mib  uint64
		want string
	}{
		{"RTX 4090 / 5090 Laptop", 24564, "24"},
		{"RTX 4080", 16376, "16"},
		{"A6000", 49140, "32"},
		{"H100", 81559, "64"},
		{"a card that genuinely has 15 GB stays under the floor", 15360, ClassUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MachineClass(discrete(mib(tc.mib))); got != tc.want {
				t.Fatalf("%d MiB: class %q, want %q", tc.mib, got, tc.want)
			}
		})
	}
}

func TestClassOfAnAbsentInventoryIsUnknownNotUnsupported(t *testing.T) {
	// The two non-size answers are DIFFERENT SENTENCES. "" says the cockpit has
	// not reported and there is nothing to fix; "unsupported" says the machine
	// reported and is under the floor, which is an accusation. On the day this
	// field shipped the first was true of every machine in every fleet, so
	// collapsing them would have been wrong about all of them at once.
	if got := MachineClass(MachineHardware{}); got != ClassUnknown {
		t.Fatalf("an absent inventory must class as %q, got %q", ClassUnknown, got)
	}
}

func TestUsableBytesIsThreeQuartersOfUnifiedMemoryOnMetal(t *testing.T) {
	// The remaining quarter is the operating system and everything else the
	// person is running. A model sized to the whole pool swaps, which is slower
	// than not running it at all.
	h := MachineHardware{
		Chip:        "Apple M4 Max",
		MemoryBytes: 64 << 30,
		Gpu:         MachineGpu{Name: "Apple M4 Max", Backend: GpuBackendMetal},
	}
	if got := UsableBytes(h); got != 48<<30 {
		t.Fatalf("usable bytes %d, want %d", got, uint64(48)<<30)
	}
	if got := MachineClass(h); got != "32" {
		t.Fatalf("a 64 GB M4 Max classes as %q, want 32 -- 48 GB usable does not reach the 64 rung", got)
	}
}

func TestUsableBytesIsVramOnADiscreteGpuAndIgnoresSystemMemory(t *testing.T) {
	// A machine with 128 GB of system RAM behind an 8 GB card is an 8 GB
	// machine for this purpose: the weights have to fit in the card. Reading
	// system memory here would recommend a 70B model to a laptop with a mobile
	// GPU, and the failure would land on that laptop rather than here.
	h := discrete(8 << 30)
	if got := UsableBytes(h); got != 8<<30 {
		t.Fatalf("usable bytes %d, want the card's %d", got, uint64(8)<<30)
	}
	if got := MachineClass(h); got != ClassUnsupported {
		t.Fatalf("8 GB of VRAM is under the floor whatever the system RAM is, got %q", got)
	}
}

func TestABackendOfNoneFallsBackToSystemMemory(t *testing.T) {
	// A CPU-only machine is not `unsupported` -- it is SLOW, which is a
	// different answer and one the probe exists to measure. Refusing it here
	// would rule out every server with a lot of RAM and no accelerator, which
	// is a real and useful shape of machine.
	h := MachineHardware{
		Chip:        "Xeon",
		MemoryBytes: 64 << 30,
		Gpu:         MachineGpu{Backend: GpuBackendNone},
		CpuCores:    32,
	}
	if got := MachineClass(h); got != "32" {
		t.Fatalf("a 64 GB CPU-only machine classes as %q, want 32", got)
	}
}

func TestAUnifiedMachineReportingNoVramIsNotACardWithNoMemory(t *testing.T) {
	// gpu.vramBytes is ZERO on unified memory, where memoryBytes is the pool.
	// Reading that zero as "no accelerator" would class every Apple Silicon
	// machine as unsupported -- the single most common machine this feature is
	// for.
	h := MachineHardware{
		Chip:        "Apple M4 Max",
		MemoryBytes: 128 << 30,
		Gpu:         MachineGpu{Name: "Apple M4 Max", VramBytes: 0, Backend: GpuBackendMetal},
	}
	if got := MachineClass(h); got != "64" {
		t.Fatalf("a 128 GB M4 Max classes as %q, want 64 -- 96 GB usable reaches the 64 rung and not the 128 one", got)
	}
}

func TestClassAtLeastIsFalseForAnUnknownClass(t *testing.T) {
	// THE FAIL-CLOSED DIRECTION. The natural spelling of this comparison is
	// numeric, and a machine that has not reported parses as zero -- which
	// clears no floor, so the natural spelling looks correct. But `unsupported`
	// parses as zero too, and so does a typo, and so does a class value from a
	// future engine. Answering false for anything that is not a rung is what
	// stops a machine being recommended a model on the strength of having said
	// nothing.
	for _, machine := range []string{ClassUnknown, ClassUnsupported, "20", "banana"} {
		if ClassAtLeast(machine, "16") {
			t.Fatalf("class %q must not clear the 16 floor", machine)
		}
	}
	if !ClassAtLeast("64", "32") {
		t.Fatal("a 64 machine must clear a 32 floor")
	}
	if !ClassAtLeast("32", "32") {
		t.Fatal("the comparison is at-least, so equal must clear")
	}
	if ClassAtLeast("32", "64") {
		t.Fatal("a 32 machine must not clear a 64 floor")
	}
}

func TestClassAtLeastRefusesAFloorThatIsNotARung(t *testing.T) {
	// A catalog row whose minMachineClass is not on the ladder is one this
	// engine cannot place. Refusing it is the same direction: a recommendation
	// nobody can justify is worse than one profile missing from a page.
	if ClassAtLeast("128", "48") {
		t.Fatal("a floor that is not a rung must not be cleared by anything")
	}
}

func TestHardwareFromRowOfNothingIsAbsent(t *testing.T) {
	if HardwareFromRow(nil).Present() {
		t.Fatal("a missing hardware key must read as absent")
	}
	if HardwareFromRow(map[string]any{}).Present() {
		t.Fatal("an empty hardware object must read as absent, not as a machine with nothing")
	}
}

func TestHardwareFromRowRefusesToWrapANegativeByteCount(t *testing.T) {
	// A wrap would produce an enormous positive, class the machine as the
	// largest in the fleet, and recommend it everything -- the one direction
	// this file must not fail in. Reading it as zero classes the machine
	// unsupported, which is wrong in the harmless direction and visible.
	h := HardwareFromRow(map[string]any{
		"chip":        "odd",
		"memoryBytes": float64(-1),
		"gpu":         map[string]any{"backend": GpuBackendNone, "vramBytes": float64(-1)},
	})
	if h.MemoryBytes != 0 || h.Gpu.VramBytes != 0 {
		t.Fatalf("a negative byte count must read as zero, got %+v", h)
	}
	if got := MachineClass(h); got != ClassUnsupported {
		t.Fatalf("class %q, want %q", got, ClassUnsupported)
	}
}
