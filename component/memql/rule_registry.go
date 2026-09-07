package memql

// rule_registry.go holds the loaded `rule` corpus: the record of one rule as
// the DSL declared it, and the registry that owns evaluation ORDER.
//
// The split with component/router is deliberate. This package owns what a rule
// IS and the order the set is walked in -- both properties of the loaded
// corpus, computed once at load so every replica agrees with no shared state.
// component/router owns what a rule DOES: matching a request against the
// conditions, applying the level override and the excludes, and walking the
// chain. A rule is data here and a decision there.

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/znasllc-io/memql/core/airoute"
)

// OnUnavailable values -- what a rule says should happen when the chain is
// exhausted at the requested level (design D9).
const (
	// OnUnavailableDegrade walks the chain again at the next level down and
	// records that it did. It is the default when a rule says nothing.
	OnUnavailableDegrade = "degrade"
	// OnUnavailablePark returns the refusal with the door report and lets the
	// caller do what it does today: a work step parks, an interactive surface
	// shows the sentence.
	OnUnavailablePark = "park"
)

// RuleWhenKeys is the CLOSED set of @when keys, in the order an error message
// lists them. Every key is optional and all present keys are ANDed.
//
// It is closed because the whole point of levels is that a call site never
// names a model: an open key set would let a rule branch on something the
// router does not carry, and the failure would be a rule that silently never
// matches.
var RuleWhenKeys = []string{"level", "modality", "prompt", "role", "actorRole", "tag", "touches"}

// RuleWhenKeyNames is the closed set comma-joined, for an error message.
func RuleWhenKeyNames() string { return strings.Join(RuleWhenKeys, ", ") }

// IsRuleWhenKey reports whether k is one of the closed set.
func IsRuleWhenKey(k string) bool {
	for _, known := range RuleWhenKeys {
		if known == k {
			return true
		}
	}
	return false
}

// RuleWhen is a rule's condition set.
//
// Present records which keys the author actually WROTE, which is not the same
// question as which are non-empty: `@when(prompt="")` is a condition that
// matches only a call with no prompt name, while an absent `prompt` key is no
// condition at all. Collapsing the two would make every rule with an empty
// value match everything.
type RuleWhen struct {
	Level     string
	Modality  string
	Prompt    string
	Role      string
	ActorRole string
	Tag       string
	Touches   string

	Present map[string]bool
}

// Has reports whether the author wrote key.
func (w RuleWhen) Has(key string) bool { return w.Present[key] }

// IsEmpty reports whether the rule states no conditions at all, which is what
// makes the shipped `default` rule the floor.
func (w RuleWhen) IsEmpty() bool { return len(w.Present) == 0 }

// RuleConfig is one loaded rule.
type RuleConfig struct {
	Name        string
	Description string

	// When is the condition set; empty means "matches every call".
	When RuleWhen

	// Policy is the policy this rule names. Required at load.
	Policy string

	// Level OVERRIDES the level the call declared. Empty means the call's own
	// level stands.
	Level airoute.Level

	// Precedence orders rules, highest first. A tie between two rules of the
	// same locked-ness is a load error.
	Precedence int

	// OnUnavailable is OnUnavailableDegrade or OnUnavailablePark. Empty reads
	// as degrade.
	OnUnavailable string

	// Excludes removes concrete models from the chain's resolution -- the
	// demotion vehicle of epic 4. Each entry is a policy-entry reference.
	Excludes []string

	// Locked marks a rule from the embedded tree that evaluates before every
	// unlocked rule regardless of precedence. It is accepted only in the
	// embedded tree; the loader, not the parser, enforces that, because the
	// parser does not know which tree it is reading.
	Locked bool

	// Disabled rules are loaded and not evaluated, the ordinary @disabled
	// lifecycle: reversible, still maintained, still conformance-checked.
	Disabled bool

	// SourceFile is where the rule was declared. Carried so a duplicate can
	// name both files rather than only the name that collided.
	SourceFile string
}

// Parks reports whether this rule refuses rather than degrading when the chain
// is exhausted. An unset OnUnavailable degrades.
func (r *RuleConfig) Parks() bool {
	return r != nil && r.OnUnavailable == OnUnavailablePark
}

// EffectiveLevel is the level this rule resolves a call at: its own override
// when it declares one, else what the call asked for.
func (r *RuleConfig) EffectiveLevel(requested airoute.Level) airoute.Level {
	if r == nil || r.Level == "" {
		return requested
	}
	return r.Level
}

// DefaultRuleName is the shipped rule that matches every call. It is the floor
// the evaluation order rests on: a call that matches no rule is impossible by
// construction rather than by care, so nothing has to handle that case.
const DefaultRuleName = "default"

// RuleRegistry holds the loaded rules and their evaluation order.
type RuleRegistry struct {
	mu      sync.RWMutex
	byName  map[string]*RuleConfig
	ordered []*RuleConfig
}

// NewRuleRegistry returns an empty registry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{byName: map[string]*RuleConfig{}}
}

// Register adds a rule, REFUSING a duplicate name rather than overwriting it.
//
// Overwriting was the pre-existing policy-registry behaviour and it is what
// let a mounted bundle silently replace a shipped construct: last writer wins,
// no message, and the cluster routes by a rule nobody can find. The error
// names BOTH files, because the name alone does not say which copy won.
func (r *RuleRegistry) Register(cfg *RuleConfig) error {
	if r == nil {
		return fmt.Errorf("rule registry is nil")
	}
	if cfg == nil || strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("rule: name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byName[cfg.Name]; ok {
		return fmt.Errorf("rule %q is declared twice: %s and %s -- a rule name is unique across the whole corpus, "+
			"because a duplicate resolved by load order is a routing decision nobody wrote and nobody can reproduce",
			cfg.Name, existing.SourceFile, cfg.SourceFile)
	}
	r.byName[cfg.Name] = cfg
	r.ordered = nil
	return nil
}

// Lookup returns a rule by name.
func (r *RuleRegistry) Lookup(name string) (*RuleConfig, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.byName[strings.TrimSpace(name)]
	return cfg, ok
}

// Count reports how many rules are registered, disabled ones included.
func (r *RuleRegistry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byName)
}

// All returns every registered rule, disabled ones included, sorted by name.
// For evaluation use Ordered.
func (r *RuleRegistry) All() []*RuleConfig {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*RuleConfig, 0, len(r.byName))
	for _, cfg := range r.byName {
		out = append(out, cfg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Finalize computes the evaluation order and refuses a precedence tie.
//
// It is called once, after every rule is registered. Computing the order at
// LOAD rather than per call is what makes two replicas agree with no shared
// state: the walk is over a slice fixed at boot, not a map iterated live.
func (r *RuleRegistry) Finalize() error {
	if r == nil {
		return fmt.Errorf("rule registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	enabled := make([]*RuleConfig, 0, len(r.byName))
	for _, cfg := range r.byName {
		if cfg.Disabled {
			continue
		}
		enabled = append(enabled, cfg)
	}
	// Sort by name first so the tie check below reports a stable pair, and so
	// the final order is deterministic where every other key agrees.
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].Name < enabled[j].Name })

	// LOCKED FIRST, REGARDLESS OF PRECEDENCE. That is what locked MEANS
	// operationally: an owner may add rules and give them any precedence they
	// like, and the shipped ones still evaluate first. Precedence orders
	// within each half.
	//
	// `default` IS EXEMPT FROM THAT PARTITION AND ALWAYS SORTS LAST, and the
	// exemption is not a tidiness choice -- without it the whole authored tier
	// is unreachable. `default` is locked and states NO conditions, so it
	// matches every call; ordered with the locked group it would win before
	// any unlocked rule was ever consulted, and design D7's "a runtime-authored
	// rule may add and may take precedence" would be false of every rule
	// anybody wrote. It is the FLOOR, not a locked rule that outranks yours,
	// and those are different things wearing one annotation.
	//
	// It keeps @locked for the OTHER two things the annotation means: it is
	// re-read from the embedded tree on every boot, and no runtime-authored
	// rule may take its name.
	isFloor := func(c *RuleConfig) bool { return c.Name == DefaultRuleName }
	sort.SliceStable(enabled, func(i, j int) bool {
		a, b := enabled[i], enabled[j]
		if isFloor(a) != isFloor(b) {
			return isFloor(b)
		}
		if a.Locked != b.Locked {
			return a.Locked
		}
		return a.Precedence > b.Precedence
	})

	// A tie between two rules of the same locked-ness is refused, naming both.
	// Resolving it by map order would be a routing decision nobody wrote. The
	// floor is exempt: it is alone at the end and ties with nothing.
	for i := 1; i < len(enabled); i++ {
		a, b := enabled[i-1], enabled[i]
		if isFloor(a) || isFloor(b) {
			continue
		}
		if a.Locked == b.Locked && a.Precedence == b.Precedence {
			return fmt.Errorf("rules %q (%s) and %q (%s) both declare @precedence(%d): "+
				"a tie is resolved by nothing, so the rule that wins would differ between replicas -- "+
				"give one of them a different precedence",
				a.Name, a.SourceFile, b.Name, b.SourceFile, a.Precedence)
		}
	}

	if _, ok := r.byName[DefaultRuleName]; !ok && len(r.byName) > 0 {
		return fmt.Errorf("no rule named %q is registered: it is the floor every call falls to, "+
			"and without it a call that matches nothing has no policy at all", DefaultRuleName)
	}

	r.ordered = enabled
	return nil
}

// Ordered returns every enabled rule in EVALUATION order: locked rules first
// in their own precedence order, then unlocked, precedence descending.
//
// It returns nil before Finalize, which is deliberate -- an unfinalized
// registry has no order, and answering with an arbitrary one would route calls
// by map iteration.
func (r *RuleRegistry) Ordered() []*RuleConfig {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ordered
}
