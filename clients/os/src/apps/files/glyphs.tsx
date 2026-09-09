import { File, FileText } from "lucide-react";

// One glyph per content kind, in one place.
//
// IT LIVES HERE RATHER THAN IN BrowseSection BECAUSE THE RAIL NEEDS IT NOW
// (epic memql#4981). The Bin place draws file rows, and BrowseSection already
// imports Rail -- so reaching back for the glyph would close a runtime import
// cycle between the two. A `import type` does not (types are erased), which is
// why the existing `DeskFolderShortcut` import was harmless and this one would
// not have been.
//
// It is deliberately NOT in `kit/`: the kit is the OS's shared vocabulary, and
// which glyph means "generated output" is one app's reading of one concept's
// enum. Four surfaces share it -- the list row, the inspector, the Bin's rows
// and its detail panel -- and they must not drift, which is the whole reason
// there is one function rather than four `<File />`s.

// A generated file: lucide's page, with two overlapping circles where the text
// lines would be.
//
// IT IS HAND-DRAWN BECAUSE NO LUCIDE ICON SAYS "A FILE THIS SYSTEM PRODUCED".
// The candidates each said something else and the codebase proved it: a
// checkmark is this shell's checkbox idiom and collides with `validationEvent`
// and `v1:work:approval`, `Bot` is Agents, `Package` is deployables, `Zap` is
// campaigns. The circles are `Blend` -- sources composed into one output, which
// is the Materializer's actual job -- and the page keeps the silhouette that
// ties this kind to `document` and `file` beside it in the same list.
//
// THE PAGE PATH IS COPIED FROM LUCIDE `file` AND THAT IS THE MAINTENANCE COST:
// a future lucide upgrade that restyles its file outline leaves this glyph
// subtly out of step with the very neighbours it exists to match. Re-copy it
// from `node_modules/lucide-react/dist/esm/icons/file.mjs` when that happens.
//
// The radius is deliberate rather than arbitrary: rendered in real row and
// desktop chrome, 2.6 closes into a blob at 16px and 3.6 crowds the page border
// at 26px.
export function GeneratedGlyph({ size = 16 }: { size?: number | string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M6 22a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.704.706l3.588 3.588A2.4 2.4 0 0 1 20 8v12a2 2 0 0 1-2 2z" />
      <path d="M14 2v5a1 1 0 0 0 1 1h5" />
      <circle cx="10.2" cy="14.8" r="3.2" />
      <circle cx="13.8" cy="14.8" r="3.2" />
    </svg>
  );
}

export function kindGlyph(kind: string, size = 16) {
  if (kind === "document") return <FileText size={size} aria-hidden />;
  if (kind === "generated_output") return <GeneratedGlyph size={size} />;
  return <File size={size} aria-hidden />;
}
