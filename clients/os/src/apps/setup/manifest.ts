import { ListChecks } from "lucide-react";

import type { OsWidgetManifest } from "../../system/registry";
import { SetupGate } from "./SetupGate";
import { SetupWidget } from "./SetupWidget";

// The first-run wizard's manifest (design record
// 2026-09-06-first-run-wizard, D1).
//
// GATED OWNER-OR-DEVELOPER, exactly as the Integrations section is: these are
// the two roles that can act on any of the four stops, and offering a rail of
// destinations to somebody who will be refused at all four is worse than not
// offering it.
//
// It declares NO `requires`. A widget's `requires` renders the setup sentence
// in its body when a module is missing -- which for this widget would be a
// setup surface inside a setup surface, about the module it exists to set up.
export const setupWidget: OsWidgetManifest = {
  id: "setup",
  name: "Set up",
  roles: { any: ["owner", "developer"] },
  icon: ListChecks,
  // Four stops with one open, inside a desk widget. Three cells tall keeps it
  // placeable on a five-row desk (a 1280x720 browser); the body scrolls when
  // the open stop is the tall one.
  size: { w: 4, h: 3 },
  component: SetupWidget,
  gate: SetupGate,
};
