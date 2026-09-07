// The app + widget registries (spec H). Static: the foundation is not a
// plugin loader -- runtime app delivery is a later question, deliberately
// unanswered here. Manifests are data; the components they name mount
// inside WindowFrame / WidgetFrame.

import type { ComponentType } from "react";

import type { ModuleId } from "./modules";
import { isModuleId } from "./modules";
import type { RoleRequirement } from "./roles";
import { roleAdmits, roleRank } from "./roles";

export interface OsAppSection {
  id: string;
  name: string;
  roles?: RoleRequirement;
  /**
   * Modules this section cannot work without. Unmet, the section shows the
   * setup surface in place of its body -- a quiet "not set up yet" rather
   * than a wall of refusals from an engine that has nothing to answer with.
   */
  requires?: readonly ModuleId[];
  /**
   * Modules this section works without but is diminished by. Unmet, they
   * mark Settings and add a Set up row; they NEVER gate. The distinction is
   * the whole point: composing a deployable works with no storage, and only
   * publishing refuses, so gating the section would take away the part that
   * works.
   */
  wants?: readonly ModuleId[];
}

export interface OsAppProps {
  /** Current section id ("" when the app declares no sections). */
  sectionId: string;
  /** Navigate the window to another of the app's sections. */
  navigate: (sectionId: string) => void;
  /** Augment the window's Ask context ("app:<id>" is always present). */
  askContext: (tag: string) => void;
  /**
   * A standing open instruction (epic memql#4842, #4845): opaque payload the
   * opener handed to `openApp`, delivered whether this window is fresh or
   * was already open. An app that acts on one calls `consumeIntent` with the
   * SAME id -- consumption is id-matched so acting on a stale render can
   * never eat a newer instruction. Apps that ignore both props are
   * unaffected.
   */
  intent?: { id: string; payload: Record<string, unknown> };
  /** Optional so a harness constructing props by hand stays valid. */
  consumeIntent?: (intentId: string) => void;
}

export interface OsAppManifest {
  id: string;
  name: string;
  /** Lucide icon component (kept as a plain component type -- no coupling). */
  icon: ComponentType<{ size?: number | string; "aria-hidden"?: boolean }>;
  roles?: RoleRequirement;
  sections?: OsAppSection[];
  /**
   * Modules the WHOLE app cannot work without: every section but Settings
   * and Logs shows the setup surface. Those two stay reachable on purpose --
   * Settings is where the fix is, and Logs is how an admin sees why.
   */
  requires?: readonly ModuleId[];
  /** As the section verb, for the whole app. Marks and rows, never a gate. */
  wants?: readonly ModuleId[];
  /**
   * Section id the title-bar gear jumps to. REQUIRED on every app
   * (memql#4743): the owner's rule is that each app carries its own
   * settings, reachable the same way everywhere, so the gear is never a
   * button some windows happen not to have.
   *
   * It must name a section this manifest declares -- a gear pointing at an
   * id `sectionsForRole` will not return navigates the window nowhere, and
   * that failure is silent. `settingsSectionProblem` is the check;
   * `test/system/settingsContract.test.ts` runs it over the real registry.
   */
  settingsSection: string;
  /**
   * Section id that shows this app's slice of the cluster's logs. REQUIRED
   * on every app (epic memql#4895, spec H "The convention"), for the reason
   * `settingsSection` is: an admin follows a fault the same way in every
   * window, and a Logs section some apps happen not to have is a section
   * somebody looks for and cannot find.
   *
   * It must name a section this manifest declares, and that section must be
   * floored at admin -- on the section itself (`roles: { min: "admin" }`),
   * or on the whole app when the app's own floor is at or above it (the
   * Logs app names its Stream and carries the floor itself). Reads on the
   * log store are admin-and-above in the engine (spec L3); a section
   * offered below that floor would open on a refusal for everyone in it.
   * `logsSectionProblem` is the check; `test/logs/logsContract.test.ts`
   * runs it over the real registry.
   */
  logsSection: string;
  /**
   * A DOCK FIXTURE is always in the dock and cannot be taken out of it
   * (memql#4784). The Bin is the only one, and it is the reason the flag
   * exists rather than a general capability: a trash can that a person can
   * unpin is one they can lose, and then archiving becomes a thing with no
   * visible destination.
   *
   * A fixture is deliberately NOT a pin. Pins live in `DesktopStore` and roam
   * with the desktop; a fixture is a property of the SHELL, so it is here on
   * the manifest, it is never written to storage, and no upgrade path or
   * corrupt document can leave somebody without one. `dockOrder` excludes it
   * from the pin strip and the dock renders it in its own slot; the context
   * menu offers no pin or unpin for it, because neither would do anything.
   */
  dockFixture?: boolean;
  component: ComponentType<OsAppProps>;
}

export interface OsWidgetManifest {
  id: string;
  name: string;
  icon: ComponentType<{ size?: number | string; "aria-hidden"?: boolean }>;
  roles?: RoleRequirement;
  /**
   * Modules this widget cannot work without. A widget has no sections and no
   * settings of its own, so an unmet requirement renders the setup surface's
   * SENTENCE in its own body rather than the whole surface -- a desktop
   * widget is too small to carry a headline and an act.
   */
  requires?: readonly ModuleId[];
  /** Size in desktop grid cells. */
  size: { w: number; h: number };
  component: ComponentType;
}

export interface OsRegistry {
  apps: OsAppManifest[];
  widgets: OsWidgetManifest[];
}

export function appById(registry: OsRegistry, id: string): OsAppManifest | undefined {
  return registry.apps.find((a) => a.id === id);
}

/** The always-docked apps the actor may see, in registry order. */
export function fixturesForRole(registry: OsRegistry, actorRole: string): OsAppManifest[] {
  return registry.apps.filter((a) => a.dockFixture === true && roleAdmits(actorRole, a.roles));
}

/** Whether an app is a dock fixture -- what the pin menu and the pin strip
 *  both ask before offering anything. */
export function isDockFixture(registry: OsRegistry, appId: string): boolean {
  return appById(registry, appId)?.dockFixture === true;
}

export function widgetById(registry: OsRegistry, id: string): OsWidgetManifest | undefined {
  return registry.widgets.find((w) => w.id === id);
}

/** Apps the actor may see, in registry order. */
export function appsForRole(registry: OsRegistry, actorRole: string): OsAppManifest[] {
  return registry.apps.filter((a) => roleAdmits(actorRole, a.roles));
}

export function widgetsForRole(registry: OsRegistry, actorRole: string): OsWidgetManifest[] {
  return registry.widgets.filter((w) => roleAdmits(actorRole, w.roles));
}

/** Sections of an app the actor may open; the first is the default. */
export function sectionsForRole(app: OsAppManifest, actorRole: string): OsAppSection[] {
  return (app.sections ?? []).filter((s) => roleAdmits(actorRole, s.roles));
}

/**
 * The one launch admission check: an app the actor cannot see cannot be
 * opened by id either (dock, launcher and deep entry all go through this).
 */
export function canOpen(registry: OsRegistry, actorRole: string, appId: string): boolean {
  const app = appById(registry, appId);
  return !!app && roleAdmits(actorRole, app.roles);
}

/**
 * The settings-section contract, as a function rather than a test assertion
 * so the apps index can report the same defect it gates on.
 *
 * Returns null when the manifest is well-formed, otherwise the sentence to
 * show. It checks the DECLARED sections, never the role-admitted ones: a
 * gear target gated above the viewer is a legitimate manifest (the window
 * simply shows no gear for them), while a gear target that exists for
 * nobody is a bug in every session.
 */
export function settingsSectionProblem(app: OsAppManifest): string | null {
  const target = app.settingsSection.trim();
  if (target === "") return `${app.id}: settingsSection is empty`;
  const sections = app.sections ?? [];
  if (!sections.some((s) => s.id === target)) {
    const declared = sections.map((s) => s.id).join(", ") || "none";
    return `${app.id}: settingsSection "${target}" names no declared section (declared: ${declared})`;
  }
  return null;
}

/** The rank floor every log read carries in the engine (spec L3). */
export const LOGS_ROLE_FLOOR = "admin";

/**
 * Whether a requirement admits nobody below the logs floor.
 *
 * `{ min: "admin" }` is accepted by name, so the check holds before the
 * cluster's ladder has loaded; any other minimum is ranked against the floor
 * and an unrankable one FAILS -- a floor that cannot be resolved is not a
 * floor that admits everyone, which is the fail-closed reading `roleAdmits`
 * takes too. A set form must name only rungs at or above the floor.
 */
function flooredAtLogs(requirement?: RoleRequirement): boolean {
  if (!requirement) return false;
  const floor = roleRank(LOGS_ROLE_FLOOR);
  if ("any" in requirement) {
    if (requirement.any.length === 0 || floor < 0) return false;
    return requirement.any.every((slug) => roleRank(slug) >= floor);
  }
  if (requirement.min === LOGS_ROLE_FLOOR) return true;
  const rank = roleRank(requirement.min);
  return floor >= 0 && rank >= floor;
}

/**
 * The logs-section contract, the way `settingsSectionProblem` is: a
 * function rather than a test assertion, returning null when the manifest
 * is well-formed and otherwise the sentence to show.
 *
 * Over DECLARED sections, like its sibling. The floor is checked as well as
 * the existence, because a Logs section a writer can open is a section that
 * opens on the engine's refusal -- and "this app is broken" is what that
 * reads as from inside the window.
 */
export function logsSectionProblem(app: OsAppManifest): string | null {
  const target = app.logsSection.trim();
  if (target === "") return `${app.id}: logsSection is empty`;
  const sections = app.sections ?? [];
  const section = sections.find((s) => s.id === target);
  if (!section) {
    const declared = sections.map((s) => s.id).join(", ") || "none";
    return `${app.id}: logsSection "${target}" names no declared section (declared: ${declared})`;
  }
  if (!flooredAtLogs(section.roles) && !flooredAtLogs(app.roles)) {
    return `${app.id}: logsSection "${target}" is not floored at ${LOGS_ROLE_FLOOR} on the section or the app`;
  }
  return null;
}

/**
 * The readiness contract, as a function for the same reason `logsSectionProblem`
 * is one: so a test can run it over the SHIPPED registry rather than restating
 * the rule per app. (`settingsSectionProblem` goes further and is rendered by
 * the apps index; this one and the logs one are not, yet.)
 *
 * A requirement on the settings or logs section is REFUSED. Those two are the
 * exemptions the window frame keeps reachable while an app is unconfigured, so
 * a requirement there would lock a person out of the one place they can fix it
 * -- an app that gates its own repair.
 */
export function readinessProblem(app: OsAppManifest): string | null {
  const bad = (ids: readonly string[] | undefined, where: string): string | null => {
    for (const id of ids ?? []) {
      if (!isModuleId(id)) return `${app.id}: ${where} names unknown module "${id}"`;
    }
    return null;
  };
  const top = bad(app.requires, "requires") ?? bad(app.wants, "wants");
  if (top) return top;
  for (const section of app.sections ?? []) {
    const p =
      bad(section.requires, `section ${section.id} requires`) ??
      bad(section.wants, `section ${section.id} wants`);
    if (p) return p;
    const exempt = section.id === app.settingsSection || section.id === app.logsSection;
    if (exempt && ((section.requires?.length ?? 0) > 0 || (section.wants?.length ?? 0) > 0)) {
      return `${app.id}: section ${section.id} is the settings or logs section and cannot carry a requirement`;
    }
  }
  return null;
}

/**
 * The requirements that gate one section: the app's, then the section's own.
 *
 * Settings and Logs answer empty whatever the app declares, which is the same
 * exemption `readinessProblem` enforces at the declaration -- stated twice
 * because one is about what an author may write and the other about what the
 * frame does with it.
 */
export function requirementsFor(
  app: OsAppManifest,
  sectionId: string,
): { requires: ModuleId[]; wants: ModuleId[] } {
  if (sectionId === app.settingsSection || sectionId === app.logsSection) {
    return { requires: [], wants: [] };
  }
  const section = (app.sections ?? []).find((s) => s.id === sectionId);
  const dedupe = (ids: readonly ModuleId[]) => Array.from(new Set(ids));
  return {
    requires: dedupe([...(app.requires ?? []), ...(section?.requires ?? [])]),
    wants: dedupe([...(app.wants ?? []), ...(section?.wants ?? [])]),
  };
}

/**
 * Everything this app needs set up ANYWHERE in it: the app's own lists plus
 * every section's, minus the settings and logs exemption.
 *
 * This is what the MARK is computed from, and it is deliberately not
 * `requirementsFor`. That function answers "does this section render", so it
 * must fold the app's list into each section and nothing else -- putting a
 * section's own requirement on the manifest instead would gate every OTHER
 * section on it, which is how Fleet briefly required a local-app credential
 * to look at its machines. The mark asks a different question: is a person
 * needed anywhere in this app. Both readings are wanted; one function cannot
 * give both.
 */
export function allRequirementsFor(app: OsAppManifest): {
  requires: ModuleId[];
  wants: ModuleId[];
} {
  const requires = new Set<ModuleId>(app.requires ?? []);
  const wants = new Set<ModuleId>(app.wants ?? []);
  for (const section of app.sections ?? []) {
    if (section.id === app.settingsSection || section.id === app.logsSection) continue;
    for (const id of section.requires ?? []) requires.add(id);
    for (const id of section.wants ?? []) wants.add(id);
  }
  return { requires: Array.from(requires), wants: Array.from(wants) };
}
