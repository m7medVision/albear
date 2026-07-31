// Project-tag extraction: pure DOM logic, unit-testable. Reads the developer-
// injected `albear-id` marker a local dev page uses to identify itself,
// independent of which port it happens to be running on.
//
// This value is page content like any other — the daemon never trusts it by
// itself; it's only ever used to ask the daemon "does anything match this
// id", and the daemon re-derives loopback-ness and re-checks the id itself
// before authorizing anything (PRD 13.2, invariant 9's loopback carve-out).

/** Reads the first `albear-id` attribute found anywhere in the page. */
export function readProjectTag(root: Document): string | null {
  const el = root.querySelector('[albear-id]')
  if (!el) return null
  const value = el.getAttribute('albear-id')?.trim()
  return value ? value : null
}
