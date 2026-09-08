package worker

import (
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/memql"
)

// THE GATE THAT MAKES TWO READERS ONE READER (epic memql#5146).
//
// registration.hardware is written here, by component/worker, and read in two
// places: here, and by component/memql, which computes the machine class and
// the recommended set from it. Those two cannot share a type. component/worker
// reaches component/identity, which reaches component/memql, so the edge
// memql -> worker would be a cycle; the class function therefore holds its own
// MachineHardware and its own HardwareFromRow.
//
// A second reader of one shape drifts silently, and the drift here has a
// particularly bad shape: a field this side writes and that side stops reading
// does not error, it reads as ZERO -- which for memoryBytes means unsupported,
// for a runtime list means "no runtimes", and for the machine's owner means a
// page that says their Mac Studio cannot run anything.
//
// This test is on the LEGAL side of the edge (worker may import memql) and
// renders through the real Inventory.Row(), so it fails the build the moment
// the two spellings disagree on any field.
func TestHardwareRowReadsIdenticallyOnBothSidesOfTheModuleEdge(t *testing.T) {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	inv := Inventory{
		Chip:          "Apple M4 Max",
		MemoryBytes:   64 << 30,
		Gpu:           Gpu{Name: "Apple M4 Max", VramBytes: 0, Backend: GpuBackendMetal},
		CpuCores:      16,
		OsVersion:     "15.3",
		DiskFreeBytes: 900 << 30,
		Runtimes: []Runtime{
			{Name: RuntimeOllama, Version: "0.5.4"},
			{Name: RuntimeKokoro, Version: "1.0"},
		},
		ReportedAt: at,
	}

	there := memql.HardwareFromRow(inv.Row())

	if !there.Present() {
		t.Fatal("a present inventory must read as present on the class side")
	}
	if there.Chip != inv.Chip {
		t.Fatalf("chip: %q vs %q", there.Chip, inv.Chip)
	}
	if there.MemoryBytes != inv.MemoryBytes {
		t.Fatalf("memoryBytes: %d vs %d", there.MemoryBytes, inv.MemoryBytes)
	}
	if there.CpuCores != inv.CpuCores {
		t.Fatalf("cpuCores: %d vs %d", there.CpuCores, inv.CpuCores)
	}
	if there.OsVersion != inv.OsVersion {
		t.Fatalf("osVersion: %q vs %q", there.OsVersion, inv.OsVersion)
	}
	if there.DiskFreeBytes != inv.DiskFreeBytes {
		t.Fatalf("diskFreeBytes: %d vs %d", there.DiskFreeBytes, inv.DiskFreeBytes)
	}
	if there.Gpu.Name != inv.Gpu.Name || there.Gpu.VramBytes != inv.Gpu.VramBytes || there.Gpu.Backend != inv.Gpu.Backend {
		t.Fatalf("gpu: %+v vs %+v", there.Gpu, inv.Gpu)
	}
	// Compared as a SET keyed by name, deliberately. Both sides sort on
	// construction, so an index-by-index comparison against a literal written
	// in declaration order fails on the ordering rather than on the content --
	// which is what this assertion did on its first run. The ordering is worth
	// pinning too, and it is pinned separately below rather than folded in
	// here, so a future failure says which of the two things went wrong.
	if len(there.Runtimes) != len(inv.Runtimes) {
		t.Fatalf("runtimes: %d vs %d", len(there.Runtimes), len(inv.Runtimes))
	}
	versions := map[string]string{}
	for _, r := range there.Runtimes {
		versions[r.Name] = r.Version
	}
	for _, want := range inv.Runtimes {
		got, ok := versions[want.Name]
		if !ok {
			t.Fatalf("runtime %q is missing on the class side", want.Name)
		}
		if got != want.Version {
			t.Fatalf("runtime %q version: %q vs %q", want.Name, got, want.Version)
		}
	}
	// BOTH SIDES SORT, AND THEY MUST SORT THE SAME WAY. Two replicas reading one
	// row have to produce the same recommended set in the same order, and the
	// runtime list is an input to that -- an order that differed between the
	// write side and the read side would be a page that changes when it is
	// reloaded, which reads as a bug in the fleet rather than in a comparator.
	names := there.RuntimeNames()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("the class side must read runtimes in sorted order, got %v", names)
		}
	}
	if there.ReportedAt != at.Format(time.RFC3339) {
		t.Fatalf("reportedAt: %q vs %q", there.ReportedAt, at.Format(time.RFC3339))
	}
}

func TestAbsentHardwareIsAbsentOnBothSidesOfTheModuleEdge(t *testing.T) {
	// The reading of silence is what the whole field rests on, so it is worth
	// asserting across the edge and not only within each side. An absent
	// inventory writes no key here; the class side must read that as ClassUnknown
	// and never as ClassUnsupported.
	var absent Inventory
	there := memql.HardwareFromRow(absent.Row())
	if there.Present() {
		t.Fatal("an absent inventory must read as absent on the class side")
	}
	if got := memql.MachineClass(there); got != memql.ClassUnknown {
		t.Fatalf("class %q, want %q -- `unsupported` is an accusation and this machine has said nothing", got, memql.ClassUnknown)
	}
}

func TestTheEngineKnowsEveryRuntimeTheClassSideMightBeAskedAbout(t *testing.T) {
	// The two runtime vocabularies must not diverge in the direction that
	// matters: a runtime this package derives a label for, and therefore
	// advertises as a routing fact, must be one the class side can also see on
	// the row. The reverse is fine -- the class side reads whatever the machine
	// listed, known or not, because the machine really has it.
	inv := Inventory{Chip: "x", MemoryBytes: 1 << 30}
	for _, name := range KnownRuntimes() {
		inv.Runtimes = append(inv.Runtimes, Runtime{Name: name, Version: "1"})
	}
	there := memql.HardwareFromRow(inv.Row())
	for _, name := range KnownRuntimes() {
		if !there.HasRuntime(name) {
			t.Fatalf("the class side cannot see runtime %q, which this package advertises as a routing label", name)
		}
	}
}
