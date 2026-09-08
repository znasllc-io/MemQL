package memql

// The MACHINE CLASS (epic memql#5146, design D2).
//
// ===========================================================================
// WHAT A CLASS IS FOR
// ===========================================================================
// The catalog says what a machine SHOULD pull, by category and by machine
// class (`minMachineClass` on v1:models:modelProfile). A class is the other
// half of that comparison: the largest of the five sizes a machine's usable
// memory reaches. Together they answer "which of these models is this machine
// able to run", which is a question nobody could ask before the scanner,
// because the cluster was never told what a machine was.
//
// IT IS COMPUTED AND STORED NOWHERE. A class is a pure function of the
// reported inventory, so a stored copy would be a second answer that ages
// while the first one cannot -- and the staleness would be invisible, because
// a class that was right last week looks exactly like a class that is right
// now.
//
// ===========================================================================
// THE READING OF SILENCE IS THE LOAD-BEARING PART
// ===========================================================================
// There are two ways for a machine not to have a class and they are DIFFERENT
// SENTENCES:
//
//	""             the cockpit has not reported an inventory. The machine is
//	               fine and there is nothing to fix. On the day this field
//	               shipped, this was every machine in every fleet.
//	"unsupported"  the machine reported, and what it reported is under the
//	               floor. This one is an accusation, and levelling it at a
//	               fleet that had merely not upgraded its cockpits would have
//	               been wrong about every machine at once.
//
// ClassAtLeast therefore returns false for "" in every direction: a machine
// that has not spoken cannot clear a floor by silence.
//
// ===========================================================================
// WHY THIS PACKAGE HOLDS ITS OWN READER
// ===========================================================================
// component/worker owns the Inventory type and cannot be imported here --
// worker reaches component/identity, which reaches this package, so the edge
// would be a cycle. This package reads the registration ROW out of the graph
// anyway (modelPullMachineFor already does), so taking the row's own shape is
// the natural surface rather than a workaround.
//
// The duplication that buys is held by a gate on the LEGAL side of the edge:
// component/worker/hardware_parity_test.go renders an Inventory through
// Inventory.Row(), reads it back through HardwareFromRow here, and fails the
// build if the two readings disagree on any field. Two readers of one shape
// drift silently; a gate is what makes them one reader with two spellings.

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/znasllc-io/memql/core/num"
)

// Class values that are not a size. Both mean "no class", and they are
// different sentences -- see the file comment.
const (
	// ClassUnknown is a machine whose cockpit has not reported an inventory.
	ClassUnknown = ""
	// ClassUnsupported is a machine that reported, under the floor.
	ClassUnsupported = "unsupported"
)

// machineClasses is the closed ladder, ascending. A machine's class is the
// largest rung its usable memory reaches.
//
// The rungs are the sizes people actually buy, and they are the same values
// modelProfile.minMachineClass declares, because the comparison between the
// two is a string equality on this ladder and a rung on one side with no
// counterpart on the other is a profile that can never be recommended.
var machineClasses = []uint64{16, 24, 32, 64, 128}

// MachineClasses returns the closed ladder, ascending.
func MachineClasses() []uint64 {
	out := make([]uint64, len(machineClasses))
	copy(out, machineClasses)
	return out
}

// MachineGpu is the accelerator half of a reported inventory.
type MachineGpu struct {
	Name      string
	VramBytes uint64
	// Backend is metal, cuda, rocm or none -- what the machine can RUN rather
	// than what is installed.
	Backend string
}

// MachineRuntime is one runtime the machine reported.
type MachineRuntime struct {
	Name    string
	Version string
}

// MachineHardware is this package's reading of registration.hardware.
type MachineHardware struct {
	Chip          string
	MemoryBytes   uint64
	Gpu           MachineGpu
	CpuCores      int
	OsVersion     string
	DiskFreeBytes uint64
	Runtimes      []MachineRuntime
	ReportedAt    string
}

// Backend values, mirroring component/worker's closed set.
const (
	GpuBackendMetal = "metal"
	GpuBackendCuda  = "cuda"
	GpuBackendRocm  = "rocm"
	GpuBackendNone  = "none"
)

// Present reports whether the machine actually told us anything. Decided by
// CONTENT rather than by whether a key existed, for the reason in the file
// comment: an object of zeroes is a statement, and silence is not.
func (h MachineHardware) Present() bool {
	return h.Chip != "" || h.MemoryBytes > 0 || h.CpuCores > 0 ||
		h.Gpu.Backend != "" || h.Gpu.Name != "" || len(h.Runtimes) > 0
}

// HasRuntime reports whether the machine listed this runtime.
func (h MachineHardware) HasRuntime(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false
	}
	for _, r := range h.Runtimes {
		if strings.EqualFold(r.Name, name) {
			return true
		}
	}
	return false
}

// RuntimeNames returns the runtimes the machine listed, sorted.
func (h MachineHardware) RuntimeNames() []string {
	out := make([]string, 0, len(h.Runtimes))
	for _, r := range h.Runtimes {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

// HardwareFromRow reads registration.hardware.
//
// A missing key and an empty object both read as ABSENT, which is the whole
// point: a registration written before the field existed has not reported, and
// that is not a machine with nothing.
func HardwareFromRow(v any) MachineHardware {
	row, ok := v.(map[string]any)
	if !ok || len(row) == 0 {
		return MachineHardware{}
	}
	h := MachineHardware{
		Chip:          hardwareString(row, "chip"),
		MemoryBytes:   hardwareBytes(row, "memoryBytes"),
		CpuCores:      hardwareInt(row, "cpuCores"),
		OsVersion:     hardwareString(row, "osVersion"),
		DiskFreeBytes: hardwareBytes(row, "diskFreeBytes"),
		ReportedAt:    hardwareString(row, "reportedAt"),
	}
	if gpu, ok := row["gpu"].(map[string]any); ok {
		h.Gpu = MachineGpu{
			Name:      hardwareString(gpu, "name"),
			VramBytes: hardwareBytes(gpu, "vramBytes"),
			Backend:   hardwareString(gpu, "backend"),
		}
	}
	if list, ok := row["runtimes"].([]any); ok {
		for _, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := hardwareString(entry, "name")
			if name == "" {
				continue
			}
			h.Runtimes = append(h.Runtimes, MachineRuntime{
				Name:    name,
				Version: hardwareString(entry, "version"),
			})
		}
		sort.Slice(h.Runtimes, func(i, j int) bool {
			if h.Runtimes[i].Name != h.Runtimes[j].Name {
				return h.Runtimes[i].Name < h.Runtimes[j].Name
			}
			return h.Runtimes[i].Version < h.Runtimes[j].Version
		})
	}
	return h
}

// UsableBytes is how much memory the weights can actually have.
//
// THREE CASES, AND THE MIDDLE ONE IS THE TRAP:
//
//   - metal: 75 percent of UNIFIED memory. The remaining quarter is the
//     operating system and everything else the person is running; a model
//     sized to the whole pool swaps, which is slower than not running it.
//   - cuda / rocm: the CARD's memory, and NOTHING else. A machine with 128 GB
//     of system RAM behind an 8 GB card is an 8 GB machine for this purpose,
//     because the weights have to fit in the card. Reading system memory here
//     would recommend a 70B model to a laptop with a mobile GPU.
//   - none: 75 percent of SYSTEM memory. A CPU-only machine is not
//     `unsupported`; it is SLOW, which is a different answer and one the probe
//     is there to measure. Refusing it outright would rule out every server
//     with a lot of RAM and no accelerator.
func UsableBytes(h MachineHardware) uint64 {
	switch h.Gpu.Backend {
	case GpuBackendCuda, GpuBackendRocm:
		return h.Gpu.VramBytes
	default:
		return h.MemoryBytes / 4 * 3
	}
}

// MachineClass is the largest class the machine's usable memory reaches, or
// one of the two non-size answers.
func MachineClass(h MachineHardware) string {
	if !h.Present() {
		return ClassUnknown
	}
	gb := usableGigabytes(UsableBytes(h))
	out := ClassUnsupported
	for _, c := range machineClasses {
		if gb >= c {
			out = strconv.FormatUint(c, 10)
		}
	}
	return out
}

// usableGigabytes converts a byte count to whole gigabytes by ROUNDING TO
// NEAREST, and the choice of rounding is a defect fix rather than a taste.
//
// ===========================================================================
// FLOORING MISCLASSIFIES ALMOST EVERY DISCRETE GPU A WHOLE STEP LOW
// ===========================================================================
// `nvidia-smi` reports total memory MINUS the driver's reservation, so a
// card's real figure sits just under its nominal size. Measured by
// `memql worker hardware` on real hardware:
//
//	RTX 4090 / 5090 Laptop  24564 MiB = 23.988 GiB  floors to 23 -> class 16
//	RTX 4080                16376 MiB = 15.992 GiB  floors to 15 -> unsupported
//	A6000                   49140 MiB = 47.988 GiB  floors to 47 -> 32 (right by luck)
//	H100                    81559 MiB = 79.647 GiB  floors to 79 -> 64 (right by luck)
//
// Systematic, always in the same direction, and the two that break are the two
// most common cards in a fleet. The Metal path is unaffected -- 75 percent of a
// power-of-two pool is exact -- which is exactly why the arithmetic looked
// right until a real card reported a real number.
//
// THE BOUND WORTH STATING: rounding moves a figure by at most half a gigabyte
// and every rung is a multiple of eight, so it absorbs the reserved-memory slop
// and nothing else. A card that genuinely has 15 GB still rounds to 15 and
// stays unsupported.
//
// The cockpit fixed its own copy the same way
// (memql-cockpit internal/worker/hardware/class.go, TestClassAbsorbsReservedGPUMemory).
func usableGigabytes(b uint64) uint64 {
	const gb = uint64(1) << 30
	return (b + gb/2) / gb
}

// UsableGigabytes is the machine's usable memory in whole gigabytes -- the
// figure MachineClass compares against the ladder, exported so the readers that
// SHOW it to an operator get the same number the engine decides on.
//
// There is exactly one implementation of this arithmetic on purpose. A second
// one would be a second answer: the three-case rule in UsableBytes (a discrete
// card is its VRAM, everything else is 75 percent of the pool) and the
// round-to-nearest in usableGigabytes are both defect fixes, and a caller that
// re-derived "memory in GB" from memoryBytes would reproduce neither -- it would
// tell somebody with a 48 GB Mac that they have 48 GB to give a model, and then
// block the 32 GB entry they were promised.
//
// Zero for a machine that has not reported. Callers must ask Present() rather
// than reading zero as "no memory".
func UsableGigabytes(h MachineHardware) uint64 {
	if !h.Present() {
		return 0
	}
	return usableGigabytes(UsableBytes(h))
}

// NormalizePlatform maps an operating system as the MACHINE reports it onto the
// vocabulary the CATALOG declares.
//
// The two never agreed. `platformInfo.os` is Go's GOOS, so a Mac says `darwin`;
// `modelProfile.offeredOn` is declared `macos | linux` and every seed spells it
// `macos`. Nothing normalized between them, so `offeredOn` compared `darwin`
// against `macos` and answered false -- which made the ONLY macOS-only profile
// in the catalog unavailable on macOS, with the sentence "Not offered on macOS.
// This entry is macos only." The tests missed it because every recommend-path
// fixture used OfferedOn: ["linux"] and every client fixture used
// platform: "macos", so neither side ever spelled the pair that collides.
//
// ONE VOCABULARY CROSSES THE WIRE. This runs where the platform is stamped onto
// a machine entry, so every reader downstream -- the catalog join, the
// recommended set, the client -- compares catalog words against catalog words.
// An unknown GOOS passes through lowercased rather than being blanked: an empty
// platform blocks nothing (offeredOn treats it as permissive), so blanking a
// value we simply do not have a catalog word for would silently widen what a
// machine is offered.
func NormalizePlatform(os string) string {
	switch v := strings.ToLower(strings.TrimSpace(os)); v {
	case "darwin":
		return "macos"
	default:
		return v
	}
}

// ClassAtLeast reports whether a machine of class `machine` meets a floor of
// `need` -- the comparison behind "is this profile offered on this machine".
//
// IT FAILS CLOSED ON AN UNKNOWN CLASS, and that is the direction that matters.
// The natural spelling of this comparison is numeric, and a machine that has
// not reported parses as zero, which clears no floor -- but `unsupported`
// parses as zero too, and so does a typo. Answering false for anything that is
// not a rung on the ladder is what stops a machine being recommended a model
// on the strength of having said nothing.
func ClassAtLeast(machine, need string) bool {
	m, ok := classRung(machine)
	if !ok {
		return false
	}
	n, ok := classRung(need)
	if !ok {
		// A profile whose floor is not a rung is a catalog row this engine
		// cannot place. Refusing it is the same fail-closed direction: a
		// recommendation nobody can justify is worse than one profile missing
		// from a page.
		return false
	}
	return m >= n
}

func classRung(v string) (uint64, bool) {
	v = strings.TrimSpace(v)
	if v == "" || v == ClassUnsupported {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, false
	}
	for _, c := range machineClasses {
		if n == c {
			return n, true
		}
	}
	return 0, false
}

func hardwareString(row map[string]any, key string) string {
	s, _ := row[key].(string)
	return strings.TrimSpace(s)
}

func hardwareInt(row map[string]any, key string) int {
	switch n := row[key].(type) {
	case int:
		return n
	case int64:
		return num.ClampInt64(n)
	case float64:
		return num.ClampFloat64(n)
	}
	return 0
}

// hardwareBytes narrows a decoded payload number to a byte count.
//
// narrowing: GUARDED -- the bound is inline and both ends are load-bearing.
// A NEGATIVE value reads as ZERO rather than wrapping to an enormous positive:
// a machine cannot have minus four gigabytes, and the wrap would class it as
// the largest machine in the fleet and recommend it everything -- the one
// direction this file is built not to fail in. An ABSURDLY LARGE float
// saturates at the top for the same reason read the other way: the figure is
// an ORDERING, a cockpit reporting more memory than the address space can hold
// is a corrupt reading rather than a claim, and `uint64(x)` above the range is
// implementation-defined and answers with the integer indefinite value.
//
// It is not core/num's because core/num narrows to `int` and this is a `uint64`
// byte count; adding a uint64 arm there for two call sites would widen the one
// narrowing seam rather than use it.
func hardwareBytes(row map[string]any, key string) uint64 {
	switch n := row[key].(type) {
	case uint64:
		return n
	case int:
		if n < 0 {
			return 0
		}
		return uint64(n)
	case int64:
		if n < 0 {
			return 0
		}
		return uint64(n)
	case float64:
		if n <= 0 {
			return 0
		}
		if n >= math.MaxUint64 {
			return math.MaxUint64
		}
		return uint64(n)
	}
	return 0
}
