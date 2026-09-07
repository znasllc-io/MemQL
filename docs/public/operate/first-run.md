---
title: What an owner sees first
audience: public
status: stable
area: operate
sinceVersion: 0.21.0
owner: znas
---

# What an owner sees first

A fresh cluster boots ownerless. Somebody claims it through the identity
service's `/setup` page and a magic link, signs in, and MemQL OS opens on a
desk with two cards on it: **Ask**, and **Set up**.

Set up is the first-run wizard. It is a rail of the things this cluster needs
before it can do useful work, each opening the one place it is configured, and
it takes itself off the desk once they are all done.

## The four stops

| Stop | What it is | Where it is set |
|---|---|---|
| **Your passkey** | A credential on your account that is not a link in your mailbox | The identity service, under Devices |
| **AI providers** | A model this cluster can reach | Fleet, or Settings → AI providers |
| **Storage** | Where files, materialized outputs, deploy bundles and log archives live | The deployment |
| **Email sender** | A mailbox this cluster can send from | Settings → Integrations |

The three modules come from the cluster's own readiness feed — the same
reading the Cluster app's Readiness section and every app's Set up group use —
so a stop lights up when the cluster says the module is configured, not when
somebody clicks a button saying they did it.

### The passkey comes first, and that is the only ordering

A sign-in link is the only way back into an account without a passkey, so
losing access to your mailbox loses the account. That is the one law on this
rail and the widget states it in one sentence.

Everything after it may be done in any order. There is no Next, no Back and no
step number: the rail opens the first stop still outstanding, you may open any
other whenever you like, and finishing one moves the rail on by itself.

### The inference stop offers three doors

Any combination is legal, and the order is the recommendation:

- **A machine on your fleet** — a computer you already own serves the model.
  Nothing leaves it and there is no bill. The act opens Fleet with the Add
  machine panel ready.
- **Anthropic** — Anthropic serves the model, on their hardware and their
  bill. The act opens Settings → AI providers at the Anthropic panel.
- **OpenAI** — OpenAI serves the model, on their hardware and their bill.

The wizard says nothing about how a vendor proves who this cluster is. That
belongs to the panel it opens, which is where the credential is actually set —
and a sentence here naming one goes stale the next time it changes.

A **developer** gets the fleet door in full. The two federation doors are
owner-only, so a developer sees a sentence naming who can do it rather than a
button the cluster would refuse — the wizard never offers an act that is not
legal.

### Storage says where it is set, and does not pretend otherwise

Storage has no OS surface and none is invented for it. Its stop names the
variables that are still missing and says "Set in the deployment, not from
here". It is on the rail because five apps sit on it, not because the wizard
can configure it.

## What it does NOT cover

The core, and deliberately nothing else. Every app has its own **Set up** group
at the top of its Settings section — Campaigns' unsubscribe secret, the GitHub
App, workbenches, local apps — and those stay there. This wizard is what a
cluster needs before anybody gets useful work out of it, not everything a
cluster can be told.

It also gates nothing. Nothing here blocks any app, redirects anybody, or
refuses a request; the engine's own refusals are the enforcement. A cluster
with no inference will refuse the calls that need it whether or not this card
is on your desk.

## Where it goes

**It removes itself from the desk when every stop is settled**, and there is
nothing to dismiss because there is nothing to remember: the readiness
verdicts are the state. If you finish the last stop while looking at it, the
card holds for a moment with every mark lit and then folds away. A cluster
that was already configured never draws it at all.

You may also remove it like any widget, from its `...` menu. It comes back on
a fresh desk, which is the honest answer to "where did it go".

## Who sees it

Owners and developers — the two roles that can act on the stops. Everybody
else gets a desk with Ask on it and no wizard.

## Related

- [Access model](auth/access-model.md) — the roles, and what each may do
- [Identity service](auth/identity-service.md) — passkeys, magic links,
  `/me/devices`
- [Workers runbook](workers-runbook.md) — pairing a machine on your fleet
- [Environment variables](env-vars.md) — the deployment half of Storage
