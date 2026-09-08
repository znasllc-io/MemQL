# The per-account front door -- Scope (sub-project D of the access program)

- **Date:** 2026-09-07
- **Epic:** memql#5168 (the design-session task #5191), filed 2026-09-08
- **Status:** scope agreed with the owner in the 2026-09-07 brainstorm; **the design is a
  session of its own**, to be held after sub-project A ships. This file fixes what that
  session must answer and what it inherits, so it starts in the right place. It is not
  implementation-ready and no epic is filed from it.
- **What it is:** serving this cluster's host set under an account's reserved MemQL name:
  `os.memql.acme.com`, `api.memql.acme.com`, `identity.memql.acme.com`, so a client's
  people reach the OS, the API and sign-in under their own company's domain.
- **What lands before it:** record A's reservation. The account row records `memqlDomain`
  and `memqlReservedAt` once ownership of the client's domain is proven; the account page
  shows the three hosts as reserved and says they are not served yet.

## Why it is its own session

The front door today is one derivation with three consumers: the host set, the identity
issuer, and the CORS origins and OAuth redirect URIs all derive from the single
`MEMQL_DOMAIN` through `component/frontdoor` and `component/envregistry/domain.go`, and
CLAUDE.md records why a second copy of that derivation is the failure that presents as
"sign-in is broken with every manifest looking correct". A per-account host set is a
second SOURCE for that derivation, read from graph rows rather than the environment. That
is a change to a load-bearing rule, and it deserves a brainstorm with the groups engine
already real, so the account context on the edge is a fact and not a guess.

## What the design session must answer

1. **The derivation gains a second source.** Account rows with a reserved name become an
   input beside the environment domain, consumed by the front-door generator
   (`cmd/frontdoorhosts` writes exact-host rules today), the edge's host resolution
   (`component/edge/resolve.go`, which already has the custom-domain alias step), the
   identity issuer list, and CORS. Decide whether the generator learns accounts or whether
   per-account hosts are provisioned by the custom-domain reconciler's script path
   (`scripts/deploy/bind-custom-domain.sh`, exact-host Ingress plus HTTP-01 Certificate)
   and never appear in a rendered overlay. The second is the likelier answer: it is how a
   client's own domain is served today, and a reserved name is three of those.
2. **The pointing check per host.** Three CNAMEs to the cluster's edge host, verified by
   `CheckPointing` per host before any certificate is requested, the custom-domain D4
   discipline; Let's Encrypt rate limits are real.
3. **The issuer model.** Sign-in at `identity.memql.acme.com` must complete on
   `os.memql.acme.com`; cookies are host-scoped. One issuer per reserved name, or one
   issuer with several audiences, verified against the shared JWKS
   (`component/identity/verifier` fetches one JWKS and accepts one issuer today). The
   magic-link device binding (`memql_ml` on the requesting host) and the passkey RP id
   (derived from `MEMQL_IDENTITY_BASE_URL`, never the request Host) both have to be
   answered per host.
4. **The edge and the account context.** `os.memql.acme.com` resolves to the OS site
   (`systemOwned`, exempt from the lifecycle) with the account in context: at minimum the
   shell knows which account's front door it came through, so the Launcher, the Accounts
   app and the pickers can default to it. Whether the OS under a client's name is the
   same OS or a narrowed one is a product decision for that session.
5. **The API host.** `api.memql.acme.com` to the bff over h2c, and its CORS origin is the
   OS host under the same name; the generated path block
   (`cmd/frontdoorpaths`) applies unchanged because paths do not vary by host.
6. **Who binds.** Cluster owner only in v1, the custom-domain D1 reasoning; the account
   page's stop shows the state and never offers a control below that floor.
7. **The guardrails.** A reserved name is never under the cluster's own domain and never
   a front-door host (`component/memql/platform_custom_domain_policy.go`, extended in
   record A); one reserved name per account; the per-site maximum applies to the three
   hosts together.

## What it inherits

- Record A: `account.memqlDomain`, `memqlReservedAt`, `domainStatus`, the ownership
  token at `_memql-verify.<domain>`, and the account page's MemQL address stop.
- The custom domains design (`2026-09-01-custom-domains-design.md`): the reconciler, the
  script contract, the typed failure reasons, the edge alias step, and the render gate
  reading the ACME solver.
- The front-door standard (`docs/public/operate/front-door.md`) and the host-set
  derivation (`component/frontdoor/hosts.go`, `frontdoor.Roles()`, `OsSite`).
- Environment parity: no `if env == "..."`, the same shape locally; a local cluster with
  no ACME issuer gets the typed refusal `no_acme_issuer`, not a pretend success.

## Out of scope for that session, already decided

Wildcard client certificates; registrar integrations; a separate identity provider per
account (the cluster's identity service stays the one provider; OIDC federation stays a
cluster-level upstream); serving anything for an account whose ownership is not verified.
