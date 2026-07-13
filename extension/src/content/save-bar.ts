// Save bar controller: in-page offer shown after a login attempt. Renders
// one of two modes based on whether a record already exists for this origin:
//   - "save"     : 0 matches → [Save] [Dismiss]
//   - "update"   : ≥1 match → [Update] [Save as new] [Dismiss]
//
// The bar is sticky (no auto-dismiss) so the user can act at their pace.
// Errors from the save RPC are surfaced in the bar's status line, never
// swallowed. Shadcn-style tokens (dark) are inlined as literal values so the
// bar renders the same on any host page regardless of the host's CSS.
export interface ExistingRecord {
  id: string
  revision: number
  name: string
  username: string
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

// ponytail: hardcoded shadcn dark tokens. The bar lives on third-party
// pages where we cannot rely on the host defining our --background etc.
const T = {
  bg: '#1f1f1f',
  fg: '#fafafa',
  border: 'rgba(255, 255, 255, 0.12)',
  primaryBg: '#e5e5e5',
  primaryFg: '#1f1f1f',
  secondaryBg: '#3a3a3a',
  secondaryFg: '#fafafa',
  destructive: '#ff6b6b',
} as const

const BUTTON_BASE =
  'border:0;border-radius:6px;padding:5px 12px;cursor:pointer;' +
  'font:500 13px ui-sans-serif,system-ui,sans-serif;transition:background 120ms ease;'

function makeButton(text: string, variant: 'primary' | 'secondary'): HTMLButtonElement {
  const b = document.createElement('button')
  b.type = 'button'
  b.textContent = text
  const bg = variant === 'primary' ? T.primaryBg : T.secondaryBg
  const fg = variant === 'primary' ? T.primaryFg : T.secondaryFg
  b.style.cssText = `background:${bg};color:${fg};${BUTTON_BASE}`
  return b
}

function makeStatus(): HTMLSpanElement {
  const s = document.createElement('span')
  s.style.cssText = `font-size:11px;color:${T.destructive};margin-left:auto;min-height:14px`
  return s
}

function makeLabel(): HTMLSpanElement {
  const s = document.createElement('span')
  s.style.cssText = 'display:flex;flex-direction:column;line-height:1.3;flex:1;min-width:0'
  return s
}

function renderUpdate(opts: RenderOpts, bar: HTMLDivElement, status: HTMLSpanElement): void {
  const ex = opts.existing!
  const label = makeLabel()
  const name = document.createElement('strong')
  name.textContent = ex.name
  name.style.cssText = 'font-size:13px;font-weight:600'
  const sub = document.createElement('span')
  sub.textContent = `Saved as ${ex.username || '(no username)'}`
  sub.style.cssText = 'font-size:11px;opacity:.75'
  label.append(name, sub)

  const update = makeButton('Update', 'primary')
  const saveNew = makeButton('Save as new', 'secondary')
  const dismiss = makeButton('Dismiss', 'secondary')
  bar.append(label, update, saveNew, dismiss)

  const run = async (cb: () => Promise<void> | void): Promise<void> => {
    update.disabled = true
    saveNew.disabled = true
    dismiss.disabled = true
    status.textContent = ''
    try {
      await cb()
      bar.remove()
    } catch (e) {
      status.textContent = e instanceof Error ? e.message : String(e)
      update.disabled = false
      saveNew.disabled = false
      dismiss.disabled = false
    }
  }

  update.addEventListener('click', () => {
    void run(() => opts.callbacks.onUpdate(ex, opts.candidate))
  })
  saveNew.addEventListener('click', () => {
    void run(() => opts.callbacks.onSaveNew(opts.candidate))
  })
  dismiss.addEventListener('click', () => {
    bar.remove()
  })
}

function renderSave(opts: RenderOpts, bar: HTMLDivElement, status: HTMLSpanElement): void {
  const label = makeLabel()
  const prompt = document.createElement('span')
  prompt.textContent = opts.candidate.username
    ? `Save ${opts.candidate.username} to albear?`
    : 'Save login to albear?'
  prompt.style.cssText = 'font-size:13px'
  label.append(prompt)

  const save = makeButton('Save', 'primary')
  const dismiss = makeButton('Dismiss', 'secondary')
  bar.append(label, save, dismiss)

  const run = async (cb: () => Promise<void> | void): Promise<void> => {
    save.disabled = true
    dismiss.disabled = true
    status.textContent = ''
    try {
      await cb()
      bar.remove()
    } catch (e) {
      status.textContent = e instanceof Error ? e.message : String(e)
      save.disabled = false
      dismiss.disabled = false
    }
  }

  save.addEventListener('click', () => {
    void run(() => opts.callbacks.onSave(opts.candidate))
  })
  dismiss.addEventListener('click', () => {
    bar.remove()
  })
}

export function renderSaveBar(opts: RenderOpts): SaveBar {
  document.getElementById(BAR_ID)?.remove()

  const bar = document.createElement('div')
  bar.id = BAR_ID
  bar.setAttribute('role', 'alertdialog')
  bar.setAttribute('aria-label', 'Save login to albear')
  bar.style.cssText =
    `position:fixed;top:12px;right:12px;z-index:2147483647;` +
    `background:${T.bg};color:${T.fg};border:1px solid ${T.border};` +
    `border-radius:8px;font:13px ui-sans-serif,system-ui,sans-serif;` +
    `box-shadow:0 8px 24px rgba(0,0,0,0.4);` +
    `display:flex;gap:10px;align-items:center;max-width:360px;padding:10px 14px`

  const status = makeStatus()

  if (opts.mode === 'update' && opts.existing) {
    renderUpdate(opts, bar, status)
  } else {
    renderSave(opts, bar, status)
  }

  bar.append(status)
  document.documentElement.appendChild(bar)

  return {
    el: bar,
    setStatus(text) {
      status.textContent = text
    },
    remove() {
      bar.remove()
    },
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
