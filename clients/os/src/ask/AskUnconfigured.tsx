import { MODULE_DESCRIPTIONS } from "../system/modules";

// WHAT ASK SAYS ON A CLUSTER WITH NO INFERENCE DOOR (design record
// 2026-09-07-core-gate-and-honest-install, D7).
//
// ONE COMPONENT, TWO ENTRY POINTS. The Ask WIDGET gated on the `ai` module and
// the Ask SHEET did not, so pressing the shortcut on an unconfigured cluster
// sent a question and rendered the engine's own error back at the person: "no
// streaming provider available". One Ask whose answer differs by which key you
// pressed is two products, so the words are one component rather than two
// copies of a string.
//
// The description is the ENGINE's, verbatim from the env manifest, so a module
// whose purpose changes says so here with no edit -- and it is the same
// sentence the per-window setup surface and the wizard's rail already use.
export function AskUnconfigured() {
  return (
    <p className="os-caption os-ask-unconfigured">
      {MODULE_DESCRIPTIONS.ai} An owner or developer can set it up in Settings.
    </p>
  );
}
