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
