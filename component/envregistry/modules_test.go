package envregistry

import (
	"strings"
	"testing"
)

func TestEmbeddedModulesDecodeStrictlyAndValidate(t *testing.T) {
	mods, err := DecodeModulesStrict(embeddedManifest)
	if err != nil {
		t.Fatalf("embedded modules do not decode strictly: %v", err)
	}
	if len(mods) == 0 {
		t.Fatal("the embedded manifest declares no modules; the block is missing, not empty")
	}
	m, err := LoadManifestFromBytes(embeddedManifest, "embedded")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateModules(); err != nil {
		t.Fatalf("embedded modules do not validate: %v", err)
	}
	core := map[string]bool{}
	for _, mod := range m.Modules {
		if mod.Core {
			core[mod.Name] = true
		}
	}
	for _, want := range []string{"ai", "storage", "email"} {
		if !core[want] {
			t.Errorf("module %q must be core (design record section 4.2)", want)
		}
	}
}

func TestModulesRefuseAnUnknownKey(t *testing.T) {
	doc := []byte("secrets: []\nvariables: []\nmodules:\n  - name: x\n    description: d\n    lane: oops\n")
	if _, err := DecodeModulesStrict(doc); err == nil || !strings.Contains(err.Error(), "lane") {
		t.Fatalf("an unknown key inside a module must refuse decode naming the key, got %v", err)
	}
}

func TestModulesRefuseAnUnknownSlot(t *testing.T) {
	doc := []byte("secrets: []\nvariables:\n  - name: MEMQL_KNOWN\n    scope: node\nmodules:\n" +
		"  - name: x\n    description: d\n    lanes:\n      - name: l\n        configurableFrom: os\n        slots: [MEMQL_KNOWN, MEMQL_TYPO]\n")
	m, err := LoadManifestFromBytes(doc, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	err = m.ValidateModules()
	if err == nil || !strings.Contains(err.Error(), "MEMQL_TYPO") {
		t.Fatalf("a slot naming no entry must fail validation naming the slot, got %v", err)
	}
}

func TestModulesRefuseLanesAndEvaluatorTogether(t *testing.T) {
	doc := []byte("secrets: []\nvariables:\n  - name: MEMQL_KNOWN\n    scope: node\nmodules:\n" +
		"  - name: x\n    description: d\n    evaluator: inferenceStatus\n    lanes:\n      - name: l\n        configurableFrom: os\n        slots: [MEMQL_KNOWN]\n")
	m, _ := LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil {
		t.Fatal("a module with both lanes and an evaluator must fail validation")
	}
	doc = []byte("secrets: []\nvariables: []\nmodules:\n  - name: x\n    description: d\n")
	m, _ = LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil {
		t.Fatal("a module with neither lanes nor an evaluator must fail validation")
	}
}

func TestModulesRefuseAnUnknownEvaluatorOrConfigurableFrom(t *testing.T) {
	doc := []byte("secrets: []\nvariables: []\nmodules:\n  - name: x\n    description: d\n    evaluator: magic\n")
	m, _ := LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("unknown evaluator must be refused by name, got %v", err)
	}
	doc = []byte("secrets: []\nvariables:\n  - name: MEMQL_KNOWN\n    scope: node\nmodules:\n" +
		"  - name: x\n    description: d\n    lanes:\n      - name: l\n        configurableFrom: elsewhere\n        slots: [MEMQL_KNOWN]\n")
	m, _ = LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil || !strings.Contains(err.Error(), "elsewhere") {
		t.Fatalf("configurableFrom outside {os, deployment} must be refused by value, got %v", err)
	}
}

func TestIsSecretDistinguishesTheTwoLists(t *testing.T) {
	doc := []byte("secrets:\n  - name: MEMQL_S\n    scope: node\nvariables:\n  - name: MEMQL_V\n    scope: node\n")
	m, _ := LoadManifestFromBytes(doc, "fixture")
	if !m.IsSecret("MEMQL_S") || m.IsSecret("MEMQL_V") || m.IsSecret("MEMQL_NONE") {
		t.Fatal("IsSecret must be true for a secrets entry only")
	}
}
