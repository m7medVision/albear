// Save bar controller: in-page offer shown after a login attempt. Renders
// one of two modes based on whether a record already exists for this origin:
//   - "save"     : 0 matches → [Save] [Dismiss]
//   - "update"   : ≥1 match → [Update] [Save as new] [Dismiss]
//
// The bar is sticky (no auto-dismiss) so the user can act at their pace.
// Errors from the save RPC are surfaced in the bar's status line, never
// swallowed, as a sentence that says how to recover — never a raw code. The
// product's dark-theme tokens are inlined as literal values so the bar renders
// the same on any host page regardless of the host's CSS.
export interface ExistingRecord {
  id: string
  revision: number
  name: string
  username: string
  // Carried through unchanged on update — the save bar never sets or clears
  // a Project ID, only a new record's creation does.
  projectId?: string
}

export interface BarCallbacks {
  onSave: (candidate: { username: string; password: string }) => Promise<void> | void
  onUpdate: (
    existing: ExistingRecord,
    candidate: { username: string; password: string },
  ) => Promise<void> | void
  onSaveNew: (candidate: { username: string; password: string }) => Promise<void> | void
}

export interface RenderOpts {
  mode: 'save' | 'update'
  existing: ExistingRecord | null
  candidate: { username: string; password: string }
  callbacks: BarCallbacks
}

export interface SaveBar {
  el: HTMLDivElement
  setStatus(text: string): void
  remove(): void
}

const BAR_ID = 'albear-save-bar'

// The bar lives on third-party pages where we cannot rely on the host defining
// our --background etc., so these are literal copies of the popup's dark-theme
// tokens (src/styles/popup.css) — keep them in step. Inline styles only: the
// host page's CSP may block a <style> element but never CSSOM writes.
const T = {
  bg: 'oklch(0.225 0.03 251)', // --card
  fg: 'oklch(0.96 0.008 250)', // --card-foreground
  mutedFg: 'oklch(0.73 0.02 250)', // --muted-foreground
  border: 'oklch(1 0 0 / 11%)', // --border
  primaryBg: 'oklch(0.72 0.13 224)', // --primary
  primaryFg: 'oklch(0.2 0.04 245)', // --primary-foreground
  secondaryBg: 'oklch(0.28 0.03 252)', // --secondary
  secondaryFg: 'oklch(0.95 0.008 250)', // --secondary-foreground
  destructive: 'oklch(0.68 0.18 25)', // --destructive
} as const

const BUTTON_BASE =
  'border:0;border-radius:6px;padding:5px 12px;cursor:pointer;white-space:nowrap;' +
  'font:500 13px ui-sans-serif,system-ui,sans-serif;'

// Error codes arrive as Error(code) from the background. Each message names
// what failed and what to do next.
const SAVE_ERRORS: Record<string, string> = {
  VAULT_LOCKED: 'Unable to save: albear is locked. Unlock it from the toolbar, then try again.',
  DISCONNECTED: 'Unable to save: vaultd isn’t reachable. Start it, then try again.',
  RELAY: 'Unable to save: vaultd isn’t reachable. Start it, then try again.',
  CONFLICT: 'Unable to update: this login changed somewhere else. Select Save as new instead.',
  ALREADY_EXISTS: 'Unable to save: this login already exists. Add it from the albear popup with another name.',
  DENIED: 'Unable to save from this page. Add the login from the albear popup instead.',
}

export function saveErrorText(e: unknown): string {
  const code = (e instanceof Error ? e.message : String(e)).replace(/^Error:\s*/, '')
  return SAVE_ERRORS[code] ?? 'Unable to save. Try again, or add the login from the albear popup.'
}

function makeButton(text: string, variant: 'primary' | 'secondary'): HTMLButtonElement {
  const b = document.createElement('button')
  b.type = 'button'
  b.textContent = text
  const bg = variant === 'primary' ? T.primaryBg : T.secondaryBg
  const fg = variant === 'primary' ? T.primaryFg : T.secondaryFg
  b.style.cssText = `background:${bg};color:${fg};${BUTTON_BASE}`
  return b
}

// Only genuine user input may drive the bar. Every control here either stores
// a credential or overwrites an existing one, so a synthetic click — which any
// page can dispatch at an element it can reach, and which `el.click()` also
// produces — must do nothing. The closed shadow root should already keep these
// buttons out of a page's reach; this is the second lock on the same door.
function onUserClick(el: HTMLElement, fn: () => void): void {
  el.addEventListener('click', (event: MouseEvent) => {
    if (!event.isTrusted) return
    fn()
  })
}

// A live region that exists (empty) before any error lands in it, so the
// error is announced reliably. It takes no space until it has text.
function makeStatus(): HTMLDivElement {
  const s = document.createElement('div')
  s.setAttribute('role', 'alert')
  s.style.cssText = `font-size:12px;line-height:1.4;color:${T.destructive};overflow-wrap:anywhere`
  return s
}

function setStatusText(status: HTMLElement, text: string): void {
  status.textContent = text
  status.style.marginTop = text ? '8px' : '0'
}

function makeLabel(): HTMLDivElement {
  const s = document.createElement('div')
  s.style.cssText = 'display:flex;flex-direction:column;gap:2px;line-height:1.4;min-width:0;overflow-wrap:anywhere'
  return s
}

// Buttons sit on their own wrapping row under the label, so neither the label
// nor a long username gets crushed by three buttons on a narrow viewport.
function makeActions(...buttons: HTMLButtonElement[]): HTMLDivElement {
  const row = document.createElement('div')
  row.style.cssText = 'display:flex;flex-wrap:wrap;gap:8px;margin-top:10px'
  row.append(...buttons)
  return row
}

function renderUpdate(
  opts: RenderOpts,
  bar: HTMLDivElement,
  status: HTMLDivElement,
  destroy: () => void,
): void {
  const ex = opts.existing!
  const label = makeLabel()
  const name = document.createElement('strong')
  name.textContent = ex.name
  name.style.cssText = 'font-size:13px;font-weight:600'
  const sub = document.createElement('span')
  sub.textContent = ex.username ? `Saved as ${ex.username}` : 'Saved without a username'
  sub.style.cssText = `font-size:12px;color:${T.mutedFg}`
  label.append(name, sub)

  const update = makeButton('Update', 'primary')
  const saveNew = makeButton('Save as new', 'secondary')
  const dismiss = makeButton('Dismiss', 'secondary')
  bar.append(label, makeActions(update, saveNew, dismiss))

  const run = async (cb: () => Promise<void> | void): Promise<void> => {
    update.disabled = true
    saveNew.disabled = true
    dismiss.disabled = true
    setStatusText(status, '')
    try {
      await cb()
      destroy()
    } catch (e) {
      setStatusText(status, saveErrorText(e))
      update.disabled = false
      saveNew.disabled = false
      dismiss.disabled = false
    }
  }

  onUserClick(update, () => {
    void run(() => opts.callbacks.onUpdate(ex, opts.candidate))
  })
  onUserClick(saveNew, () => {
    void run(() => opts.callbacks.onSaveNew(opts.candidate))
  })
  onUserClick(dismiss, destroy)
}

function renderSave(
  opts: RenderOpts,
  bar: HTMLDivElement,
  status: HTMLDivElement,
  destroy: () => void,
): void {
  const label = makeLabel()
  const prompt = document.createElement('span')
  prompt.textContent = opts.candidate.username
    ? `Save ${opts.candidate.username} to albear?`
    : 'Save login to albear?'
  prompt.style.cssText = 'font-size:13px'
  label.append(prompt)

  const save = makeButton('Save', 'primary')
  const dismiss = makeButton('Dismiss', 'secondary')
  bar.append(label, makeActions(save, dismiss))

  const run = async (cb: () => Promise<void> | void): Promise<void> => {
    save.disabled = true
    dismiss.disabled = true
    setStatusText(status, '')
    try {
      await cb()
      destroy()
    } catch (e) {
      setStatusText(status, saveErrorText(e))
      save.disabled = false
      dismiss.disabled = false
    }
  }

  onUserClick(save, () => {
    void run(() => opts.callbacks.onSave(opts.candidate))
  })
  onUserClick(dismiss, destroy)
}

export function renderSaveBar(opts: RenderOpts): SaveBar {
  document.getElementById(BAR_ID)?.remove()

  // The host is a bare anchor in the page; everything real lives in a closed
  // shadow root hanging off it. Closed rather than open, because with an open
  // root the page walks host.shadowRoot straight to our controls: it could
  // read the candidate username out of the DOM, or drive the buttons. Closed
  // leaves host.shadowRoot null for page script.
  //
  // `all:initial` is the other half of the isolation: it stops host-page CSS
  // from reaching the host and hiding or repositioning the bar under
  // something else.
  const host = document.createElement('div')
  host.id = BAR_ID
  host.style.cssText = 'all:initial'
  const root = host.attachShadow({ mode: 'closed' })

  const bar = document.createElement('div')
  // A non-modal dialog: it never steals focus from the page's own flow, and
  // Escape dismisses it once the user has tabbed into it.
  bar.setAttribute('role', 'dialog')
  bar.setAttribute('aria-label', 'Save login to albear')
  bar.style.cssText =
    `position:fixed;top:12px;right:12px;z-index:2147483647;box-sizing:border-box;` +
    `background:${T.bg};color:${T.fg};border:1px solid ${T.border};` +
    `border-radius:8px;font:13px ui-sans-serif,system-ui,sans-serif;` +
    `box-shadow:0 8px 24px rgba(0,0,0,0.4);` +
    `display:flex;flex-direction:column;width:max-content;` +
    `max-width:min(360px, calc(100vw - 24px));padding:10px 14px`

  const status = makeStatus()
  // Removing the host takes the shadow root and the bar with it; removing the
  // bar alone would leave an orphaned host in the page.
  const destroy = (): void => host.remove()
  bar.addEventListener('keydown', (event: KeyboardEvent) => {
    if (event.isTrusted && event.key === 'Escape') destroy()
  })

  if (opts.mode === 'update' && opts.existing) {
    renderUpdate(opts, bar, status, destroy)
  } else {
    renderSave(opts, bar, status, destroy)
  }

  bar.append(status)
  root.appendChild(bar)
  document.documentElement.appendChild(host)

  return {
    el: bar,
    setStatus(text) {
      setStatusText(status, text)
    },
    remove: destroy,
  }
}

// Match-based mode decision. Pure function so the content-script wiring and
// the unit tests can both depend on it without a DOM.
export function decideMode(
  matches: ExistingRecord[],
  candidateUsername: string,
): { mode: 'save' | 'update'; existing: ExistingRecord | null } {
  const same = matches.find((m) => m.username === candidateUsername) ?? null
  if (same) return { mode: 'update', existing: same }
  if (matches.length > 0) return { mode: 'update', existing: matches[0]! }
  return { mode: 'save', existing: null }
}
