package routingrules

// generate.go -- turning a routing rule's FORM into a real `rule` construct
// (epic memql#5127, design D7).
//
// # Why a generated construct at all
//
// A rule is evaluated from the registry the loader fills, and the loader reads
// `.memql` text. So the only way an owner adds a rule at runtime is for
// something to author one -- and the runtime authoring pipeline (bundle ->
// validate -> activate -> AuthoredRuntimeRegistry) is the tree's existing
// machinery for exactly that. Using it brings the whole governance apparatus
// along: the sandbox's Gate 1, the core-first resolver that refuses a shipped
// name, boot re-arm, and an author-scoped actor rather than a system one.
//
// # Why no model is involved
//
// The natural-language path -- a person describing a rule in a sentence -- is
// epic 3's. This is the deterministic path underneath it, and it is what the
// OS's "edit as fields" panel calls. The distinction matters because the two
// have different failure modes: a described rule can be compiled into the wrong
// rule, which is why epic 3 shows the compiled result for confirmation, while
// this one can only be the rule the form says.
//
// # Why the output is short
//
// A rule construct is annotations and an empty body. There is no room here for
// a generator that is a program writing a program: every line below is one
// annotation, rendered from one field, and the whole output is short enough
// that a person can read it and see what it does. That is the property the
// "no LLM" decision was protecting, and it is cheaper to keep here than to
// re-establish later.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	langparser "github.com/znasllc-io/memql/component/language/parser"
	"github.com/znasllc-io/memql/component/memql"
)

// Form is what the OS sends and what the builtin takes. It is the rule as a
// person filled it in, and deliberately NOT the generated half -- no bundle id,
// no construct name, no last error. A generator that read its own past output
// would produce a construct whose content depended on it.
type Form struct {
	// Name is the rule's construct name. It must not be a shipped name; the
	// authoring pipeline's core-first resolver refuses that, and Validate
	// refuses it earlier so the operator hears it from the form.
	Name string
	// Description becomes the /// doc comment, which is what a reader of the
	// rules list sees.
	Description string

	// When is the condition set, keyed by the closed @when keys. A key present
	// with an empty value is a condition matching only an empty value; a key
	// ABSENT is no condition at all. The map preserves that distinction, which
	// a struct of strings could not.
	When map[string]string

	// Policy is required.
	Policy string
	// Level optionally overrides the call's declared level.
	Level string
	// Precedence orders the rule. A tie with another unlocked rule refuses at
	// load, so the form carries the number rather than inventing one.
	Precedence int
	// OnUnavailable is "degrade" or "park". Empty renders nothing and reads as
	// degrade, matching the DSL's own default.
	OnUnavailable string
	// Excludes removes concrete models from this rule's resolution.
	Excludes []string
}

// ConstructNameFor is the name the generated construct carries. It is the
// form's own name: a rule is addressed by name in the registry, in the
// decision record and on the operator's screen, and a derived name would make
// those three disagree.
func ConstructNameFor(f Form) string { return strings.TrimSpace(f.Name) }

// Validate refuses a form the DSL would refuse, in the operator's vocabulary
// and before anything is written.
//
// It duplicates checks the parser also makes, and that duplication is the
// point: the parser's message is about a construct the operator never wrote,
// at a line number in a file that does not exist. This one is about the field
// they filled in.
func Validate(f Form, shipped ShippedNames) error {
	name := strings.TrimSpace(f.Name)
	if name == "" {
		return fmt.Errorf("a rule needs a name")
	}
	if !isIdentifier(name) {
		return fmt.Errorf("rule name %q must be a bare identifier: letters and digits, starting with a letter", name)
	}
	if shipped.HasRule(name) {
		return fmt.Errorf("%q is a shipped rule. A shipped rule is re-read from the embedded tree on every "+
			"boot, so a custom rule taking its name would be replaced at the next restart with nothing "+
			"saying so. Choose another name; a custom rule can still take precedence with @precedence", name)
	}
	if strings.TrimSpace(f.Policy) == "" {
		return fmt.Errorf("rule %q names no policy: a rule that selects no chain resolves nothing", name)
	}
	if err := langparser.ValidatePolicyEntry(f.Policy); err != nil {
		return fmt.Errorf("rule %q: %w", name, err)
	}
	if _, isPolicyRef := langparser.IsPolicyEntry(f.Policy); isPolicyRef {
		return fmt.Errorf("rule %q: @policy takes a policy NAME, not a policy: reference -- write %q",
			name, strings.TrimPrefix(f.Policy, "policy:"))
	}
	if lvl := strings.TrimSpace(f.Level); lvl != "" && !validLevel(lvl) {
		return fmt.Errorf("rule %q: level %q is not one of fast, strong, reasoning, embeddings", name, lvl)
	}
	switch strings.TrimSpace(f.OnUnavailable) {
	case "", memql.OnUnavailableDegrade, memql.OnUnavailablePark:
	default:
		return fmt.Errorf("rule %q: onUnavailable %q is not %q or %q",
			name, f.OnUnavailable, memql.OnUnavailableDegrade, memql.OnUnavailablePark)
	}
	for key, value := range f.When {
		if !memql.IsRuleWhenKey(key) {
			return fmt.Errorf("rule %q: %q is not a condition key -- the closed set is %s",
				name, key, memql.RuleWhenKeyNames())
		}
		if strings.ContainsAny(value, `"\`+"\n") {
			return fmt.Errorf("rule %q: the value for %q contains a quote, a backslash or a newline, "+
				"which a rendered annotation cannot carry", name, key)
		}
	}
	if lvl, ok := f.When["level"]; ok && lvl != "" && !validLevel(lvl) {
		return fmt.Errorf("rule %q: @when(level=%q) is not one of fast, strong, reasoning, embeddings -- "+
			"a level outside the closed set is a condition that can never be true, which presents as a "+
			"rule that is simply never reached", name, lvl)
	}
	for _, entry := range f.Excludes {
		if err := langparser.ValidatePolicyEntry(entry); err != nil {
			return fmt.Errorf("rule %q: exclude %q: %w", name, entry, err)
		}
	}
	return nil
}

// GenerateRule renders the construct. It assumes Validate passed; the one
// thing it re-checks is the name, because rendering an empty one produces text
// the parser refuses with a message about a missing identifier.
func GenerateRule(f Form) (string, error) {
	name := ConstructNameFor(f)
	if name == "" {
		return "", fmt.Errorf("a rule needs a name")
	}
	var b strings.Builder
	if d := strings.TrimSpace(f.Description); d != "" {
		for _, line := range wrapDoc(d) {
			b.WriteString("/// " + line + "\n")
		}
	}
	b.WriteString("@when(" + renderWhen(f.When) + ")\n")
	if lvl := strings.TrimSpace(f.Level); lvl != "" {
		b.WriteString("@level(" + strconv.Quote(lvl) + ")\n")
	}
	b.WriteString("@policy(" + strconv.Quote(strings.TrimSpace(f.Policy)) + ")\n")
	b.WriteString("@precedence(" + strconv.Itoa(f.Precedence) + ")\n")
	if u := strings.TrimSpace(f.OnUnavailable); u != "" {
		b.WriteString("@onUnavailable(" + strconv.Quote(u) + ")\n")
	}
	for _, entry := range f.Excludes {
		if e := strings.TrimSpace(entry); e != "" {
			b.WriteString("@exclude(" + strconv.Quote(e) + ")\n")
		}
	}
	// NO @locked, ever. It is the one annotation runtime authoring does not
	// grant: a rule carrying it would evaluate before every shipped rule, and
	// the authority to do that is what the embedded tree has and an owner's
	// form does not. The absence here is enforcement, not omission -- the
	// sandbox refuses one that arrives anyway.
	b.WriteString("rule " + name + " { }\n")
	return b.String(), nil
}

// renderWhen emits the condition list in a STABLE order, so regenerating an
// unchanged form produces byte-identical source. Map iteration order would
// make every regeneration look like an edit to whatever reads the construct's
// source hash.
func renderWhen(when map[string]string) string {
	keys := make([]string, 0, len(when))
	for k := range when {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(when[k]))
	}
	return strings.Join(parts, ", ")
}

// wrapDoc breaks a description into doc-comment lines. It wraps on spaces at a
// conservative width rather than reflowing, because a description a person
// typed is theirs and the only thing being changed is where the lines end.
func wrapDoc(s string) []string {
	const width = 76
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var out []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			out = append(out, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(out, line)
}

func validLevel(s string) bool {
	switch s {
	case "fast", "strong", "reasoning", "embeddings":
		return true
	}
	return false
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return !(s[0] >= '0' && s[0] <= '9')
}

// ShippedNames answers "is this name the embedded tree's". It is an interface
// so this package can be tested without an engine, and so the one live
// implementation is the engine's own registries rather than a second list that
// would drift from them.
type ShippedNames interface {
	HasRule(name string) bool
	HasPolicy(name string) bool
}

// EngineShippedNames reads the live registries.
type EngineShippedNames struct{ Engine *memql.MemQLEngine }

func (e EngineShippedNames) HasRule(name string) bool {
	if e.Engine == nil || e.Engine.Rules() == nil {
		return false
	}
	_, ok := e.Engine.Rules().Lookup(name)
	return ok
}

func (e EngineShippedNames) HasPolicy(name string) bool {
	if e.Engine == nil || e.Engine.Policies() == nil {
		return false
	}
	_, ok := e.Engine.Policies().Lookup(name)
	return ok
}
