# The per-account front door -- Design (sub-project D of the access program)

- **Date:** 2026-09-08
- **Epic:** memql#5168. Design session: memql#5191.
- **Status:** approved. Every fork below was put to the owner in the 2026-09-08
  session and answered; each locked decision names what it rejected.
- **Scope record:** `2026-09-07-per-account-front-door-scope.md`, whose seven
  questions this record answers one for one (D1-D7 below are numbered to match).
- **Program index:** `2026-09-07-access-program.md`.
- **What it is:** serving this cluster's host set under an account's reserved
  MemQL name -- `app.memql.acme.com`, `api.memql.acme.com`,
  `id.memql.acme.com` -- so a client's people reach the OS, the API and sign-in
  under their own company's domain.
- **What landed before it:** record A's reservation (memql#5165). The account
  row records `memqlDomain` and `memqlReservedAt` once ownership of the
  client's domain is proven. Record A section N: "Record D consumes
  `memqlDomain` and `memqlReservedAt` and nothing else." That is still true of
  this record.

## Why this was its own session

The front door is one derivation with three consumers: the host set
(`component/frontdoor`), the identity issuer and the CORS origins and OAuth
redirect URIs (`component/envregistry/domain.go`). CLAUDE.md records why a
second copy of that derivation is the failure that presents as "sign-in is
broken with every manifest looking correct".

This record does **not** add a second copy. It adds a second *source* at
exactly one layer -- provisioning -- and leaves the derivation itself single.
`MEMQL_DOMAIN` remains the only thing `component/frontdoor` and
`component/envregistry` know about. A reserved name never enters a rendered
overlay, never changes an env derivation, and never widens the issuer. What it
does is put four more Ingress objects and one more Certificate into the cluster
at runtime, through the reconciler path that already serves a client's own
domain, and teach three browser-facing values to be resolved per request from a
row instead of read from the environment.

## Locked decisions

| # | Decision | Choice (owner-approved) |
|---|---|---|
| D1 | The second source | **The reconciler's script path, never a rendered overlay.** A reserved name is provisioned at runtime by a capability script, exactly as a client's own domain is. `cmd/frontdoorhosts` does not learn about accounts. Rejected: teaching the generator, which would make every new client a regeneration, a merge and an ArgoCD sync -- a release per client -- and would put runtime rows into a build-time artifact whose `--check` gate compares against a committed domain |
| D2 | The pointing check | **Pointing only, three CNAMEs, one target.** Ownership of `acme.com` is already proven by record A's TXT walk, and `memqlReservedAt` is stamped only on success; `memql.acme.com` is under it. All three names CNAME to the same target -- the cluster's edge host -- because one ingress controller terminates all three and routes by `Host`. Rejected: a second ownership token per reserved name, which would ask an operator to prove twice what the account row already asserts |
| D3 | The issuer model | **One issuer, many hosts.** The identity service keeps issuing `iss = https://identity.<cluster-domain>` whatever host the request arrived on, over one keyset, so `component/identity/verifier` is untouched -- it accepts exactly one issuer today and keeps doing so. What becomes per-reserved-name is only what a browser sees: the sign-in origin, the WebAuthn RP id, the `app` client's redirect URI and the identity CORS origin. Rejected: an issuer per reserved name, which would need the verifier to accept N issuers and fetch N JWKS to distinguish tokens that one service signs with one key, buying no enforcement -- authorization is per-row through record A's account grant and `sub` is cluster-wide either way |
| D4 | The edge and the account context | **The same OS, with the account in context.** One shell, one product. The narrowing already exists and is enforced server-side by record A's `account=` argument and each surface's `roles: { min }`. The front door adds context: `RuntimeConfig` gains an `account` block, and the OS defaults its pickers to it and shows whose door the visitor came through. A cluster owner arriving here sees the full OS with that marker, because hiding it from them is not a boundary. Rejected: a narrowed OS per account -- a second product surface to design, test and screenshot, every narrowing of which is a presentation mirror of a rule the engine already enforces |
| D5 | The API host | **The whole generated path block, from the same generator.** `api.<reserved>` gets byte-identical routing to `api.<cluster-domain>`: the h2c catch-all plus the generated HTTP path block, which `cmd/frontdoorpaths` now also emits as a committed Go slice under the same staleness gate. Rejected: a hand-kept subset of "what a client actually calls", which is the second source that leaves the next HTTP route silently unrouted under every client's name and failing as HTTP/1.1 handed to an h2c backend, naming nothing |
| D6 | Who binds | **Cluster owner only**, the custom-domain D1 reasoning. Certificates and Ingress objects are cluster-level resources with real rate limits, and this automates what is already owner territory rather than widening it. The Accounts app shows the state to admin and above and offers the act to nobody below cluster owner |
| D7 | The guardrails | Record A's two refusals stand (a reserved name is never under the cluster's own domain, never a front-door host). D adds: the three derived hosts must not collide with a live site or a live custom domain; **one front door per account**; and a **typed reservation reason**, so an absent `memqlReservedAt` stops meaning two different things |
| D8 | All three, or nothing | **The certificate is the enforcement.** One Certificate with three SANs under HTTP-01 cannot go Ready unless all three names point at the cluster, so activation is all-or-nothing by construction rather than by a rule we would have to police. One ACME order, one rate-limit unit, one Ready condition to promote on. Rejected: per-host activation, which ships a client a URL that is not answering yet and gives the rail three states to explain instead of one |
| D9 | A changed domain | **Tear the front door down.** Changing an account's `domain` clears `memqlDomain`, the token and the verification (record A, `resetAccountDomainOnChange`). A live front door whose reservation was just discarded is the cluster serving three names it no longer holds proof for, so the reconciler moves the row to `removing` and the unbind script pulls the objects. Rejected: parking it in a seventh `suspended` status, which keeps a client's people working through a typo at the cost of serving an unproven name |
| D10 | Delivery | **One PR.** Engine, scripts and OS together. The surface is one rail stop whose only content is this feature; splitting it would ship a control for a mechanism that is not there, or a mechanism nothing can reach |

## A. What exists today (the ground this builds on)

Read on 2026-09-08, at the paths named.

- **The host set is derived, not listed.** `component/frontdoor/hosts.go` takes
  `domain string` as a parameter and reads no environment variable itself:
  `Roles()` is the closed `{api, identity, mcp}`, `OsSite` is `"os"`, and
  `Hosts(domain)` returns those three plus the OS host, the `*.<domain>`
  wildcard and the apex. `CertificateSANs` is `Hosts` minus the wildcard,
  because one wildcard dnsName fails a whole HTTP-01 order (memql#4224). It is
  its own Go module with no dependencies, and that isolation is load-bearing.
- **Six env values derive from the domain.** `component/envregistry/domain.go`
  `DomainDerivations(domain)` emits the identity base URL, the expected issuer
  (the same string), the bootstrap domain, the discovery endpoint, the identity
  CORS origins (`https://api.<d>,https://app.<d>,https://os.<d>`), the
  registered OAuth clients with their redirect URIs, and the MCP public URL.
  `ApplyDomainDerivations` is **set-if-absent**, so a pinned env var silently
  defeats it -- which `render_domain_derivation_test.go` already gates.
- **The verifier accepts exactly one issuer.** `component/identity/verifier`
  passes `jwt.WithIssuer(v.issuer)` -- one string, exact match, no list, no
  alternates. This is what D3 is chosen to leave alone.
- **The passkey RP id never comes from the request Host.**
  `component/identity/webauthn.RelyingParty(baseURL)` returns
  `parsed.Hostname()` and the scheme-plus-host origin, derived from
  `MEMQL_IDENTITY_BASE_URL`. The comment at the top of that file says why: a
  Host-derived RP id would let anyone reaching the service under a name they
  control have credentials minted for it.
- **`memql_ml` is already per-host.** `component/identity/web/magiclink.go` and
  its `component/identity/http` twin set the cookie host-only (no `Domain`
  attribute) at `Path=/auth`, `SameSite=Lax`. That is the device binding, and
  it needs nothing from this record.
- **The edge resolves `Host` to a site in two steps, and caches misses.**
  `component/edge/resolve.go` normalizes the host, reads a per-replica cache
  under `singleflight`, tries `siteByHostname`, and only on `nil` tries
  `SiteForCustomDomain` (itself `liveCustomDomainByHostname` then `siteById`).
  A miss is cached, which is what stops the alias step being a database
  amplifier for anyone walking hostnames against the wildcard. No match is
  `(nil, nil)` and a plain 404.
- **A hosted site is already same-origin with its API.**
  `component/edge/proxy.go` forwards `/_memql/*` to the bff, opt-in per site on
  `site.apiProxy`; `component/edge/identity_proxy.go` forwards exactly four
  identity JSON paths (`/oauth/token`, `/auth/refresh`, `/auth/logout`,
  `/.well-known/jwks.json`) and is deliberately **not** gated on that flag.
  `RuntimeConfig.IdentityAPIBaseURL` is published empty for exactly this
  reason (memql#4154).
- **`/runtime-config.json` is already per-site.**
  `component/edge/runtimeconfig.go` serves `IdentityURL`, `IdentityAPIBaseURL`,
  `OAuthClientID` (looked up from the site's own hostname), `AuthEnabled`,
  `Domain`, an optional `Storefront` block and an always-present `Settings`
  map. The struct is documented additive-only: a field is added, never a
  required one removed.
- **Custom domains are the whole mechanism this reuses.**
  `integrations/customdomain` carries `CheckOwnership` / `CheckPointing` over a
  `Resolver` interface, a `Store` running under a system actor, a `Reconciler`
  that makes **one state transition per pass** because the row is the only
  cross-replica coordination point, and a `Provisioner` with an in-cluster and
  a script substrate chosen by capability rather than by environment.
  `scripts/deploy/bind-custom-domain.sh` is `domain.bind` under the
  capability-script contract: exit 3 with `no_acme_issuer` when no issuer is
  configured, exit 0 even when the certificate is not yet Ready, and
  `certificateReady` the only field the reconciler promotes on.
- **The guardrails already refuse the dangerous names.**
  `component/memql/platform_custom_domain_policy.go` refuses a wildcard, a
  single label, the cluster's apex or any suffix of it, any host in
  `frontdoor.Hosts(domain)`, a collision with a live site or another live
  binding, and anything past the per-site cap. Both uniqueness probes are
  two-step and read **without** row-authz narrowing, because a hostname another
  user holds must collide even when the caller cannot see that row.
- **Record A's fields, confirmed with memql-8a.** `memqlDomain` and
  `memqlReservedAt` at `dsl/accounts/concepts.memql`, stamped in the **same
  write** that sets `domainStatus: "verified"` -- one write rather than two,
  because a separate one would leave a window in which the account is verified
  and its name unreserved. `component/memql/account_domain_validation.go` is
  the home of the reserved-name guard and already emits
  `domain_not_verified`, `domain_under_cluster_domain` and
  `domain_is_front_door_host`.

## B. The row

`v1:platform:accountFrontDoor`, in `dsl/platform/concepts.memql`, beside
`customDomain` and on the same tier for the same reason.

| Field | Type | Meaning |
|---|---|---|
| `accountId` | `string!` | `references` account. One live row per account (D7) |
| `reservedName` | `string!` | Copied from `account.memqlDomain` at create. The row is self-describing, and stays readable after the reservation it came from is cleared -- which is exactly the state D9 has to act on |
| `status` | `enum(pending_dns, verifying, issuing, live, removing, removed)!` | The same six `customDomain` uses, deliberately: one vocabulary, one panel grammar, one `NonTerminal` predicate |
| `hostChecks` | `object` | Per role (`app`, `api`, `id`): `{ ok, reason, detail }`. D8 makes activation all-or-nothing; this is what still lets the rail name *which* CNAME is wrong |
| `failureReason` | `string` | Typed code, not an enum, for `customDomain.failureReason`'s reason: an unseen refusal must still reach the row |
| `failureDetail` | `string` | What the lookup or the script saw, verbatim |
| `lastCheckedAt` / `verifiedAt` / `issuedAt` / `removedAt` | `datetime` | The `customDomain` lifecycle set |

`@rowAuthz(clusterOwner)`. What a cluster serves at its front door is one
deployment's fact -- `customDomain` D3's reasoning, and the reason this is a
new concept rather than four more fields on the account row, which sits on a
rank-visible tier where a client-rank member can read it.

`@relationship(type="references", as="servedFor", field="accountId",
target=account, direction="outgoing")`.

### The three hosts

`app.<reservedName>`, `api.<reservedName>`, `id.<reservedName>`, derived in one
place -- `frontdoor.AccountHosts(reservedName)` in `component/frontdoor` beside
`Hosts`. The labels are `app` / `api` / `id` rather than the cluster's own
`os` / `api` / `identity`, and that divergence is a decision rather than an
accident: these hosts appear under a **client's** brand and are read by their
employees, who do not know what MemQL OS is. `app` and `id` are what a person
expects there. The cluster's own labels do not change.

`AccountHosts` returns them in a fixed order with their role tag, so the
certificate's SAN list, the Ingress set, the check map and the rail all walk one
slice.

## C. The typed reservation reason (D7)

memql-6e asked for this from the Accounts rail, and it is right: an absent
`memqlReservedAt` currently means either "ownership unproven" or "the name was
refused", and the rail infers which by reading the ownership stop beside it.

`account.memqlReservationReason` (string, typed code). Values: `""` (held, or
not yet walked), `ownership_unproven`, `domain_under_cluster_domain`,
`domain_is_front_door_host`, `name_collides_with_site`,
`name_collides_with_custom_domain`.

**WHO WRITES IT IS A CORRECTION TO THIS RECORD.** It said the guard in
`component/memql/account_domain_validation.go` writes it. The guard cannot: it
REFUSES the write, so there is no row left to carry a reason -- the caller gets
an error and the field would never be set on the two codes that matter most.
The state where an absent `memqlReservedAt` is ambiguous is the one where a row
DOES exist, so the writer has to be the sweep. It rides record A's
`recordAccountDomainCheck` as one more optional argument rather than a second
writer of the same fields, which is what that mutation was shaped for.

**`domain_is_front_door_host` will be written by nothing** on any cluster whose
labels are today's. Every front-door host is a single label under the cluster's
own domain, so the under-domain test fires first and that branch is unreachable
for any name a person would type. Record A asserts the overlap honestly rather
than claiming both fire; this record does not reorder those branches to make the
second reachable, and the value exists because the code that would set it does.

**The overlap memql-8a flagged is real and is kept honest.** Every front-door
host is a single label under the cluster's domain, so the under-domain test
fires first and `domain_is_front_door_host` is unreachable for any name a
person would actually type. This record does not reorder those branches to make
the second reachable -- the ordering is correct, and the code that never fires
is documented as unreachable-by-construction rather than removed, because the
name it names is the one an operator would otherwise have to guess at. The test
asserts the overlap rather than claiming both branches fire.

## D. The reconciler

`accountFrontDoorReconcile`, a builtin in `integrations/customdomain`
(**not** a new integration: it is the same DNS resolver, the same provisioner
selection, the same script seam, and a second package would be a second copy of
`CheckPointing`'s apex handling). Driven by a new automation
`reconcileAccountFrontDoors` in `dsl/platform/automations.memql` on
`reconcileCustomDomains`' schedule.

One state transition per pass, the `customDomain` discipline:

1. **A verified account with a reserved name and no row** -> create at
   `pending_dns`.
2. **`pending_dns` / `verifying`** -> `CheckPointing` for each of the three
   hosts against the cluster's edge host. Every result is written to
   `hostChecks` on every pass, so the rail always names the record still
   wrong. All three OK -> `issuing` and dispatch `frontdoor.bind`. Any miss ->
   `verifying` with `dns_not_pointing`.
3. **`issuing`** -> re-dispatch and read `certificateReady`. Ready -> `live`.
   The typed refusals (`no_acme_issuer`, `issuance_failed`) land on the row
   with the script's envelope detail; a local cluster with no ACME issuer sits
   in `issuing` forever, which is D7-of-custom-domains' correct behaviour and
   not a stuck state.
4. **The account's reservation is gone** (`memqlReservedAt` absent, or
   `memqlDomain` no longer equal to `reservedName`) -> `removing` and dispatch
   `frontdoor.unbind` (D9). This is the only transition that can leave `live`.
5. **`removing`** -> `removed` when the unbind envelope says applied. Terminal.
   Rows survive; the history is the audit.

The pointing target is `MEMQL_CUSTOM_DOMAIN_EDGE_HOST`, which already defaults
to `frontdoor.OsHost(MEMQL_DOMAIN)`. One target for all three names, because
one ingress controller terminates all three.

## E. Provisioning

`scripts/deploy/bind-account-front-door.sh` (capability `frontdoor.bind`) and
`unbind-account-front-door.sh` (`frontdoor.unbind`), under the capability-script
contract, registered in `component/automations/steps/capability_script.go`.

Params: required `accountId`, `reservedName`; optional `namespace` (`memql`),
`issuer` (**empty refuses**, exit 3 `no_acme_issuer`), `ingressClass`
(`nginx`), `edgeService` (`edge`), `edgePort` (`8085`), `bffHttpService`
(`bff-http`), `bffHttpPort` (`8085`), `bffGrpcService` (`bff`), `bffGrpcPort`
(`50051`), `identityService` (`identity`), `identityPort` (`8085`),
`waitSeconds` (`15`), `dryRun`.

Five objects, one field manager (`memql-account-front-door`), object name
`account-front-door-<sanitised accountId>`:

| Object | Host | Backend |
|---|---|---|
| `Certificate` | -- | three SANs, one secret `<obj>-tls`, `issuerRef` the cluster issuer |
| `Ingress` app | `app.<reserved>` | edge:8085, path `/` Prefix |
| `Ingress` api | `api.<reserved>` | bff-http:8085, the generated path block, no catch-all |
| `Ingress` api-grpc | `api.<reserved>` | bff:50051, path `/`, `backend-protocol: GRPC` |
| `Ingress` id | `id.<reserved>` | identity:8085, path `/` Prefix |

The api pair is two Ingress objects over one host for the reason the cluster's
own front door has two: an ingress controller's backend protocol is a
per-Service setting, so the h2c edge and the HTTP edge cannot share one object.

`--dry-run` reaches no cluster and needs no kubectl, the
`bind-custom-domain.sh` discipline: it renders and greps its own output rather
than calling `kubectl apply --dry-run=client`, which fetches OpenAPI and fails
on a CI runner.

### The path block is generated (D5)

`cmd/frontdoorpaths` gains `--emit-go=<file>`, writing
`component/frontdoor/paths.generated.go`: a `[]PathRule{Path, PathType}` slice
in the same order the Ingress blocks use. It lives in `component/frontdoor`
because that module has no dependencies and a generated slice adds none, and
because the provisioner and the manifest generator then read one artifact.

`TestFrontDoorPathsAreNotStale` gains the new file, so `make frontdoor-paths-check`
covers it with no new gate. `make frontdoor` writes it alongside the three
existing outputs.

## F. The edge and the account context (D4)

`component/edge/resolve.go` gains one lookup, **after** the custom-domain step
and before the miss is cached:

```
siteByHostname  ->  SiteForCustomDomain  ->  SiteForAccountFrontDoor
```

`SiteForAccountFrontDoor(ctx, host)` runs one query,
`liveAccountFrontDoorByAppHost(host:)`, which returns the account id, the
account name and the reserved name for a `live` row whose `app.<reservedName>`
equals the host. It resolves to the OS site by the constant `frontdoor.OsSite`
-- no second read, because the OS site's id is not a lookup.

A site's own hostname still wins, then a custom domain, then this. That order
is not arbitrary: a deployable already answering on a name must never lose
traffic to a front door, and a client's own domain is a more specific claim
than a reserved name under ours.

`Site` gains `Account *SiteAccount{ID, ReservedName}`, nil for every other
resolution path, and `RuntimeConfig` gains:

**TWO FIELDS, AND THE MISSING ONE IS A CORRECTION TO THIS RECORD.** It said
`{id, name, reservedName}`; what shipped has no account NAME, for two reasons
that only became clear against the code. The document is served
UNAUTHENTICATED to every visitor of the host, so a display name there tells any
passer-by which company this cluster serves at this name; and it would be
denormalized onto a serving row and wrong the first time somebody renamed the
client. The OS reads the name through its own authorized query once there is a
signed-in person to read it for, which is both fresher and narrower.

```go
// Account is present ONLY when this page was served through an account's
// reserved front door, and carries which account's door it was.
Account *AccountContext `json:"account,omitempty"`
```

with `{id, name, reservedName}`. Omitted entirely otherwise, so a document
served on the cluster's own host is byte-identical to what it was.

**`IdentityURL` becomes per-door.** When `Account` is present it is
`https://id.<reservedName>`; otherwise it is the env value, unchanged. This is
the one change that keeps a client's employee on their own domain through
sign-in: `IdentityAPIBaseURL` stays empty, so the four identity XHR paths are
already same-origin through the edge, and `IdentityURL` is what top-level
`/authorize` navigation uses.

`csp.go`'s `connect-src` names the door's identity origin for the same reason
it names the cluster's.

## G. Identity, per door (D3)

Three values become per-reserved-name. Every one of them is resolved from a
**live `accountFrontDoor` row**, never from `r.Host`, and falls back to the
cluster's own value when the name does not resolve. That is the whole security
property: the Host header is a claim, the row is the fact.

- **The WebAuthn RP id** is the reserved name itself (`memql.acme.com`), not
  `id.memql.acme.com`, so one passkey works across `app.` and `id.` -- an RP id
  may be a registrable-domain suffix of the origin.
  `webauthn.RelyingParty(baseURL)` keeps its signature and its callers; a new
  `webauthn.RelyingPartyForDoor(reservedName, host)` is used only where a
  resolved door is in hand, and the file's standing comment about never
  trusting `r.Host` is extended rather than weakened.
- **The `app` client's redirect URI** gains
  `https://app.<reservedName>/auth/callback` for each live door.
- **The identity CORS origins** gain `https://app.<reservedName>`.

The last two are read from rows at request time through a small cache on the
identity node, refreshed on `accountFrontDoor` events, which broadcast. An
issuer, an audience and a keyset are all untouched, so no token minted before
this change stops verifying and no verifier learns anything new.

**Two things this deliberately does not do.** It does not give a door its own
identity provider (out of scope in the scope record, and OIDC federation stays
a cluster-level upstream). It does not brand the identity service's own pages
per account: a client's employee signing in at `id.memql.acme.com` sees MemQL's
sign-in page on their own domain. Branding those pages is a product decision
nobody has asked for and is named here so it is a known state rather than a
surprise.

## H. Guardrails (D7)

In `component/memql/account_domain_validation.go`, extending record A's guard
rather than moving it:

- The three derived hosts are checked against live sites and live custom
  domains with the same two-step, un-narrowed staged-data probes
  `platform_custom_domain_policy.go` uses -- a hostname another user holds must
  collide even when the caller cannot see that row.
- One live front door per account.
- Record A's two refusals stand unchanged and unreordered (section C).

## I. The surface

The Accounts app's Domain rail (memql-6e's `AccountDomainRail.tsx`) already has
a **MemQL address** stop that says "Recorded now; served when the per-account
front door lands" and draws the three hosts as chips. This record replaces that
sentence with the live thing:

- The three hosts, each with its pointing state and, when it is wrong, the
  exact CNAME record to create -- one copyable target for all three, because
  there is one target.
- One activation state for the door, not three (D8), with the certificate's own
  condition message verbatim when it is issuing.
- The reservation reason from section C when there is no door to show, so
  "not held" says which of the two things it is.
- The act is cluster-owner only (D6); admin and above **see** the stop.

There is no manual re-check button, the custom-domain D5 reasoning: the
two-minute schedule is the only retry, because a button invites hammering a
recursive resolver and an ACME endpoint.

## J. Failure modes

- **A local cluster.** No ACME issuer: the bind script refuses with exit 3 and
  the row sits at `issuing` carrying `no_acme_issuer`. The same flow shape
  everywhere, honest about what the target can do -- environment parity, not an
  `if env ==` branch.
- **Two accounts reserving the same name.** Refused at the guard by the
  collision probe, and record A stamps `memqlReservedAt` in the same write that
  verifies, so there is no window between the two.
- **A door whose account is archived.** Record A archives the account's groups;
  the reconciler treats an archived account as a reservation that is gone and
  takes the D9 path.
- **A reserved name pointing at us that we never issued for.** The host reaches
  the ingress controller, matches no rule, and gets the default backend. The
  edge is never asked, so there is no row to leak.
- **A `live` row whose certificate later fails to renew.** cert-manager owns
  renewal; the row stays `live` and the browser sees the expiry. This record
  does not add a renewal watcher, and says so rather than implying one.
- **The account context on a cached bundle.** `RuntimeConfig` is fetched per
  page load, never bundled, so an older cached bundle that does not read
  `account` behaves exactly as before -- the additive-only rule the struct
  already documents.

## K. Testing

- `frontdoor.AccountHosts` and the label order -- pure, in the frontdoor
  module.
- The generated path slice against the manifest generator's own block, so the
  two cannot drift; folded into `TestFrontDoorPathsAreNotStale`.
- `CheckPointing` over the fake resolver for the three-host shape, including
  the one-target property and an apex reserved name.
- The reconciler's state machine, one transition per pass, and the D9
  transition proven by clearing the reservation under a `live` row.
- The guard's collision probes, db-gated, including the un-narrowed read (a
  hostname held by another user's site collides for a caller who cannot see
  it).
- Edge resolution, db-gated: a live door resolves to the OS site with the
  account attached; a `pending_dns` one does not; a site's own hostname and a
  live custom domain both still win.
- `RuntimeConfig` per door: `identityUrl` is the door's, `account` is present,
  and a document served on the cluster's own host is byte-identical to before.
- The RP id resolution: a door name resolves to the reserved name, an
  unresolvable one falls back to the cluster's, and `r.Host` never reaches it.
- Both scripts under the standing `capability_contract_test.go` gate, plus
  envelope round-trip through `deploycontrol.ParseCapabilityResult` and a
  `--dry-run` render assertion.
- The OS stop in both modes, empty and populated, screenshotted in a real
  browser.

## L. Delivery

**One PR** (D10), on `epic/per-account-front-door`, branched off `main` after
record A's engine PR lands. Tasks are filed as sub-issues of memql#5168 and all
close in that PR.

Wire changes for the frontend and the cockpit, named in the commit body:
`RuntimeConfig` gains an optional `account` block and `identityUrl` becomes
per-door; no gRPC message changes.

## M. Out of scope, and neighbors

Out of scope: an identity provider per account; branding the identity pages per
account (section G); wildcard client certificates; registrar integrations; a
`mcp.<reserved>` host (the MCP endpoint is a cluster fact a machine is
configured with once, and nothing asked for it under a client's name); serving
anything for an account whose ownership is not verified; a certificate-renewal
watcher (section J); self-serve binding, which is the custom-domain v2 shape
and would arrive for both at once.

Neighbors: record A (`2026-09-07-groups-and-grants-design.md`) supplies the two
fields; record C (`2026-09-07-users-app-and-account-ties-design.md`) owns
`AccountDomainRail.tsx`, whose MemQL address stop this record replaces; the
custom domains design (`2026-09-01-custom-domains-design.md`) is the mechanism
this reuses end to end; the front-door standard
(`docs/public/operate/front-door.md`) gains a section on the per-account
regime.
