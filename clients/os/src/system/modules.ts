// The readiness module ids the engine declares (scripts/secrets/manifest.yaml,
// `modules:`), as the shell spells them.
//
// IT IS A COPY, AND A GO GATE PINS IT. component/envregistry/os_modules_parity_test.go
// reads the tuple below and fails the build the moment it disagrees with the
// manifest. Keep the tuple on ONE line with double-quoted ids: the gate parses
// it by regexp, not by executing TypeScript.
export const READINESS_MODULES = ["ai", "storage", "email", "githubApp", "campaigns", "workbench", "localApps"] as const;

export type ModuleId = (typeof READINESS_MODULES)[number];

export function isModuleId(id: string): id is ModuleId {
  return (READINESS_MODULES as readonly string[]).includes(id);
}

/**
 * What a person calls the module. Sentence case, no variable names.
 *
 * These are the shell's words, not the engine's. The engine's `description`
 * says what the module is FOR and the setup surface renders it verbatim; this
 * is only the short label a row or a dot needs.
 */
export const MODULE_NAMES: Record<ModuleId, string> = {
  ai: "AI providers",
  storage: "Storage",
  email: "Email sender",
  githubApp: "GitHub App",
  campaigns: "Campaign sending",
  workbench: "Workbenches",
  localApps: "Local apps",
};

/**
 * Where a module is configured from the OS, when it is. Null means the
 * deployment: the Set up group names the variables instead of offering a
 * button.
 *
 * The ENGINE decides which through each lane's `configurableFrom`, and this
 * map only knows which Settings section to open for the ones that are. So a
 * module whose lane is `deployment` maps to null here and the group reads
 * the engine's answer, never this map, when deciding whether to offer an act.
 */
export const MODULE_SETTINGS_SECTION: Record<ModuleId, { section: string; name: string } | null> = {
  ai: { section: "providers", name: "AI providers" },
  email: { section: "integrations", name: "Integrations" },
  storage: null,
  githubApp: null,
  campaigns: null,
  workbench: null,
  localApps: null,
};

/**
 * The manifest descriptions, VERBATIM. The setup surface renders one per unmet
 * module, so this is the sentence a person reads when an app will not open --
 * it says what the module is for, which is the reason they would want it.
 *
 * Pinned to the engine's manifest by the same Go gate that pins the ids. Keep
 * each entry on ONE line with a double-quoted value: the gate parses it by
 * regexp, not by executing TypeScript.
 */
export const MODULE_DESCRIPTIONS: Record<ModuleId, string> = {
  ai: "Inference needs a provider: a machine on your fleet serving a model, or a federated cloud vendor.",
  storage: "Files, materialized outputs, deploy bundles and log archives live in blob storage.",
  email: "Sending mail needs a mailbox this cluster can send from.",
  githubApp: "Connecting a source through the GitHub App needs the app registered on the identity node.",
  campaigns: "Sending a campaign needs a one-click unsubscribe secret and a reachable unsubscribe address.",
  workbench: "Workbenches need a workbench node this agent can reach.",
  localApps: "Running a task in Claude Code or Codex on your machine needs the agent to mint a session credential.",
};
