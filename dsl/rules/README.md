# `dsl/rules` -- the routing rules

A **rule** maps a call's declared metadata to a **policy**. It is the third of
the three nouns the AI routing seam is built from (epic memql#5127):

- a **level** is how much intelligence a call needs -- `fast`, `strong`,
  `reasoning`, `embeddings`, and no fifth;
- a **policy** is an ordered chain of entries, each a provider name, a
  `fleet:` / `app:` / `federation:` selector, or another policy;
- a **rule** decides which policy a given call goes through.

```memql
/// Operators reason at the reasoning level.
@when(prompt="agentReply", role="operator")
@level("reasoning")
@policy("federationStrongest")
@precedence(60)
@onUnavailable("degrade")
@locked
rule operatorReasoning { }
```

## This domain is core, and sealed

`rules` is a **core** domain, exactly as `policies` is. Two consequences that
are easy to discover the hard way:

- **A pack cannot mount it.** Pack validation rejects a pack declaring a core
  domain, so a pack cannot ship a `rules/` directory of its own.
- **A runtime mount colliding with it is SKIPPED, silently by design.**
  `MEMQL_DSL_PATH` adds *new* domains; a directory whose name collides with an
  embedded one is passed over and the embedded tree keeps the namespace. A
  bundle that ships `rules/` therefore does not fail -- its rules simply are
  not there, and the cluster routes by the shipped set.

An owner extends routing by ADDING a rule at a higher precedence in their own
domain, not by redefining a shipped one. That is deliberate: a rule is the
thing that decides where a person's data goes, and "the shipped set is what
shipped" is a property worth having by construction.

## Locked, and re-seeded

Every shipped rule carries `@locked`, which means two things:

- it evaluates **before every unlocked rule regardless of precedence**, so an
  owner may add rules at any precedence they like and the shipped ones still
  run first;
- `@locked` is accepted **only in the embedded tree**. The loader refuses it
  elsewhere, because the parser cannot tell which tree it is reading -- a slice
  handed to `ParseRuleDecl` carries no origin.

Shipped definitions are re-read from the embedded tree on every boot, so
nothing done to one at runtime survives a restart.

## What lands here

The six shipped rules land in `rules.memql`:

| Rule | Matches | Policy | Exhausted chain |
|---|---|---|---|
| `default` | every call | `localFirst` | degrade |
| `reasoningParks` | `level="reasoning"` | `federationStrongest` | park |
| `embeddingsPark` | `level="embeddings"` | `localFirst` | park |
| `backgroundLane` | `tag="background"` | `localFirst` | degrade |
| `backgroundEscalation` | `tag="backgroundEscalation"` | `localFirst` | degrade |
| `operatorReasoning` | `prompt="agentReply", role="operator"` | `federationStrongest` | degrade |

`default` is the floor: it states no conditions, so it matches every call, and
a call that matches no rule is impossible by construction rather than by care.

`embeddingsPark` parks rather than degrading because a degraded embedder
answers in a **different vector space** -- the result is not a worse vector, it
is one that does not belong in the index it is about to be written to, and
nothing downstream can tell the difference.
