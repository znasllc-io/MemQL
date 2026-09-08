---
title: Settings, AI -- doors, levels, rules and decisions
audience: public
status: stable
area: operate
sinceVersion: 0.21.0
owner: znas
---

# Settings, AI

Four screens in MemQL OS, under Settings. They answer four different
questions, which is why they do not look alike.

| Screen | The question | The shape it takes |
|---|---|---|
| Doors | Where can a model come from? | A list of places, in try order |
| Levels | What would happen if something asked for this? | Four sentences |
| Rules | Who gets what? | An ordered list, first match wins |
| Decisions | What actually happened? | A log |

There is no API key field on any of them, or anywhere else in the product.

---

## The order is the point

A call tries the doors in this order and takes the first one that is open:

1. **Your machines** -- a model running on hardware you own. Nothing is billed.
2. **Signed-in apps** -- Claude Code or Codex on a machine you own, spending a
   subscription you already pay for. Nothing is billed here either.
3. **Anthropic** or **OpenAI** -- billed per call, which is why they are last.

That order does not vary with what your cluster currently has. A fresh cluster
shows the same four rows a fully configured one does, with the two free doors
shut -- which is the thing it should be showing you.

An owner who genuinely wants a paid model first writes a rule of higher
precedence that names it. That is explicit, and every decision record then
reports that it was.

## Doors

One row per door: what is true of it now, what it costs, and at most one act.

- **Your machines**: "Add a machine" opens Fleet's guided install with local
  models already selected: the install line carries `--inference`, the second
  command is stated beside it, and the Checks stop offers the recommended pull
  and a round-trip test once the machine reports.
- **Signed-in apps**: names the apps that are signed in and opens Fleet, Apps,
  where delegation is set.
- **Anthropic** / **OpenAI**: opens that vendor's federation form below the
  list. The form asks for ids from the vendor's own console, never a
  credential.

**Saving is not applying.** Saving writes the ids; the registry each node
resolved at boot does not move until Apply broadcasts. They are two controls
because they are two facts.

**A half-configured vendor is the state that shouts**, because the engine
refuses to boot on it -- hours after the save that caused it. One to three of
Anthropic's four ids, or one of OpenAI's two, and the fleet goes down at its
next restart. That row carries the engine's own sentence, which names exactly
which ids are set.

**A local cluster is told, not asked.** Its OIDC issuer is not reachable from
the public internet, so neither vendor can verify a token it mints. The forms
are absent there with a sentence in their place -- no id you enter could ever
work, and an afternoon spent trying is an afternoon wasted.

## Levels

A level is how much intelligence a call needs. There are four: `fast`,
`strong`, `reasoning`, `embeddings`. Every call names one and none of them
names a model, which is what lets a model change without a release.

This screen has no controls. It is a mirror: it says what each level resolves
to right now, and the way to change the answer is to open a door or write a
rule.

Each row is a sentence rather than a table cell, and that is deliberate. A
level with no local door would leave three columns to fill, and the mark you
fill them with is a dash -- so four levels would carry nine dashes and read as
a broken screen rather than as a cluster that has not pulled a model yet.
Written as a sentence, the same row is the answer:

> **reasoning** -- No model on your fleet, so a signed-in app takes this. Your
> subscription, nothing billed here.

**`embeddings` never degrades**, at any setting. A smaller embedder answers in
a different vector space, so the vector does not belong in the index it is
about to be written to, and every later similarity read comes back plausible
and wrong. Every other level steps down rather than parking.

## Rules

A rule looks at what a call is -- its level, its prompt, the role acting, a
tag, what it touches -- and picks a policy. Rules are tried in order and the
first match wins.

Each row is the rule written as one English sentence, because you read this
list to decide whether the set does what you meant, and a row of field values
makes you assemble that claim yourself once per row.

### Shipped rules, and the floor

Shipped rules come back on every restart and cannot be edited or removed. All
but one of them run before the rules you write, so the way past one is a rule
of your own that matches more narrowly.

The exception is **the floor** -- the shipped rule that states no conditions.
It runs **last**, and it is drawn last, below a hairline. If it ran with the
other shipped rules it would match every call before any rule of yours was
consulted, and your whole set would be dead with nothing failing and every
rule still listed.

### Describing a rule

"Describe a rule" takes a sentence in your own words and compiles it. The
panel has three states and each act appears only when it becomes legal:

1. **Typing.** One text field, nothing else.
2. **Compiled.** The rule your sentence became, written back as a sentence in
   the same form the list uses -- so what you approve is what you will see
   afterwards. "Check it" appears.
3. **Simulated.** What the rule would have done to calls your cluster has
   actually made. **Only now does "Activate" appear.**

Editing the sentence withdraws the simulation, because a check belongs to one
exact rule.

If the compiler cannot read your sentence it says so in its own words and
leaves your text in the box.

"Edit as fields" is the structured editor, for people who prefer it. Each
condition there is a checkbox plus a value, and the checkbox is the point: a
condition that is not ticked is not sent at all, which is different from one
sent empty. An absent condition means the rule does not look at that; a
condition present and empty means it matches only an empty value.

## Decisions

Every call, newest first: the rule that decided it, the door it went through,
the model, what it cost and how long it took. Refine by level, door, rule and
outcome.

**A free call never shows a money figure.** A call your own machine answered,
or one a subscription paid for, cost this cluster nothing -- and `$0.00` would
claim a measurement was taken and came out zero, which is a different
statement. It would also make a free call and a mis-priced billed one look
identical. Those rows say "your own machine, no charge" instead. A call whose
billing nobody reported says "not reported", and is never rounded toward
either answer.

**A row expands to the walk**: every entry the chain looked at, its door, and
why it was passed over. This is the half that separates "there was nothing
else to try" from "everything else was shut" -- two very different situations
with the same outcome, and the pair of them is what the list exists to tell
apart.

## Who can see what

| Screen | Who |
|---|---|
| Doors | Owner or developer |
| Rules | Owner or developer |
| Levels | Owner, developer or admin |
| Decisions | Owner, developer or admin |

Decisions is readable one rung wider because a decision record carries neither
the prompt nor the error message -- the engine's projection omits both -- so
an admin answering "why did this go to a vendor" can have it without being
shown anything they should not see.

Note that on this cluster's role ladder **developer outranks admin**, so a
floor of "admin" admits all three.

## Related

- [What an owner sees first](first-run.md) -- the gate that holds a fresh
  cluster until it has an inference door.
- [Local models](local-models.md) -- pulling a model onto a machine you own.
- [Workers runbook](workers-runbook.md) -- pairing a machine.
