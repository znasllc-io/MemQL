import { describe, expect, it } from "vitest";

import {
  conceptMatches,
  domainCounts,
  groupConcepts,
  originBadgeFor,
  originBadgeLabel,
} from "../../src/apps/concepts/registry";
import {
  readSchema,
  standingSentence,
  undeclaredFinding,
  unwrittenSentence,
} from "../../src/apps/concepts/schema";
import { cardFor } from "../../src/apps/concepts/displayCard";
import {
  captureConceptOpen,
  clearParkedConceptOpen,
  readConceptOpen,
  takeParkedConceptOpen,
} from "../../src/apps/concepts/openConcept";
import { conceptOf, nodeOf } from "./harness";

describe("searching the registry", () => {
  const rows = [
    conceptOf({ id: "v1:library:artifact", description: "An index row" }),
    conceptOf({ id: "v1:shopify:order", description: "A customer order" }),
    conceptOf({ id: "v1:shopify:product", description: "A thing for sale" }),
  ];

  it("ANDs the terms rather than ORing them", () => {
    // The narrowing reading. Under OR, "shopify order" would also match
    // every other shopify concept -- which is the opposite of what somebody
    // typing two words is asking for.
    expect(conceptMatches(rows[1]!, "shopify order")).toBe(true);
    expect(conceptMatches(rows[2]!, "shopify order")).toBe(false);
  });

  it("matches the description as well as the id", () => {
    expect(conceptMatches(rows[2]!, "for sale")).toBe(true);
  });

  it("counts domains over the UNFILTERED registry", () => {
    // A facet that renumbered as you typed would answer "how many are
    // there" with "how many are left".
    expect(domainCounts(rows)).toEqual([
      { domain: "library", count: 1 },
      { domain: "shopify", count: 2 },
    ]);
  });

  it("a domain naming nothing yields no groups, not every group", () => {
    expect(groupConcepts(rows, "", "nosuchdomain")).toEqual([]);
  });
});

describe("the origin badge", () => {
  it("native earns none -- the absence is the statement", () => {
    const native = conceptOf({ id: "v1:work:goal", dataState: "native", dataOrigin: "memql" });
    expect(originBadgeFor(native)).toEqual({ kind: "none" });
  });

  it("a mirror names the system that owns it", () => {
    const mirror = conceptOf({
      id: "v1:shopify:order",
      dataState: "mirror",
      dataOrigin: "shopify",
    });
    expect(originBadgeLabel(originBadgeFor(mirror))).toBe("Mirror of shopify");
  });

  it("a mirror with no origin earns no badge rather than a half-written one", () => {
    // A descriptor from a server predating the field. "Mirror of" with
    // nothing after it is worse than no badge.
    const old = conceptOf({ id: "v1:x:y", dataState: "mirror", dataOrigin: "" });
    expect(originBadgeFor(old)).toEqual({ kind: "none" });
  });

  it("an origin with no targets earns no badge", () => {
    const origin = conceptOf({ id: "v1:x:y", dataState: "origin", dataMirroredTo: [] });
    expect(originBadgeFor(origin)).toEqual({ kind: "none" });
  });
});

describe("declared against observed", () => {
  const concept = conceptOf({
    id: "v1:test:thing",
    fields: [
      { name: "name", kind: "string", required: true, enumValues: [], description: "" },
      { name: "retiredAt", kind: "datetime", required: false, enumValues: [], description: "" },
    ],
  });

  it("finds a declared field no loaded row carries", () => {
    // The finding this surface exists to produce: a field with no writer
    // reads exactly like one whose value happens to be empty.
    const reading = readSchema(concept, [nodeOf("a", { name: "one" })]);
    const retired = reading.fields.find((f) => f.name === "retiredAt");
    expect(retired?.standing).toBe("declared-not-seen");
    expect(retired?.presentIn).toBe(0);
  });

  it("finds a key the concept does not declare", () => {
    const reading = readSchema(concept, [nodeOf("a", { name: "one", sneaked: 3 })]);
    const sneaked = reading.fields.find((f) => f.name === "sneaked");
    expect(sneaked?.standing).toBe("undeclared");
    expect(sneaked?.observedTypes).toEqual(["number"]);
  });

  it("scopes every observed claim to the sample it rests on", () => {
    // An unscoped "no rows carry this" is a claim about the concept that a
    // page of rows cannot support.
    const reading = readSchema(concept, [nodeOf("a", { name: "one" })]);
    const retired = reading.fields.find((f) => f.name === "retiredAt")!;
    expect(standingSentence(retired, reading.sampleSize)).toBe(
      "Declared, but none of the 1 rows loaded carries it.",
    );
  });

  it("an unpublished shape is its own answer, not 'no fields'", () => {
    const bare = conceptOf({ id: "v1:test:bare", fields: [] });
    const reading = readSchema(bare, [nodeOf("a", { whatever: 1 })]);
    expect(reading.shapeUnpublished).toBe(true);
    // The rows are still profiled, so the panel has something true to show.
    expect(reading.fields.map((f) => f.name)).toEqual(["whatever"]);
  });

  it("orders required fields first", () => {
    const reading = readSchema(concept, []);
    expect(reading.fields.map((f) => f.name)).toEqual(["name", "retiredAt"]);
  });
});

describe("an undeclared key is a fault, and the surface says what it costs", () => {
  const concept = conceptOf({
    id: "v1:test:thing",
    fields: [{ name: "name", kind: "string", required: true, enumValues: [], description: "" }],
  });

  it("says nothing when there is nothing to say", () => {
    // The control. A finding object with count 0 would invite a caller to
    // render an empty warning on every clean concept in the tree.
    const reading = readSchema(concept, [nodeOf("a", { name: "one" })]);
    expect(undeclaredFinding(reading, true)).toBeNull();
  });

  it("names the consequence, not just the standing", () => {
    // The whole point. `undeclared` describes the schema; what a reader needs
    // is that the row cannot be written again -- which is what the chip could
    // never carry and what memql#5199 and memql#5210 are both about.
    const reading = readSchema(concept, [nodeOf("a", { name: "one", gender: "x" })]);
    const finding = undeclaredFinding(reading, true)!;
    expect(finding.count).toBe(1);
    expect(finding.sentence).toContain("cannot be written again");
    expect(finding.next).toContain("migration");
  });

  it("a census and a page are different claims", () => {
    // The honesty constraint this file's header states, applied to the count.
    // While the walk has more to give, the number is a FLOOR -- and a floor
    // reported as a total is the failure campaignStats refuses by name.
    const reading = readSchema(concept, [nodeOf("a", { name: "one", gender: "x" })]);
    // Asserted as the DISTINCTION rather than as the exact wording: the census
    // claims nothing about a sample, and the page says both that it is one and
    // that reading further may find more.
    const census = undeclaredFinding(reading, true)!.sentence;
    expect(census).not.toContain("loaded so far");
    expect(census).not.toContain("Loading more");
    const sampled = undeclaredFinding(reading, false)!.sentence;
    expect(sampled).toContain("loaded so far");
    expect(sampled).toContain("Loading more may find others");
  });

  it("counts the keys and leaves the naming to the list", () => {
    // Rule 7: a scope is named in one place. Listing the keys here would
    // repeat the lines directly above it.
    const reading = readSchema(concept, [nodeOf("a", { name: "one", gender: "x", planId: "p" })]);
    const finding = undeclaredFinding(reading, true)!;
    expect(finding.count).toBe(2);
    expect(finding.sentence).toContain("2 keys");
    expect(finding.sentence).not.toContain("gender");
  });

  it("the other finding is a separate sentence, not a clause of this one", () => {
    // They were one caption, which gave a fault and a curiosity the same
    // weight and the same voice.
    const withBoth = conceptOf({
      id: "v1:test:thing",
      fields: [
        { name: "name", kind: "string", required: true, enumValues: [], description: "" },
        { name: "unwritten", kind: "string", required: false, enumValues: [], description: "" },
      ],
    });
    const reading = readSchema(withBoth, [nodeOf("a", { name: "one", gender: "x" })]);
    // The fault's sentence is about the fault and carries none of the other
    // finding's words -- no count of unwritten fields, no "carried by none".
    expect(undeclaredFinding(reading, true)!.sentence).not.toContain("carried by none");
    expect(unwrittenSentence(reading, true)).toBe(
      "1 declared field is carried by none of this concept's 1 row.",
    );
  });

  it("counts rows in English", () => {
    // "1 rows" is the kind of sloppiness that reads as machine-generated, and
    // this surface is read by people deciding whether to write a migration.
    const one = readSchema(concept, [nodeOf("a", { name: "one", gender: "x" })]);
    expect(undeclaredFinding(one, false)!.sentence).toContain("in the 1 row loaded so far");
    const two = readSchema(concept, [
      nodeOf("a", { name: "one", gender: "x" }),
      nodeOf("b", { name: "two" }),
    ]);
    expect(undeclaredFinding(two, false)!.sentence).toContain("in the 2 rows loaded so far");
  });

  it("says nothing about unwritten fields when nothing has been loaded", () => {
    // With no rows, EVERY declared field is unseen -- which is a fact about
    // the walk rather than about the concept, and reporting it would fire on
    // every page before its first read returns.
    const reading = readSchema(concept, []);
    expect(unwrittenSentence(reading, false)).toBe("");
  });
});

describe("rendering a row of a concept nobody designed a screen for", () => {
  it("falls back to the id rather than guessing at a name field", () => {
    // The tempting fallback -- name, then title, then label -- is a guess
    // that reads as a fact.
    const concept = conceptOf({ id: "v1:test:thing" });
    const card = cardFor(concept, nodeOf("v1:test:thing:abc", { name: "not the name" }));
    expect(card.primary).toBe("v1:test:thing:abc");
    expect(card.inferred).toBe(true);
  });

  it("uses the declared card when there is one", () => {
    const concept = conceptOf({
      id: "v1:test:thing",
      displayCard: { primary: "title", secondary: "kind", status: "active" },
    });
    const card = cardFor(concept, nodeOf("id-1", { title: "A thing", kind: "big", active: true }));
    expect(card.primary).toBe("A thing");
    expect(card.secondary).toBe("big");
    expect(card.status).toBe("true");
    expect(card.inferred).toBe(false);
  });

  it("a declared primary that is empty on this row falls back to the id", () => {
    // A nameless entry in a list is unpickable.
    const concept = conceptOf({ id: "v1:test:thing", displayCard: { primary: "title" } });
    const card = cardFor(concept, nodeOf("id-2", { title: "" }));
    expect(card.primary).toBe("id-2");
  });
});

describe("arriving with a concept named in the address", () => {
  it("an EMPTY value is not a request", () => {
    // Honouring it would open the app on its list, making a broken link
    // indistinguishable from a working one.
    expect(readConceptOpen("?concept=")).toBeNull();
    expect(readConceptOpen("?concept=%20%20")).toBeNull();
  });

  it("reads the id, scrubs only its own parameter, and parks it once", () => {
    clearParkedConceptOpen();
    const replaced: string[] = [];
    const win = {
      location: { search: "?concept=v1:library:artifact&keep=1", pathname: "/", hash: "" },
      history: { replaceState: (_s: unknown, _t: string, url: string) => replaced.push(url) },
    } as unknown as Window;

    expect(captureConceptOpen(win)).toBe("v1:library:artifact");
    // Only its own parameter goes; everything else is left for whoever
    // else reads this query string at boot.
    expect(replaced).toEqual(["/?keep=1"]);
    expect(takeParkedConceptOpen()).toBe("v1:library:artifact");
    // ONCE: a second take finds nothing, so a StrictMode remount cannot
    // open a second window.
    expect(takeParkedConceptOpen()).toBeNull();
  });
});
