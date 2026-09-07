package memql

// rule_converter.go translates the langparser-produced AST node into the
// registry value the router reads. It is where a declaration stops being text
// and becomes a decision the engine will act on, so it is where the values a
// rule may carry are checked -- the parser checked the SHAPE.

import (
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/language/ast"
	languageParser "github.com/znasllc-io/memql/component/language/parser"
	"github.com/znasllc-io/memql/core/airoute"
)

// ruleDeclToRuleConfig converts one parsed rule, refusing every value the
// router could not act on. sourceFile is carried onto the config so a
// duplicate or a precedence tie can name both files rather than only the name
// that collided.
func ruleDeclToRuleConfig(decl *ast.RuleDecl, sourceFile string) (*RuleConfig, error) {
	if decl == nil {
		return nil, fmt.Errorf("rule declaration is nil")
	}
	name := strings.TrimSpace(decl.Name)
	if name == "" {
		return nil, fmt.Errorf("rule: name is required")
	}
	policy := strings.TrimSpace(decl.Policy)
	if policy == "" {
		return nil, fmt.Errorf("rule %q: @policy is required -- a rule that names no policy selects no chain, "+
			"so every call it matches would resolve nothing", name)
	}

	cfg := &RuleConfig{
		Name:        name,
		Description: languageParser.EffectiveDescription(decl.DocComment, decl.Description),
		When: RuleWhen{
			Level:     decl.When.Level,
			Modality:  decl.When.Modality,
			Prompt:    decl.When.Prompt,
			Role:      decl.When.Role,
			ActorRole: decl.When.ActorRole,
			Tag:       decl.When.Tag,
			Touches:   decl.When.Touches,
			Present:   copyPresent(decl.When.Present),
		},
		Policy:        policy,
		Precedence:    decl.Precedence,
		OnUnavailable: strings.TrimSpace(decl.OnUnavailable),
		Excludes:      append([]string(nil), decl.Excludes...),
		Locked:        decl.Locked,
		Disabled:      decl.Disabled,
		SourceFile:    sourceFile,
	}

	if raw := strings.TrimSpace(decl.Level); raw != "" {
		level, err := airoute.ParseLevel(raw)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", name, err)
		}
		cfg.Level = level
	}

	// A @when(level=...) condition is compared against a request's level, so a
	// value outside the closed four is a condition that can never be true --
	// which reads at runtime as a rule that was simply never reached.
	if raw := strings.TrimSpace(cfg.When.Level); cfg.When.Has("level") && raw != "" {
		if _, err := airoute.ParseLevel(raw); err != nil {
			return nil, fmt.Errorf("rule %q: @when(level=%q): %w -- a level outside the closed set is a "+
				"condition that can never be true, which presents as a rule that is simply never reached",
				name, raw, err)
		}
	}
	if raw := strings.TrimSpace(cfg.When.Modality); cfg.When.Has("modality") && raw != "" {
		if !airoute.Modality(raw).Valid() {
			return nil, fmt.Errorf("rule %q: @when(modality=%q) is not a modality the router derives -- "+
				"a modality outside the closed set is a condition that can never be true", name, raw)
		}
	}

	switch cfg.OnUnavailable {
	case "", OnUnavailableDegrade, OnUnavailablePark:
	default:
		return nil, fmt.Errorf("rule %q: @onUnavailable(%q) is not %q or %q",
			name, cfg.OnUnavailable, OnUnavailableDegrade, OnUnavailablePark)
	}
	if cfg.OnUnavailable == "" {
		cfg.OnUnavailable = OnUnavailableDegrade
	}

	for _, entry := range cfg.Excludes {
		if err := languageParser.ValidatePolicyEntry(entry); err != nil {
			return nil, fmt.Errorf("rule %q: @exclude(%q): %w", name, entry, err)
		}
	}
	return cfg, nil
}

// copyPresent defensively copies the parser's map. The registry outlives the
// AST, and a shared map is a way for a later parse to change a rule that was
// already registered.
func copyPresent(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
