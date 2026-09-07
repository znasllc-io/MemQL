package parser

import (
	"fmt"
	"strings"
)

// policy_entry.go holds the CLOSED grammar for one entry of a policy chain --
// the strings that appear in `@primary(...)`, `@fallback(...)` and a rule's
// `@exclude(...)`.
//
// It lives in the parser rather than beside the registries because both the
// policy loader and the rule loader hold their entries to it, and a second copy
// of a grammar is a copy that drifts. The whole point of the grammar is that a
// call site never names a model: an entry says which DOOR to try and how to
// pick behind it, so an entry the grammar does not recognise is a routing
// instruction the router will silently do nothing with.
//
// The accepted forms, and nothing else:
//
//	<providerName>              a registry entry, by name
//	fleet:strongest             the strongest model on the person's own machines
//	fleet:fastest               the fastest one
//	fleet:<modelId>             one named local model
//	app:*                       any signed-in local app that can run the call
//	app:<id>                    one named local app
//	federation:cheapest         the cheapest qualifying federated record
//	federation:strongest        the strongest one
//	federation:<providerName>   one named federated provider
//	policy:<name>               another policy, expanded at load

// Entry schemes. The set is closed: a scheme outside it is refused rather than
// passed through, because an unrecognised scheme reads as a provider name that
// happens to contain a colon and fails much later, at resolution, as "no such
// provider".
const (
	// EntrySchemeFleet reaches the person's own machines.
	EntrySchemeFleet = "fleet"
	// EntrySchemeApp reaches a signed-in local app (Claude Code, Codex).
	EntrySchemeApp = "app"
	// EntrySchemeFederation reaches a paid vendor through workload identity
	// federation.
	EntrySchemeFederation = "federation"
	// EntrySchemePolicy names another policy, expanded at load so the router
	// never walks one.
	EntrySchemePolicy = "policy"
)

// entrySchemes is the closed set, in the order an error message lists them:
// the three doors in cost order, then the composition form.
var entrySchemes = []string{EntrySchemeFleet, EntrySchemeApp, EntrySchemeFederation, EntrySchemePolicy}

// fleetSelectors and federationSelectors are the reserved words behind their
// scheme. Anything else after the colon is a concrete id -- which is why a
// misspelled selector cannot be caught by "is it in this set": it is
// indistinguishable from a model nobody has installed. What IS catchable, and
// is caught below, is a selector word carrying extra colon-separated junk.
var (
	fleetSelectors      = map[string]bool{"strongest": true, "fastest": true}
	federationSelectors = map[string]bool{"cheapest": true, "strongest": true}
)

// ValidatePolicyEntry reports whether entry is one of the accepted forms.
//
// It is called from the policy loader (over every `@primary` / `@fallback`) and
// from the rule parser (over every `@exclude`), so a bundle mounted at
// MEMQL_DSL_PATH is held to the same grammar as the embedded tree.
func ValidatePolicyEntry(entry string) error {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return fmt.Errorf("policy entry is empty: an entry is a provider name, or one of %s",
			strings.Join(entryFormsForMessage(), ", "))
	}

	// fleet:* is REFUSED BY NAME, before the generic path, so its message can
	// carry the replacement. An author who wrote fleet:* meant "the best local
	// model"; the spelling for that is fleet:strongest, and a refusal that says
	// only "invalid" sends them to the source to work out what changed.
	if trimmed == EntrySchemeFleet+":*" {
		return fmt.Errorf("policy entry %q is retired: write fleet:strongest for the best local model, "+
			"or fleet:fastest for the quickest one -- the wildcard could not say which of a person's "+
			"machines it meant, so the choice was made by whichever registered first", trimmed)
	}

	scheme, rest, hasColon := strings.Cut(trimmed, ":")
	if !hasColon {
		// A bare name is a provider registry entry.
		if err := validateEntryIdentifier(trimmed, "provider name"); err != nil {
			return fmt.Errorf("policy entry %q: %w", trimmed, err)
		}
		return nil
	}

	switch scheme {
	case EntrySchemeFleet:
		if rest == "" {
			return fmt.Errorf("policy entry %q names no local model: write fleet:strongest, fleet:fastest, "+
				"or fleet:<modelId>", trimmed)
		}
		// A MODEL ID MAY ITSELF CONTAIN A COLON (`qwen3.8:27b`), which is why
		// the split above takes the FIRST colon only. That makes one mistake
		// invisible: `fleet:strongest:extra` would read as a model literally
		// named "strongest:extra" and fail at resolution as "no such model".
		// A selector word is reserved, so a colon after one is a malformed
		// selector rather than an id.
		if head, tail, more := strings.Cut(rest, ":"); more && fleetSelectors[head] {
			return fmt.Errorf("policy entry %q: %q is a selector, not a model id, so nothing may follow it "+
				"(the trailing %q looks like a typo)", trimmed, head, tail)
		}
		if fleetSelectors[rest] {
			return nil
		}
		return validateEntryId(trimmed, rest, "model id")

	case EntrySchemeApp:
		if rest == "" {
			return fmt.Errorf("policy entry %q names no app: write app:* for any signed-in app, "+
				"or app:<id> for one of them", trimmed)
		}
		if rest == "*" {
			return nil
		}
		return validateEntryId(trimmed, rest, "app id")

	case EntrySchemeFederation:
		if rest == "" {
			return fmt.Errorf("policy entry %q names no federated provider: write federation:cheapest, "+
				"federation:strongest, or federation:<providerName>", trimmed)
		}
		if federationSelectors[rest] {
			return nil
		}
		if err := validateEntryIdentifier(rest, "provider name"); err != nil {
			return fmt.Errorf("policy entry %q: %w", trimmed, err)
		}
		return nil

	case EntrySchemePolicy:
		if rest == "" {
			return fmt.Errorf("policy entry %q names no policy: write policy:<name>", trimmed)
		}
		if err := validateEntryIdentifier(rest, "policy name"); err != nil {
			return fmt.Errorf("policy entry %q: %w", trimmed, err)
		}
		return nil
	}

	return fmt.Errorf("policy entry %q: unknown scheme %q -- an entry is a bare provider name, or one of %s",
		trimmed, scheme, strings.Join(entryFormsForMessage(), ", "))
}

// IsSelectorEntry decomposes a DOOR entry into its scheme and what follows the
// colon, reporting ok only for fleet / app / federation.
//
// A `policy:` entry is deliberately NOT a selector -- it is expanded away at
// load and the router never walks one -- so it answers through IsPolicyEntry
// instead. The two are disjoint on purpose: a caller that treated `policy:x` as
// a door would try to resolve a provider named "x".
//
// It reports the SHAPE and does not validate: run ValidatePolicyEntry for that.
func IsSelectorEntry(entry string) (scheme, selector string, ok bool) {
	scheme, selector, hasColon := strings.Cut(strings.TrimSpace(entry), ":")
	if !hasColon {
		return "", "", false
	}
	switch scheme {
	case EntrySchemeFleet, EntrySchemeApp, EntrySchemeFederation:
		return scheme, selector, true
	}
	return "", "", false
}

// IsPolicyEntry reports whether entry names another policy, and which.
func IsPolicyEntry(entry string) (name string, ok bool) {
	scheme, rest, hasColon := strings.Cut(strings.TrimSpace(entry), ":")
	if !hasColon || scheme != EntrySchemePolicy || rest == "" {
		return "", false
	}
	return rest, true
}

// entryFormsForMessage is the accepted forms as an error message lists them.
// Built from the scheme constants so a scheme cannot be added without the
// message learning it.
func entryFormsForMessage() []string {
	out := make([]string, 0, len(entrySchemes))
	for _, scheme := range entrySchemes {
		switch scheme {
		case EntrySchemeFleet:
			out = append(out, "fleet:strongest, fleet:fastest, fleet:<modelId>")
		case EntrySchemeApp:
			out = append(out, "app:*, app:<id>")
		case EntrySchemeFederation:
			out = append(out, "federation:cheapest, federation:strongest, federation:<providerName>")
		case EntrySchemePolicy:
			out = append(out, "policy:<name>")
		}
	}
	return out
}

// validateEntryIdentifier holds a NAME to the DSL's identifier shape: it starts
// with a letter and continues in letters and digits.
//
// Provider names, policy names and federated provider names are all registry
// keys authored in this DSL, so they are held to the identifier shape. Model
// ids and app ids are NOT -- those are somebody else's names and carry dots,
// dashes and colons -- which is what validateEntryId is for.
func validateEntryIdentifier(name, what string) error {
	if name == "" {
		return fmt.Errorf("%s is empty", what)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return fmt.Errorf("%s %q starts with a digit -- a %s is an identifier", what, name, what)
			}
		default:
			return fmt.Errorf("%s %q contains %q -- a %s is letters and digits, starting with a letter", what, name, string(r), what)
		}
	}
	return nil
}

// validateEntryId holds an EXTERNAL id -- a model id, an app id -- to the one
// thing that is genuinely an error rather than a naming convention somebody
// else chose: it must be non-empty and carry no whitespace.
//
// Deliberately permissive. `qwen3.8:27b` and `claude-code` are both real, and a
// tighter rule here would refuse a model that exists on the operator's machine
// in the name of a house style that is not ours to impose.
func validateEntryId(entry, id, what string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("policy entry %q names an empty %s", entry, what)
	}
	if strings.ContainsAny(id, " \t\n\r") {
		return fmt.Errorf("policy entry %q: the %s %q contains whitespace", entry, what, id)
	}
	return nil
}
