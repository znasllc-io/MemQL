package memql

import (
	"strings"
	"testing"
)

func macStudio(runtimes ...string) MachineHardware {
	h := MachineHardware{
		Chip:        "Apple M4 Max",
		MemoryBytes: 128 << 30,
		Gpu:         MachineGpu{Name: "Apple M4 Max", Backend: GpuBackendMetal},
		CpuCores:    16,
	}
	for _, r := range runtimes {
		h.Runtimes = append(h.Runtimes, MachineRuntime{Name: r, Version: "1"})
	}
	return h
}

func profile(id, level, runtime, minClass string, params int64, mut ...func(*CatalogProfile)) CatalogProfile {
	p := CatalogProfile{
		ModelId:         id,
		Category:        "text",
		Runtime:         runtime,
		MinMachineClass: minClass,
		RecommendedFor:  []string{level},
		Params:          params,
		ContextWindow:   128000,
	}
	for _, m := range mut {
		m(&p)
	}
	return p
}

func find(set []Recommendation, level string) (Recommendation, bool) {
	for _, r := range set {
		if r.Level == level {
			return r, true
		}
	}
	return Recommendation{}, false
}

func TestRecommendedSetPrefersHigherPrecisionWhenItFits(t *testing.T) {
	// Quantization changes storage, not the model's parameter count. The
	// catalog must choose the eight-bit variant on larger machines without
	// inflating its parameter count to influence ordering.
	rows := []map[string]any{
		{"modelId": "qwen3.8:27b", "params": int64(27_300_000_000), "quant": "Q4_K_M", "contextWindow": 262144, "runtime": "ollama", "minMachineClass": "24", "recommendedFor": []any{"strong"}},
		{"modelId": "qwen3.8:27b-q8_0", "params": int64(27_300_000_000), "quant": "Q8_0", "contextWindow": 262144, "runtime": "ollama", "minMachineClass": "64", "recommendedFor": []any{"strong"}},
	}
	for _, reverse := range []bool{false, true} {
		if reverse {
			rows[0], rows[1] = rows[1], rows[0]
		}
		for class, want := range map[string]string{"24": "qwen3.8:27b", "32": "qwen3.8:27b", "64": "qwen3.8:27b-q8_0", "128": "qwen3.8:27b-q8_0"} {
			got, ok := find(RecommendedSet(class, macStudio("ollama"), "darwin", catalogProfilesFromRows(rows)), "strong")
			if !ok || !got.Pullable() || got.Profile.ModelId != want {
				t.Errorf("class %s: got %+v, want %s", class, got, want)
			}
		}
	}
}

func TestRecommendedSetIsOnePerLevelStrongestFirst(t *testing.T) {
	// One entry per level, and within a level the strongest model wins. The
	// second key is parameters descending, which is orderModels' own -- so the
	// recommended set and the router agree about which of two models is
	// stronger, rather than each having an opinion.
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		profile("small:3b", "fast", "ollama", "16", 3_000_000_000),
		profile("mid:9b", "fast", "ollama", "16", 9_000_000_000),
		profile("big:32b", "reasoning", "ollama", "32", 32_000_000_000),
	})
	if len(set) != 2 {
		t.Fatalf("expected one entry for each of two levels, got %d: %+v", len(set), set)
	}
	fast, ok := find(set, "fast")
	if !ok {
		t.Fatal("no fast entry")
	}
	if fast.Profile.ModelId != "mid:9b" {
		t.Fatalf("fast should be the strongest pullable model, got %q", fast.Profile.ModelId)
	}
	// Presentation order is the level ladder, not the catalog's order.
	if set[0].Level != "fast" || set[1].Level != "reasoning" {
		t.Fatalf("levels must appear in presentation order, got %q then %q", set[0].Level, set[1].Level)
	}
}

func TestRecommendedSetSkipsAProfileAboveTheClass(t *testing.T) {
	// A 16 GB machine and a 64 GB model. The model is not recommended, and the
	// entry that IS returned says why -- see the next test for the case where
	// nothing at the level is pullable.
	h := MachineHardware{
		Chip:        "MacBook Air",
		MemoryBytes: 24 << 30,
		Gpu:         MachineGpu{Backend: GpuBackendMetal},
		Runtimes:    []MachineRuntime{{Name: "ollama", Version: "1"}},
	}
	set := RecommendedSet(MachineClass(h), h, "darwin", []CatalogProfile{
		profile("huge:70b", "strong", "ollama", "64", 70_000_000_000),
		profile("small:3b", "strong", "ollama", "16", 3_000_000_000),
	})
	strong, ok := find(set, "strong")
	if !ok {
		t.Fatal("no strong entry")
	}
	if strong.Profile.ModelId != "small:3b" {
		t.Fatalf("the entry must be the one the machine can actually run, got %q", strong.Profile.ModelId)
	}
	if !strong.Pullable() {
		t.Fatalf("small:3b is within a 16 GB machine's reach and must be pullable, blocked with %q", strong.Blocked)
	}
}

func TestAMissingRuntimeIsBlockedWithItsOwnSentence(t *testing.T) {
	// NAMED, not "a runtime is missing". The repair lives on that machine and
	// nowhere else, and a person cannot perform it without the name. Dropping
	// the row entirely -- the obvious filter -- answers "why can my machine not
	// do voice" with silence.
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		profile("voice:1b", "fast", "kokoro", "16", 1_000_000_000),
	})
	entry, ok := find(set, "fast")
	if !ok {
		t.Fatal("a blocked profile must still be returned; silence is indistinguishable from a catalog that never had one")
	}
	if entry.Pullable() {
		t.Fatal("a profile whose runtime the machine lacks must not be pullable")
	}
	if !strings.Contains(entry.Blocked, "Kokoro") {
		t.Fatalf("the sentence must name the runtime, got %q", entry.Blocked)
	}
	if gap := RuntimeGap(set, h); len(gap) != 1 || gap[0] != "kokoro" {
		t.Fatalf("the runtime gap must name kokoro, got %v", gap)
	}
}

func TestARuntimeTheInventoryCannotReportIsBlocked(t *testing.T) {
	// The catalog's runtime enum and the inventory's closed six OVERLAP but are
	// not the same set: `nemo` and `comfyui` are catalog runtimes the cockpit
	// does not install and cannot report. A profile naming one must be blocked
	// with a sentence, never recommended -- recommending a runtime no cockpit
	// reports produces a pull that fails on somebody's laptop for a reason the
	// page never showed.
	h := macStudio("ollama", "mlx", "whispercpp", "kokoro", "mflux", "docker")
	set := RecommendedSet("64", h, "linux", []CatalogProfile{
		profile("video:5b", "fast", "comfyui", "16", 5_000_000_000),
	})
	entry, ok := find(set, "fast")
	if !ok {
		t.Fatal("the entry must be returned")
	}
	if entry.Pullable() {
		t.Fatal("a machine reporting every runtime it CAN report must still be blocked on one it cannot")
	}
	if !strings.Contains(entry.Blocked, "ComfyUI") {
		t.Fatalf("the sentence must name the runtime, got %q", entry.Blocked)
	}
}

func TestAProfileNotOfferedOnThisOsIsBlocked(t *testing.T) {
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		profile("linuxonly:7b", "fast", "ollama", "16", 7_000_000_000, func(p *CatalogProfile) {
			p.OfferedOn = []string{"linux"}
		}),
	})
	entry, _ := find(set, "fast")
	if entry.Pullable() {
		t.Fatal("a linux-only profile must be blocked on a Mac")
	}
	if !strings.Contains(entry.Blocked, "macOS") {
		t.Fatalf("the sentence must name the platform in the words a person uses, got %q", entry.Blocked)
	}
}

func TestAMachineThatDidNotSayItsPlatformIsNotRuledOut(t *testing.T) {
	// Permissive in the direction that matters. A restriction the machine
	// cannot be checked against is not a reason to refuse it -- refusing would
	// rule out every cockpit that reports an inventory without a platform.
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "", []CatalogProfile{
		profile("linuxonly:7b", "fast", "ollama", "16", 7_000_000_000, func(p *CatalogProfile) {
			p.OfferedOn = []string{"linux"}
		}),
	})
	entry, _ := find(set, "fast")
	if !entry.Pullable() {
		t.Fatalf("an unstated platform must not block, got %q", entry.Blocked)
	}
}

func TestAnAbsentInventoryRecommendsNothingAndBlocksNothing(t *testing.T) {
	// An EMPTY LIST, not a list of blocked entries. A page listing every
	// profile as blocked reads as a machine that failed rather than one that
	// has not spoken, and on the day this shipped every machine had not spoken.
	set := RecommendedSet(ClassUnknown, MachineHardware{}, "darwin", []CatalogProfile{
		profile("small:3b", "fast", "ollama", "16", 3_000_000_000),
	})
	if len(set) != 0 {
		t.Fatalf("a machine that has not reported has nothing to recommend and nothing to explain, got %+v", set)
	}
}

func TestAnUnsupportedMachineSaysTheFloorRatherThanTheClass(t *testing.T) {
	// The sentence for a machine under the floor is about the FLOOR, not about
	// a per-profile comparison: telling somebody their 8 GB laptop is "class
	// unsupported, needs 16" once per profile is six copies of one fact.
	h := MachineHardware{Chip: "old laptop", MemoryBytes: 8 << 30, Gpu: MachineGpu{Backend: GpuBackendNone}}
	set := RecommendedSet(MachineClass(h), h, "linux", []CatalogProfile{
		profile("small:3b", "fast", "ollama", "16", 3_000_000_000),
	})
	entry, _ := find(set, "fast")
	if entry.Pullable() {
		t.Fatal("nothing is pullable under the floor")
	}
	if !strings.Contains(entry.Blocked, "under the floor") {
		t.Fatalf("the sentence must be about the floor, got %q", entry.Blocked)
	}
}

func TestABlockedEntryNeverDisplacesAPullableOne(t *testing.T) {
	// The first sort key, and what makes "one per level" honest. A blocked
	// entry only reaches the set when nothing at that level is pullable, which
	// is exactly when its reason is worth showing.
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		// Bigger, and blocked. Under the old key order it would have won.
		profile("voice:70b", "fast", "kokoro", "16", 70_000_000_000),
		profile("small:3b", "fast", "ollama", "16", 3_000_000_000),
	})
	entry, _ := find(set, "fast")
	if entry.Profile.ModelId != "small:3b" {
		t.Fatalf("a pullable entry must win over a bigger blocked one, got %q", entry.Profile.ModelId)
	}
}

func TestWhenNothingIsPullableTheFIXABLEBlockIsShown(t *testing.T) {
	// THE CASE THAT PRODUCED THE FIXABILITY RANK, and it was found by a test
	// fixture rather than by review. Order blocked entries by STRENGTH and a
	// person whose only real problem is that Ollama is not installed reads
	// "needs a 64 GB machine" -- because the 70B model's complaint is about
	// size and it sorts first on parameters. They go shopping.
	//
	// A missing runtime is the only block with a repair the reader can perform,
	// so it is the one worth putting in front of them.
	h := MachineHardware{
		Chip:        "MacBook Air",
		MemoryBytes: 24 << 30,
		Gpu:         MachineGpu{Backend: GpuBackendMetal},
		// No runtimes at all.
	}
	set := RecommendedSet(MachineClass(h), h, "darwin", []CatalogProfile{
		profile("huge:70b", "strong", "ollama", "64", 70_000_000_000),
		profile("small:3b", "strong", "ollama", "16", 3_000_000_000),
	})
	entry, ok := find(set, "strong")
	if !ok {
		t.Fatal("no strong entry")
	}
	if entry.Pullable() {
		t.Fatal("nothing is pullable with no runtime reported")
	}
	if !strings.Contains(entry.Blocked, "Ollama") {
		t.Fatalf("the entry shown must be the one a person can act on, got %q", entry.Blocked)
	}
	if entry.Profile.ModelId != "small:3b" {
		t.Fatalf("the runtime-blocked entry must win over the bigger class-blocked one, got %q", entry.Profile.ModelId)
	}
}

func TestUnknownParametersSortLast(t *testing.T) {
	// orderModels' rule, and it must hold here for the same reason: a profile
	// that does not say how big it is must not win by silence. Sorting it first
	// would quietly make every unstated entry the fleet's strongest model.
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		profile("unstated:?", "strong", "ollama", "16", 0),
		profile("stated:3b", "strong", "ollama", "16", 3_000_000_000),
	})
	entry, _ := find(set, "strong")
	if entry.Profile.ModelId != "stated:3b" {
		t.Fatalf("a model that states its size must beat one that does not, got %q", entry.Profile.ModelId)
	}
}

func TestRecommendedSetIsStableAcrossReplicas(t *testing.T) {
	// Two replicas reading one catalog must produce the same set in the same
	// order with no shared state, or the machine page changes when it is
	// reloaded -- which reads as a bug in the fleet rather than in a
	// comparator. The last key is the model id, so ties are broken by a value
	// rather than by map iteration.
	h := macStudio("ollama")
	catalog := []CatalogProfile{
		profile("bbb:9b", "fast", "ollama", "16", 9_000_000_000),
		profile("aaa:9b", "fast", "ollama", "16", 9_000_000_000),
		profile("ccc:9b", "fast", "ollama", "16", 9_000_000_000),
	}
	first := RecommendedSet("64", h, "darwin", catalog)
	for i := 0; i < 50; i++ {
		again := RecommendedSet("64", h, "darwin", catalog)
		if len(again) != len(first) || again[0].Profile.ModelId != first[0].Profile.ModelId {
			t.Fatalf("run %d disagreed: %q vs %q", i, again[0].Profile.ModelId, first[0].Profile.ModelId)
		}
	}
	if first[0].Profile.ModelId != "aaa:9b" {
		t.Fatalf("ties must break on the model id, got %q", first[0].Profile.ModelId)
	}
}

func TestAProfileRecommendedForTwoLevelsAppearsUnderBoth(t *testing.T) {
	// The set answers a question PER LEVEL -- "what should serve my fast
	// calls" -- so one row standing for two answers would make the
	// strongest-first order within a level meaningless.
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		profile("both:9b", "fast", "ollama", "16", 9_000_000_000, func(p *CatalogProfile) {
			p.RecommendedFor = []string{"fast", "strong"}
		}),
	})
	if len(set) != 2 {
		t.Fatalf("expected the profile under both levels, got %+v", set)
	}
}

func TestAProfileRecommendedForNothingIsNotInTheSet(t *testing.T) {
	// An empty recommendedFor is how the catalog is honest about a model it
	// lists and recommends nowhere -- videoGen is the shipped case. It must not
	// become a recommendation by default.
	h := macStudio("ollama")
	set := RecommendedSet("64", h, "darwin", []CatalogProfile{
		profile("listed:5b", "", "ollama", "16", 5_000_000_000, func(p *CatalogProfile) {
			p.RecommendedFor = nil
		}),
	})
	if len(set) != 0 {
		t.Fatalf("a profile recommended for nothing must not be recommended, got %+v", set)
	}
}
