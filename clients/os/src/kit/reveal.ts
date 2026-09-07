// BRING A REGION INTO VIEW AND PUT THE CURSOR IN IT (epic memql#5106).
//
// An intent that opens another app at a section leaves a person looking at
// the top of a long page with the thing they asked for somewhere below it --
// which is a handoff that stops one step short of the act. This is the last
// step: scroll the region to the top of its scroller and focus its first
// control, so a keyboard reader lands where a mouse reader is looking.
//
// FOCUS RATHER THAN A HIGHLIGHT. A tint that fades is a decoration a screen
// reader cannot see, and focus is the one signal every reader shares -- it
// says "you are here" and it is where typing will go, which for a form is
// the same answer.

/** Whether this reader has asked for less movement. */
function prefersReducedMotion(): boolean {
  return globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
}

/**
 * The region tagged `data-<attribute>="<value>"`, found by COMPARING rather
 * than by building a selector out of the value.
 *
 * The value arrives in a window intent, which is a payload another surface
 * wrote -- so interpolating it into a selector would let it become a
 * different selector. `CSS.escape` would answer that, and does not exist in
 * every environment this runs in (jsdom has no `CSS` at all), so a comparison
 * is both safer and more portable than the thing that would make the
 * interpolation safe.
 */
export function findRegion(attribute: string, value: string, root: ParentNode = document): Element | null {
  if (value === "") return null;
  for (const el of root.querySelectorAll(`[data-${attribute}]`)) {
    if (el.getAttribute(`data-${attribute}`) === value) return el;
  }
  return null;
}

export function revealRegion(el: Element | null | undefined): void {
  if (!el) return;
  // jsdom implements neither, and a test that renders this must not throw on
  // a browser API it does not have. Both are progressive: a reveal that
  // cannot scroll still focuses, and one that cannot focus still scrolls.
  if (typeof (el as HTMLElement).scrollIntoView === "function") {
    (el as HTMLElement).scrollIntoView({
      block: "start",
      behavior: prefersReducedMotion() ? "auto" : "smooth",
    });
  }
  const control = el.querySelector<HTMLElement>(
    "input:not([type=hidden]), select, textarea, button, [tabindex]:not([tabindex='-1'])",
  );
  control?.focus?.({ preventScroll: true });
}
