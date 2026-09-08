package memql

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"

	"github.com/znasllc-io/memql/component/metrics"
)

// The engine half of `memql provider-auth check` (memql#4335).
//
// WHY A SUBCOMMAND AND NOT A HEALTH ENDPOINT. The question this answers --
// "is this pod authenticating to Anthropic the way I think it is, right now"
// -- is asked exactly twice in the life of a cluster: at the moment of the
// cutover, and the moment something looks wrong afterwards. Both times the
// operator is already at a shell (`kubectl exec`), and both times the answer
// must come from INSIDE a pod, because the whole premise of federation is
// that the credential is the pod's own projected token and exists nowhere
// else. A health endpoint would have to be reachable, authorized and shaped,
// and would still be answering from the same place.
//
// It forces a REAL exchange and a REAL API call on purpose. Reading config
// back proves the config parses; the failure modes that matter here -- a rule
// whose subject prefix does not match, an audience typo, a service account
// removed in the Console -- are all invisible until Anthropic answers.

// ProviderAuthReport is what `provider-auth check` prints. Every field is
// safe to show an operator: ids name objects, never credentials. The bearer
// the exchange returns appears in no field.
type ProviderAuthReport struct {
	// Provider is the DSL provider entry the check ran against.
	Provider string
	Type     string
	Model    string

	// CredentialPath is "federation" or "api-key" -- the question the
	// cutover asks.
	// Vendor is "anthropic" or "openai". With two federating vendors, a report
	// that does not say which one it describes is a report an operator can
	// read as an answer about the other.
	Vendor string

	CredentialPath string

	// The federation ids, empty on the api-key path.
	// IdentityProviderID is OpenAI's half of the id pair; FederationRuleID,
	// OrganizationID and WorkspaceID are Anthropic's. Each vendor fills only
	// its own, so an empty field means "not this vendor" rather than "unset".
	IdentityProviderID string
	FederationRuleID   string
	OrganizationID     string
	ServiceAccountID   string
	WorkspaceID        string
	IdentityTokenFile  string

	// TokenSubject / TokenAudience come from the projected token on disk,
	// not from Anthropic: they are what the federation rule matches on, and
	// a mismatch here is the single most common denial.
	TokenSubject  string
	TokenAudience []string

	// The observed exchange. Zero on the api-key path (there is none).
	ExchangeOutcome string
	TokenExpiresAt  time.Time
	TokenExpiresIn  time.Duration

	// ModelsListed is the count returned by the live models.list call -- the
	// proof that the credential is not merely well-formed but accepted.
	ModelsListed int
}

// anthropicProviderTypes are the DSL provider types this check understands.
var anthropicProviderTypes = map[string]bool{
	"anthropic":       true,
	"anthropicchat":   true,
	"anthropicstream": true,
}

// isOpenAIProviderType matches by PREFIX rather than by an exhaustive set,
// unlike the Anthropic map above.
//
// That asymmetry is deliberate. There are three Anthropic provider types and
// ten OpenAI ones (OpenAI, OpenAIStream, OpenAITTS, OpenAISTT, OpenAIWhisper,
// OpenAIRealtime, OpenAIAudio, OpenAIImage, OpenAIDeepResearch, ...), several
// of which are placeholders that will grow. An exhaustive list would go stale
// the way every list in this repo that is not generated goes stale, and its
// failure mode is `provider-auth check --provider openai` refusing to check a
// provider that federates perfectly well.
func isOpenAIProviderType(providerType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(providerType)), "openai")
}

// CheckProviderAuth builds the provider the way boot does, forces one
// credential exchange, and calls models.list.
//
// providerName selects one DSL provider entry by name; empty picks the first
// available Anthropic entry in name order (deterministic, so two runs on two
// pods compare).
//
// ONE HONEST LIMITATION, stated here and in the command's output: this runs
// without the engine, so the two concept-storage tiers of auth resolution
// (v1:platform:globalSecret, globalVariable) are not consulted -- only the
// process environment, which is where a pod's credential config actually
// lives. An operator who seeded the key into concept storage instead will see
// this report say the key is absent while the running node has it. It is the
// same trade `memql env` makes, for the same reason: a check that needed a
// database would not run in the situations it exists for.
func CheckProviderAuth(ctx context.Context, logger *slog.Logger, providerName string) (ProviderAuthReport, error) {
	registry := newProviderRegistry()
	if _, err := LoadUnifiedProviders(logger, registry); err != nil {
		return ProviderAuthReport{}, fmt.Errorf("load providers: %w", err)
	}

	entry, err := selectVendorEntry(registry, providerName)
	if err != nil {
		return ProviderAuthReport{}, err
	}

	vendor := metrics.FederationVendorAnthropic
	if isOpenAIProviderType(entry.Config.Type) {
		vendor = metrics.FederationVendorOpenAI
	}

	report := ProviderAuthReport{
		Provider: entry.Config.Name,
		Type:     entry.Config.Type,
		Model:    entry.Config.Model,
		Vendor:   vendor,
	}

	// Rebuild the credential from the resolved auth map rather than reading
	// the registered client's, so the report describes the same decision the
	// constructor made and names it in the constructor's own words when it
	// refuses.
	var path credentialPath
	if vendor == metrics.FederationVendorOpenAI {
		fed := openaiFederationFrom(entry.Config.Auth)
		_, _, p, cerr := openaiCredential(entry.Config, guardedHTTPClient(nil))
		if cerr != nil {
			return report, cerr
		}
		path = p
		if path == credentialPathFederation {
			report.IdentityProviderID = fed.IdentityProviderID
			report.ServiceAccountID = fed.ServiceAccountID
			report.IdentityTokenFile = fed.TokenFile
			if claims, terr := readIdentityTokenClaims(fed.TokenFile); terr == nil {
				report.TokenSubject, _ = claims["sub"].(string)
				report.TokenAudience = jwtAudienceValues(claims)
			}
		}
	} else {
		fed := anthropicFederationFrom(entry.Config.Auth)
		_, p, cerr := anthropicCredential(entry.Config, guardedHTTPClient(nil))
		if cerr != nil {
			return report, cerr
		}
		path = p
		if path == credentialPathFederation {
			report.FederationRuleID = fed.RuleID
			report.OrganizationID = fed.OrganizationID
			report.ServiceAccountID = fed.ServiceAccountID
			report.WorkspaceID = fed.WorkspaceID
			report.IdentityTokenFile = fed.TokenFile
			if claims, terr := readIdentityTokenClaims(fed.TokenFile); terr == nil {
				report.TokenSubject, _ = claims["sub"].(string)
				report.TokenAudience = jwtAudienceValues(claims)
			}
		}
	}
	report.CredentialPath = string(path)

	// UNAVAILABLE IS AN ANSWER, NOT A FAILURE. A cluster with no federation
	// ids for this vendor is a fresh cloud cluster or any local one, and the
	// command's job there is to say so plainly rather than to error out on a
	// provider that is behaving exactly as designed. Returning here also
	// avoids reporting a models.list failure whose real cause is "no
	// credential", which reads as an outage.
	if path == credentialPathUnavailable {
		return report, nil
	}

	if !entry.Available || entry.Client == nil {
		if entry.err != nil {
			return report, fmt.Errorf("provider %q did not construct: %w", entry.Config.Name, entry.err)
		}
		return report, fmt.Errorf("provider %q is registered but unavailable", entry.Config.Name)
	}

	// The live call. models.list is the cheapest authenticated request either
	// vendor serves -- it spends no tokens, so a check that runs on every
	// deploy costs nothing but a round trip.
	listed, err := listModelsForCheck(ctx, entry, vendor)
	if err != nil {
		if rec := LastFederationExchange(); rec != nil && rec.Vendor == vendor {
			report.ExchangeOutcome = rec.Outcome
		}
		return report, fmt.Errorf("models.list failed on the %s credential path: %w", report.CredentialPath, err)
	}
	report.ModelsListed = listed

	// The exchange record is per-process and holds the LAST exchange of any
	// vendor, so it is only this report's evidence when the vendors match.
	// Reading it unconditionally would attribute Anthropic's expiry to an
	// OpenAI check on any node that had done both.
	if rec := LastFederationExchange(); rec != nil && rec.Vendor == vendor && path == credentialPathFederation {
		report.ExchangeOutcome = rec.Outcome
		report.TokenExpiresAt = rec.ExpiresAt
		report.TokenExpiresIn = rec.ExpiresIn
	}
	return report, nil
}

// listModelsForCheck makes the one authenticated call, per vendor.
func listModelsForCheck(ctx context.Context, entry *ProviderConfigEntry, vendor string) (int, error) {
	if vendor == metrics.FederationVendorOpenAI {
		client, ok := openAIClientOf(entry.Client)
		if !ok {
			return 0, fmt.Errorf("provider %q carries no OpenAI client to check", entry.Config.Name)
		}
		page, err := client.Models.List(ctx)
		if err != nil {
			return 0, err
		}
		if page == nil {
			return 0, nil
		}
		return len(page.Data), nil
	}

	client, err := anthropicClientOf(entry.Client)
	if err != nil {
		return 0, err
	}
	page, err := client.Models.List(ctx, anthropic.ModelListParams{})
	if err != nil {
		return 0, err
	}
	if page == nil {
		return 0, nil
	}
	return len(page.Data), nil
}

// openAIClientOf reaches the SDK client inside whichever OpenAI provider shape
// the registry built.
func openAIClientOf(provider AIProvider) (*openai.Client, bool) {
	switch p := provider.(type) {
	case *openAIProvider:
		return p.client, p.client != nil
	case *openAIStreamProvider:
		return p.client, p.client != nil
	case *openAITTSProvider:
		return p.client, p.client != nil
	}
	return nil, false
}

// selectVendorEntry picks the provider entry to check.
//
// The argument is a VENDOR name ("anthropic" / "openai" -- how an operator
// says it, and how --provider is documented) or the exact name of one DSL
// provider entry. A vendor name is also the name of that vendor's @base
// provider, which is metadata and carries no client, so it is treated as "any
// entry of this vendor".
func selectVendorEntry(registry *ProviderRegistry, providerName string) (*ProviderConfigEntry, error) {
	name := strings.TrimSpace(providerName)
	lower := strings.ToLower(name)

	vendorNamed := lower == "" || anthropicProviderTypes[lower] || lower == "openai"
	if !vendorNamed {
		entry, ok := registry.Entry(name)
		if !ok {
			return nil, fmt.Errorf("no provider named %q is declared in the DSL tree", name)
		}
		if !anthropicProviderTypes[strings.ToLower(entry.Config.Type)] && !isOpenAIProviderType(entry.Config.Type) {
			return nil, fmt.Errorf(
				"provider %q is type %q; provider-auth check covers the two federating vendors, "+
					"Anthropic and OpenAI", name, entry.Config.Type)
		}
		return entry, nil
	}

	// Which vendor a bare name selects. An empty name means Anthropic, which
	// is what it meant before OpenAI federated -- changing it would silently
	// re-point every existing runbook step and CI invocation at a different
	// vendor.
	wantOpenAI := lower == "openai"
	matches := func(entry *ProviderConfigEntry) bool {
		if wantOpenAI {
			return isOpenAIProviderType(entry.Config.Type)
		}
		return anthropicProviderTypes[strings.ToLower(entry.Config.Type)]
	}

	names := registry.Names()
	sort.Strings(names)
	var fallback *ProviderConfigEntry
	for _, n := range names {
		entry, ok := registry.Entry(n)
		if !ok || entry.Config.Base {
			continue
		}
		if !matches(entry) {
			continue
		}
		if entry.Available {
			return entry, nil
		}
		if fallback == nil {
			fallback = entry
		}
	}
	if fallback != nil {
		// Every entry of this vendor failed to construct. Returning one anyway
		// is deliberate: its construction error is the answer the operator
		// came for, and swallowing it for "none available" would hide it.
		return fallback, nil
	}
	vendor := "Anthropic"
	if wantOpenAI {
		vendor = "OpenAI"
	}
	return nil, fmt.Errorf("no %s provider is declared in the DSL tree", vendor)
}

// anthropicClientOf reaches the SDK client inside whichever of the two
// Anthropic provider shapes was registered.
func anthropicClientOf(p AIProvider) (*anthropic.Client, error) {
	switch v := p.(type) {
	case *anthropicProvider:
		return &v.client, nil
	case *anthropicStreamProvider:
		return &v.client, nil
	default:
		return nil, fmt.Errorf("provider client is %T, not an Anthropic client", p)
	}
}

// readIdentityTokenClaims re-reads the projected token for the report. It is
// separate from preflightIdentityToken because the report wants the claims
// even when it would rather not fail -- by the time this runs, preflight has
// already passed.
func readIdentityTokenClaims(path string) (map[string]any, error) {
	raw, err := readFileTrimmed(path)
	if err != nil {
		return nil, err
	}
	return parseJWTClaims(raw)
}
