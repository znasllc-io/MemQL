import { useEffect } from "react";

import { useSession } from "../chrome/access";
import { gateFor } from "../kit/ReadinessStates";
import { useAsk } from "./AskProvider";
import { AskSurface } from "./AskSurface";
import { AskUnconfigured } from "./AskUnconfigured";
import { useMakeGoal } from "./useMakeGoal";

// The Ask sheet: anchored above the dock (a bottom sheet on phones via
// CSS). Never a window, never counts against the desk cap (spec D6).
// Esc closes from anywhere -- the sheet is modal over the desk, so the
// close key cannot depend on where focus happens to sit.
//
// IT GATES ON THE `ai` MODULE, LIKE THE WIDGET (design record
// 2026-09-07-core-gate-and-honest-install, D7). The widget did and this did
// not, so the keyboard shortcut sent a question on an unconfigured cluster and
// rendered the engine's own error at the person: "no streaming provider
// available". Same module, same sentence, same component -- one Ask whose
// answer differs by which key you pressed is two products.

export function AskSheet() {
  const { sheet, closeAsk, transport, voice, settings } = useAsk();
  const { readiness } = useSession();
  const makeGoal = useMakeGoal();

  useEffect(() => {
    if (!sheet.open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeAsk();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [sheet.open, closeAsk]);

  if (!sheet.open) return null;
  // NOTHING WHILE THE FEED IS UNKNOWN. `gateFor` answers "unknown" for an
  // unloaded feed, and the sheet opens normally then: a shell that does not
  // yet know what is configured must not refuse a question it could answer.
  const unconfigured = gateFor(readiness, ["ai"], []).state === "unconfigured";
  return (
    <div
      className="os-ask-backdrop"
      onPointerDown={(event) => {
        if (event.target === event.currentTarget) closeAsk();
      }}
    >
      <div
        className="os-ask-sheet"
        data-os-sheet
        role="dialog"
        aria-modal="true"
        aria-label="Ask"
        onKeyDown={(event) => {
          if (event.key === "Escape") closeAsk();
        }}
      >
        {unconfigured ? (
          <div className="os-ask-sheet-unconfigured">
            <AskUnconfigured />
          </div>
        ) : (
          <AskSurface
            transport={transport}
            voicePorts={voice}
            settings={settings}
            context={sheet.context}
            variant="sheet"
            autoFocus
            makeGoal={makeGoal}
          />
        )}
      </div>
    </div>
  );
}
