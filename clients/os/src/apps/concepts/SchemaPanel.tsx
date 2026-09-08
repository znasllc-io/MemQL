import { useMemo } from "react";
import type { Concept, Row } from "@znasllc-io/memql-sdk-core/client";

import { Caption, Chip, Notice, Panel, Subhead } from "../../kit";
import {
  readSchema,
  standingSentence,
  undeclaredFinding,
  unwrittenSentence,
  type SchemaField,
} from "./schema";

// What the concept declares, joined against what its rows carry.
//
// The join is the point -- see `schema.ts`'s header. A declared field no row
// carries, and a key no field declares, are both real defects with no other
// symptom, and neither is visible in the DSL file or in any shaped read.

export function SchemaPanel({
  concept,
  rows,
  showUndeclared,
  /** True when the walk has reached the end, so the reading is a census
   *  rather than a page. See `undeclaredFinding`: a floor reported as a total
   *  is the failure campaignStats refuses by name. */
  complete,
}: {
  concept: Concept;
  rows: readonly Row[];
  showUndeclared: boolean;
  complete: boolean;
}) {
  const reading = useMemo(() => readSchema(concept, rows), [concept, rows]);
  const fields = showUndeclared
    ? reading.fields
    : reading.fields.filter((f) => f.standing !== "undeclared");

  const finding = undeclaredFinding(reading, complete);
  const unwritten = unwrittenSentence(reading, complete);

  return (
    <Panel label="Schema">
      <Subhead>Schema</Subhead>

      {/* EMPTY IS NOT "no fields". A server that publishes no declared shape
          is a different answer from a concept with nothing in it, and
          collapsing the two renders a real concept as blank. */}
      {reading.shapeUnpublished ? (
        <Notice
          tone="info"
          sentence="This cluster does not publish a declared shape for this concept."
          next={
            rows.length === 0
              ? "Load some rows and the fields they carry will be listed here."
              : "The fields below are what the loaded rows carry, not what the concept declares."
          }
        />
      ) : null}

      {fields.length === 0 ? (
        <Caption>
          {rows.length === 0
            ? "No declared fields, and no rows loaded yet to profile."
            : "No fields."}
        </Caption>
      ) : (
        <ul className="os-schema-list">
          {fields.map((field) => (
            <FieldLine key={field.name} field={field} sampleSize={reading.sampleSize} />
          ))}
        </ul>
      )}

      {/* THE TWO FINDINGS ARE NOT ONE REMARK, and they used to be: a single
          caption ran them together, which gave a fault and a curiosity the
          same weight and the same voice.

          The undeclared half is a FAULT -- those rows cannot be written
          again -- so it takes the shape this page already uses for "this
          changes what a caller may do": a Notice that names the consequence
          and the repair. The unwritten half stays a caption, because a
          declared field nothing carries is worth knowing and costs nothing.

          THE CAPTION COMES FIRST and the fault LAST, which is the opposite of
          the order they were written in. Read at real size the panel ended on
          a quiet grey line floating under a bordered block, so the last thing
          the eye left was the finding that costs nothing.

          IT IS STATED EVEN WHEN THE PREFERENCE HIDES THE LINES (rule 4). The
          setting decides what the LIST shows; it cannot decide whether a
          fault is announced, or the one reader who turned it off is the one
          reader who never learns. Rule 4's own remedy applies -- the state
          points at the setting that produced it. */}
      {unwritten === "" ? null : <Caption>{unwritten}</Caption>}

      {finding === null ? null : (
        <Notice
          tone="warn"
          sentence={finding.sentence}
          next={
            showUndeclared
              ? finding.next
              : `${finding.next} The list above is hiding them; turn on "Show undeclared fields" in this app's Settings to see which.`
          }
        />
      )}
    </Panel>
  );
}

function FieldLine({ field, sampleSize }: { field: SchemaField; sampleSize: number }) {
  return (
    <li className={`os-schema-row os-schema-${field.standing}`}>
      <div className="os-schema-head">
        <span className="os-schema-name">{field.name}</span>
        {field.kind === "" ? null : <span className="os-schema-kind">{field.kind}</span>}
        {field.required ? <Chip tone="muted">required</Chip> : null}
        {/* Only the two exceptional standings are marked. Marking the
            ordinary one would put a chip on nearly every line and hide
            these. */}
        {field.standing === "declared-not-seen" ? (
          <Chip tone="accent" title={standingSentence(field, sampleSize)}>
            not seen
          </Chip>
        ) : null}
        {field.standing === "undeclared" ? (
          <Chip tone="accent" title={standingSentence(field, sampleSize)}>
            undeclared
          </Chip>
        ) : null}
      </div>
      {field.description === "" ? null : (
        <p className="os-schema-desc">{field.description}</p>
      )}
      {field.enumValues.length === 0 ? null : (
        <p className="os-schema-enum">{field.enumValues.join(" / ")}</p>
      )}
      {/* The observed types matter where they DISAGREE with the declaration
          or where there is no declaration at all -- an undeclared key's
          types are the only thing known about it. */}
      {field.standing === "undeclared" && field.observedTypes.length > 0 ? (
        <p className="os-schema-observed">{field.observedTypes.join(" | ")}</p>
      ) : null}
    </li>
  );
}
