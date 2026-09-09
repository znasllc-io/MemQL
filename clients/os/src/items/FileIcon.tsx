import type { ComponentType } from "react";
import {
  CalendarDays,
  File as FileGlyph,
  FileText,
  ListTodo,
  NotebookPen,
  Radio,
} from "lucide-react";

import { GeneratedGlyph } from "../apps/files/glyphs";
import { FileProvenanceDot } from "../kit";
import type { MachinePresence } from "./provenance";
import type { FileEntry } from "../system/desktop";

// SECOND MAP, SAME CONCEPT: `apps/files/glyphs.tsx` maps kind -> glyph too, and
// its own comment claims to be the only one. Nothing enforces that they agree.
// They must, so `generated_output` is IMPORTED from there rather than drawn
// again here -- one glyph, two call sites. Consolidating the maps themselves is
// its own job (memql#5263 records it) and is not done here.
//
// The value type is widened from `typeof FileGlyph` to the lucide-shaped
// signature, because the composite glyph is a plain component rather than a
// lucide forwardRef.
const GLYPHS: Record<string, ComponentType<{ size?: number | string; "aria-hidden"?: boolean }>> = {
  document: FileText,
  generated_output: GeneratedGlyph,
  note: NotebookPen,
  todo: ListTodo,
  calendar_event: CalendarDays,
  live_source: Radio,
  file: FileGlyph,
};

// A desktop file icon. Open (double-click / Enter) is the VS Code handoff;
// the desktop layer owns the handoff ports and its no-answer timeout, so
// this component only renders what it is told (spec D3).
export function FileIcon({
  entry,
  machine = null,
  selected,
  noAnswerMessage = null,
  onOpen,
  onSelect,
  onRetryUpload,
}: {
  entry: FileEntry;
  machine?: MachinePresence | null;
  selected: boolean;
  /** Non-null after a handoff nothing answered; rendered as a status. */
  noAnswerMessage?: string | null;
  onOpen: () => void;
  onSelect: () => void;
  onRetryUpload?: () => void;
}) {
  const Glyph = GLYPHS[entry.fileKind] ?? FileGlyph;
  const uploading = entry.uploadState === "uploading";
  const failed = entry.uploadState === "failed";

  return (
    <div className="os-file" data-os-file={entry.id} data-selected={selected || undefined}>
      <button
        type="button"
        className="os-file-button"
        aria-label={`${entry.title} -- open in VS Code`}
        disabled={uploading}
        onClick={onSelect}
        onDoubleClick={onOpen}
        onKeyDown={(event) => {
          if (event.key === "Enter") onOpen();
        }}
      >
        <span className="os-file-glyph" data-uploading={uploading || undefined}>
          <Glyph size={26} aria-hidden />
          <FileProvenanceDot file={entry} machine={machine} />
        </span>
        <span className="os-file-title">{entry.title}</span>
      </button>
      {uploading ? <span className="os-caption">Uploading</span> : null}
      {failed ? (
        <button type="button" className="os-file-retry" onClick={onRetryUpload}>
          Upload failed -- retry
        </button>
      ) : null}
      {noAnswerMessage ? (
        <span role="status" className="os-file-noanswer">
          {noAnswerMessage}
        </span>
      ) : null}
    </div>
  );
}
