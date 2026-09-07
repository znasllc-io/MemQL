package memql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3/option"
)

// OpenAI workload identity federation (epic memql#5088).
//
// The engine reached OpenAI with a static key that never rotated, lived in
// every engine pod's environment, and let anyone who could read the
// `memql-secrets` Secret spend against the account from anywhere. Federation
// replaces it: the pod presents the OIDC JWT Kubernetes projects for it, the
// engine exchanges that at OpenAI's auth host for a bearer that expires within
// the hour, and nothing long-lived is at rest.
//
// The 2026-08-22 Anthropic record said OpenAI offered no federation. THAT WAS
// WRONG WHEN IT WAS WRITTEN -- OpenAI's workload identity federation has been
// generally available since 2025-05-26 -- and the sentence outlived its
// correction for long enough to shape a design. It is deleted from that
// runbook rather than annotated.
//
// WHY THE ENGINE OWNS THE EXCHANGE RATHER THAN THE SDK (design D3).
// option.WithWorkloadIdentity exists on openai-go/v3 and would serve the HTTP
// clients perfectly well. It would NOT serve the Realtime transcription
// WebSocket, which dials with a raw `Authorization: Bearer` header of its own
// and never goes through the SDK's request pipeline. Letting the SDK federate
// would leave two exchangers with two caches and two refresh clocks, one of
// them invisible to `provider-auth check` -- so the operator command that
// exists to answer "is federation working" would be answering it about half
// the traffic. One exchanger here, and every consumer takes its bearer.
//
// Runbook (Platform console setup, the two ids, verification):
// docs/public/operate/auth/openai-federation.md

// The three federation env names, as the DSL provider's auth block references
// them (dsl/providers/providers.memql). They appear here for the error
// messages ONLY -- the values are read out of the provider's RESOLVED auth
// map, never with os.Getenv, so the three-tier resolution (globalSecret ->
// globalVariable -> env) applies to them and the env registry's "registered
// but read nowhere" gate sees the DSL reference.
const (
	envOpenAIIdentityProviderID = "MEMQL_AI_OPENAI_IDENTITY_PROVIDER_ID"
	envOpenAIServiceAccountID   = "MEMQL_AI_OPENAI_SERVICE_ACCOUNT_ID"
	envOpenAIIdentityTokenFile  = "MEMQL_AI_OPENAI_IDENTITY_TOKEN_FILE"
)

// openaiAudience is the audience the projected Kubernetes token must carry and
// the one the identity-provider mapping pins. Kubernetes mints a token FOR one
// audience, so the Anthropic token mounted beside it is not interchangeable
// with this one -- which is the point, and the reason every engine Deployment
// carries two projected volumes rather than sharing one.
const openaiAudience = "https://api.openai.com/v1"

// openaiTokenEndpoint is where the RFC 8693 exchange is posted. It is a var so
// the exchange tests can point it at an httptest server; nothing else writes
// it.
var openaiTokenEndpoint = "https://auth.openai.com/oauth/token"

// The auth-map keys the DSL's openai auth block declares.
const (
	authKeyIdentityProviderID     = "identityProviderId"
	authKeyOpenAIServiceAccountID = "serviceAccountId"
	authKeyOpenAITokenFile        = "identityTokenFile"
	// authKeyOpenAITokenEndpoint overrides the exchange endpoint. It exists for
	// the same reason authKeyBaseURL does -- pointing a provider at a local
	// server -- and is not something an operator sets.
	authKeyOpenAITokenEndpoint = "tokenEndpoint"
)

// credentialPathUnavailable names the branch where no credential is
// configured at all.
//
// It is a PATH rather than an error because it is the normal state of a fresh
// cluster and of every local cluster: a k3d cluster's OIDC issuer is private,
// so neither vendor can discover it, and after this epic there is no key to
// fall back to (design D6). The readiness model reports that as `ai`
// unconfigured; nothing about it is a fault.
const credentialPathUnavailable credentialPath = "unavailable"

// openaiFederation is the resolved federation half of an OpenAI provider's
// auth block.
type openaiFederation struct {
	IdentityProviderID string
	ServiceAccountID   string
	TokenFile          string
	// TokenEndpoint is empty in every real configuration; see
	// authKeyOpenAITokenEndpoint.
	TokenEndpoint string
}

// requiredFederationFields is the set that decides the branch: all three
// present means federate, none means unavailable, anything between refuses.
//
// The order is the order the missing-field error names them in, which is the
// order the runbook creates them in.
func (f openaiFederation) requiredFederationFields() []struct {
	envName string
	value   string
} {
	return []struct {
		envName string
		value   string
	}{
		{envOpenAIIdentityProviderID, f.IdentityProviderID},
		{envOpenAIServiceAccountID, f.ServiceAccountID},
		{envOpenAIIdentityTokenFile, f.TokenFile},
	}
}

func (f openaiFederation) present() []string {
	var out []string
	for _, field := range f.requiredFederationFields() {
		if field.value != "" {
			out = append(out, field.envName)
		}
	}
	return out
}

func (f openaiFederation) missing() []string {
	var out []string
	for _, field := range f.requiredFederationFields() {
		if field.value == "" {
			out = append(out, field.envName)
		}
	}
	return out
}

func (f openaiFederation) tokenEndpoint() string {
	if endpoint := strings.TrimSpace(f.TokenEndpoint); endpoint != "" {
		return endpoint
	}
	return openaiTokenEndpoint
}

// openaiFederationFrom reads the federation half out of a provider's resolved
// auth map.
func openaiFederationFrom(auth map[string]string) openaiFederation {
	return openaiFederation{
		IdentityProviderID: strings.TrimSpace(auth[authKeyIdentityProviderID]),
		ServiceAccountID:   strings.TrimSpace(auth[authKeyOpenAIServiceAccountID]),
		TokenFile:          strings.TrimSpace(auth[authKeyOpenAITokenFile]),
		TokenEndpoint:      strings.TrimSpace(auth[authKeyOpenAITokenEndpoint]),
	}
}

// BearerSource hands out the current OpenAI bearer, exchanging when needed.
//
// It is an interface rather than the concrete exchanger because it has three
// consumers with nothing else in common: the SDK middleware, the Realtime
// transcription WebSocket, and `provider-auth check`. Every one of them wants
// the same thing -- a token that is valid right now -- and none of them should
// know how it is obtained or cached.
type BearerSource interface {
	Bearer(ctx context.Context) (token string, expiresAt time.Time, err error)
}

// Refresh thresholds. The advisory one re-exchanges early enough that no
// request waits on the network; the mandatory one is the point past which a
// cached token is not handed out at all. They are the Anthropic SDK's own
// numbers, adopted rather than invented so both vendors behave alike.
const (
	openaiRefreshAdvisory  = 120 * time.Second
	openaiRefreshMandatory = 30 * time.Second
)

// openaiExchanger performs the RFC 8693 token exchange and caches the result.
type openaiExchanger struct {
	fed        openaiFederation
	httpClient *http.Client

	// now is injectable so the refresh-threshold tests do not sleep.
	now func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newOpenAIExchanger(fed openaiFederation, httpClient *http.Client) *openaiExchanger {
	if httpClient == nil {
		httpClient = guardedHTTPClient(nil)
	}
	return &openaiExchanger{fed: fed, httpClient: httpClient, now: time.Now}
}

// Bearer returns a valid access token, exchanging the projected identity token
// for a new one when the cached bearer is missing or close to expiry.
func (x *openaiExchanger) Bearer(ctx context.Context) (string, time.Time, error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	now := x.now()
	if x.token != "" && x.expiresAt.After(now.Add(openaiRefreshMandatory)) {
		// Inside the advisory window the cached token is still handed out --
		// this is a synchronous refresh, so pre-empting here would make some
		// unlucky caller wait for a network round trip in order to spare a
		// later one. The mandatory threshold is what protects correctness; the
		// advisory one is what the check command reports.
		return x.token, x.expiresAt, nil
	}

	token, expiresAt, err := x.exchange(ctx)
	if err != nil {
		// THE LAST GOOD BEARER KEEPS WORKING. A denial is a configuration
		// answer that arrives while a perfectly valid token is still in hand,
		// and dropping it would turn a misconfiguration an operator has up to
		// an hour to fix into an immediate outage. This mirrors what the
		// Anthropic SDK does with its own cache, and the `denied` counter is
		// what makes the window visible.
		if x.token != "" && x.expiresAt.After(now) {
			return x.token, x.expiresAt, nil
		}
		return "", time.Time{}, err
	}

	x.token, x.expiresAt = token, expiresAt
	return token, expiresAt, nil
}

// exchangeRequest is the RFC 8693 token-exchange body OpenAI expects.
type exchangeRequest struct {
	GrantType        string `json:"grant_type"`
	SubjectTokenType string `json:"subject_token_type"`
	SubjectToken     string `json:"subject_token"`
	IdentityProvider string `json:"identity_provider_id"`
	ServiceAccount   string `json:"service_account_id"`
}

const (
	tokenExchangeGrantType = "urn:ietf:params:oauth:grant-type:token-exchange"
	subjectTokenTypeJWT    = "urn:ietf:params:oauth:token-type:jwt"
)

// exchange posts the projected token and returns the bearer it is exchanged
// for. The caller holds x.mu.
func (x *openaiExchanger) exchange(ctx context.Context) (string, time.Time, error) {
	// The token file is re-read on EVERY exchange rather than cached at
	// construction. Kubernetes rewrites a projected token in place as it
	// approaches expiry, and a copy taken at boot is a copy that stops being
	// valid on a schedule nothing here controls.
	subject, err := readFileTrimmed(x.fed.TokenFile)
	if err != nil {
		return "", time.Time{}, fmt.Errorf(
			"cannot read the projected identity token at %s (%s): %w",
			x.fed.TokenFile, envOpenAIIdentityTokenFile, err)
	}

	body, err := json.Marshal(exchangeRequest{
		GrantType:        tokenExchangeGrantType,
		SubjectTokenType: subjectTokenTypeJWT,
		SubjectToken:     subject,
		IdentityProvider: x.fed.IdentityProviderID,
		ServiceAccount:   x.fed.ServiceAccountID,
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal openai token exchange: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, x.fed.tokenEndpoint(), bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create openai token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Through the guarded client, so the federation observer on that transport
	// sees this exchange exactly as it sees Anthropic's.
	resp, err := x.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("openai token exchange did not complete: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read openai token exchange response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The vendor's own error body is carried through, truncated, for the
		// reason the Anthropic observer carries Anthropic's: it names the
		// reason the Platform console will show, and an operator holding that
		// string can act without reading any of our code. The SUBJECT TOKEN is
		// never in an error body -- it is something we sent, not something the
		// vendor echoes -- and is never logged from here either.
		return "", time.Time{}, fmt.Errorf(
			"openai refused the token exchange (status %d): %s. Runbook: docs/public/operate/auth/openai-federation.md",
			resp.StatusCode, truncateForLog(string(raw), maxFederationBodyNote))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// The body is NOT included: on a 2xx it holds the bearer.
		return "", time.Time{}, fmt.Errorf("openai token exchange response is not JSON (status %d)", resp.StatusCode)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return "", time.Time{}, fmt.Errorf("openai token exchange returned no access_token (status %d)", resp.StatusCode)
	}

	expiresIn := time.Duration(parsed.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		// OpenAI documents at most an hour and always sends expires_in. A
		// missing value is treated as the shortest sensible lifetime rather
		// than as forever: an over-eager re-exchange costs one request, and a
		// token cached forever is a token that stops working silently.
		expiresIn = openaiRefreshMandatory * 2
	}
	return parsed.AccessToken, x.now().Add(expiresIn), nil
}

// openaiBearerMiddleware sets Authorization from the bearer source on every
// request the SDK makes.
//
// PER REQUEST, not once at construction, and that is the whole reason this is
// a middleware rather than option.WithAPIKey: a bearer expires within the hour
// and a header fixed at construction would serve a stale one for the rest of
// the process's life.
func openaiBearerMiddleware(src BearerSource) option.Middleware {
	return func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		token, _, err := src.Bearer(req.Context())
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return next(req)
	}
}

// openaiCredential decides how one OpenAI client authenticates and returns the
// request options that do it, the bearer source behind them, and which path it
// chose.
//
// The three-way switch is D2 of the design, and the middle case is the one
// worth defending. A half-configured federation is a MISCONFIGURATION, and the
// alternative -- carrying on with whatever else is around -- is exactly the
// failure this change exists to remove. There is no key to fall back to any
// more, so the only other option would be to treat "one id set" as "no ids
// set", which would silently disable OpenAI on a cluster whose operator is
// halfway through the runbook and believes it is being enabled.
//
// httpClient is the guarded client; it is attached on every branch, federation
// included, because the exchange rides the same client and the exchange
// observer lives on that transport.
func openaiCredential(cfg ProviderConfig, httpClient *http.Client) ([]option.RequestOption, BearerSource, credentialPath, error) {
	fed := openaiFederationFrom(cfg.Auth)
	present, missing := fed.present(), fed.missing()

	switch {
	case len(missing) == 0:
		if err := preflightIdentityToken(fed.TokenFile, openaiAudience, envOpenAIIdentityTokenFile); err != nil {
			return nil, nil, "", fmt.Errorf("provider %q openai federation preflight: %w", cfg.Name, err)
		}
		source := newOpenAIExchanger(fed, httpClient)
		opts := []option.RequestOption{
			option.WithHTTPClient(httpClient),
			option.WithMiddleware(openaiBearerMiddleware(source)),
			// AMBIENT CREDENTIALS ARE CLEARED, and this is load-bearing.
			// openai.NewClient prepends DefaultClientOptions, which reads
			// OPENAI_API_KEY out of the process environment and turns it into a
			// persistent Authorization header. The middleware would then
			// OVERWRITE that header per request, so the key would not actually
			// be used -- but a header set at config time is also what
			// determines whether other credential machinery engages, and
			// leaving a stray key in the request config means a future SDK
			// version could act on it while every log line here still says
			// "federation". Deleting it costs nothing: the middleware sets
			// Authorization on every request that goes out.
			option.WithHeaderDel("Authorization"),
		}
		return opts, source, credentialPathFederation, nil

	case len(present) > 0:
		// One or two set: refuse. Naming both halves is deliberate -- the
		// operator is mid-runbook and needs to know which of the three they
		// have, not only which they lack.
		return nil, nil, "", fmt.Errorf(
			"provider %q is HALF-CONFIGURED for OpenAI workload identity federation: %s set, %s missing. "+
				"Set all three, or none to leave the OpenAI providers unavailable. A partial federation config "+
				"is refused rather than quietly read as 'not configured', because an operator halfway through "+
				"the runbook believes they are ENABLING OpenAI, not disabling it. "+
				"Runbook: docs/public/operate/auth/openai-federation.md",
			cfg.Name, strings.Join(present, ", "), strings.Join(missing, ", "))

	default:
		// None set. NOT AN ERROR: this is a fresh cloud cluster and every local
		// cluster, and after key removal there is nothing else it could mean.
		// The provider registers as unavailable and the readiness model reports
		// `ai` unconfigured.
		return nil, nil, credentialPathUnavailable, nil
	}
}

// OpenAIBearer returns a bearer source for this cluster's OpenAI federation,
// or false when OpenAI is not federated here.
//
// It exists for the ONE consumer that cannot take an SDK client: the Realtime
// transcription WebSocket, which dials with an Authorization header of its own
// (app/integrations_stt.go wires it). `provider-auth check` uses it too.
//
// It resolves from a registered OpenAI provider's auth map rather than reading
// the environment, so it goes through the same three-tier resolution
// (globalSecret -> globalVariable -> env) as every other credential and sees
// ids an operator applied since boot. Reading os.Getenv here instead would
// have given the transcription path a DIFFERENT answer from every other OpenAI
// consumer on the same node, which is the kind of split that presents as
// "transcription is broken" long after the change that caused it.
func (e *MemQLEngine) OpenAIBearer() (BearerSource, bool) {
	if e == nil || e.providers == nil {
		return nil, false
	}
	for _, name := range e.providers.Names() {
		entry, ok := e.providers.Entry(name)
		if !ok || entry == nil || !strings.HasPrefix(strings.ToLower(entry.Config.Type), "openai") {
			continue
		}
		fed := openaiFederationFrom(entry.Config.Auth)
		if len(fed.missing()) != 0 {
			continue
		}
		// preflight is deliberately NOT re-run here: the provider that carried
		// these ids was constructed at boot, which is where a structurally
		// wrong token is refused. Repeating it would make a transcription
		// session fail for a reason the provider registry already reported.
		return newOpenAIExchanger(fed, guardedHTTPClient(nil)), true
	}
	return nil, false
}
