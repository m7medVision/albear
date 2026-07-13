// Content script: detects login forms, fills after explicit user action, and
// offers to save submitted credentials. It never persists or broadcasts
// credentials and never trusts page-provided origin strings (PRD 13.2).
//
// Save flow (PRD 13.3): on a form submit or formless button click while a
// password field is filled, the candidate is stashed to the background
// SYNCHRONOUSLY off the event so a same-tab navigation away from the page
// (which a real login handler triggers immediately after submit) does not
// race ahead of the stash. The password is then wiped on the next tick so
// the page's own (synchronous) submit handler can still read the field. If
// the document is still around after the gate/match chain, the save bar is
// rendered into it; otherwise the next page load picks the capture back up
// via records.consumeCapture and offers the bar there.
import { detectLoginForms, type LoginForm } from './forms'
import {
  decideMode,
  renderSaveBar,
  type ExistingRecord,
} from './save-bar'

interface BgResponse<T> {
  ok: boolean
  data?: T
  error?: { code: string; message: string }
}

async function bg<T>(msg: Record<string, unknown>): Promise<T> {
  const resp = (await chrome.runtime.sendMessage(msg)) as BgResponse<T>
  if (!resp?.ok) throw new Error(resp?.error?.code ?? 'INTERNAL')
  return resp.data as T
}

// ---- fill (popup → content) -----------------------------------------------

interface FillRequest {
  kind: 'albear.fill'
  recordId: string
}

chrome.runtime.onMessage.addListener(
  (msg: FillRequest, _sender: chrome.runtime.MessageSender, sendResponse: (r: unknown) => void) => {
    if (msg?.kind !== 'albear.fill') return undefined
    fillSelected(msg.recordId)
      .then(() => sendResponse({ ok: true }))
      .catch((e: unknown) => sendResponse({ ok: false, error: String(e) }))
    return true
  },
)

async function fillSelected(recordId: string): Promise<void> {
  const forms = detectLoginForms(document)
  const target = forms[0]
  if (!target) throw new Error('no login form on this page')
  const secret = await bg<{ username: string; password: string }>({
    kind: 'records.revealForFill',
    id: recordId,
  })
  fillLogin(target, secret.username, secret.password)
  secret.password = ''
}

function fillLogin(form: LoginForm, username: string, password: string): void {
  if (form.username && username) fillField(form.username, username)
  fillField(form.password, password)
}

function fillField(input: HTMLInputElement, value: string): void {
  input.focus()
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  if (setter) setter.call(input, value)
  else input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
  input.blur()
}

// ---- capture (submit or formless click) -----------------------------------

interface Captured {
  username: HTMLInputElement | null
  password: HTMLInputElement
}

function onSubmit(ev: Event): void {
  const form = ev.target
  if (!(form instanceof HTMLFormElement)) return
  const forms = detectLoginForms(document)
  const match = forms.find((f) => f.form === form)
  if (!match || !match.password.value) return
  void capture(match)
}

function onClick(ev: MouseEvent): void {
  const target = ev.target
  if (!(target instanceof HTMLElement)) return
  // Only fire on real buttons; defer to the form-submit path if a form
  // wraps the inputs (the submit event will capture instead).
  if (target.tagName !== 'BUTTON') return
  if (target.closest('form')) return
  const found = findFilledForm()
  if (!found) return
  void capture(found)
}

function findFilledForm(): Captured | null {
  const forms = detectLoginForms(document)
  for (const f of forms) if (f.password.value) return f
  return null
}

function capture(c: Captured): void {
  const passwordValue = c.password.value
  if (!passwordValue) return
  const usernameValue = c.username?.value ?? ''
  // Stash synchronously: this MUST be in flight before we await anything
  // else, otherwise a fast same-tab navigation triggered by the page's own
  // submit handler could tear down the document before the round-trip.
  void bg({
    kind: 'records.stashCapture',
    username: usernameValue,
    password: passwordValue,
  })
  // Wipe on the next tick. The page's own (synchronous) submit/click
  // handler still gets to read the live value; we just don't leave it
  // sitting in the DOM after we've taken a copy.
  setTimeout(() => {
    c.password.value = ''
  }, 0)
  void offerBar({ username: usernameValue, password: passwordValue })
}

async function offerBar(candidate: { username: string; password: string }): Promise<void> {
  try {
    const st = await bg<{ paired?: boolean; unlocked?: boolean }>({ kind: 'status' })
    if (!st.paired || !st.unlocked) return
    const matches = await bg<ExistingRecord[]>({ kind: 'records.matchForTab' })
    if (document.getElementById('albear-save-bar')) return
    const decision = decideMode(matches, candidate.username)
    renderSaveBar({
      mode: decision.mode,
      existing: decision.existing,
      candidate,
      callbacks: {
        onSave: async (c) => {
          await bg({ kind: 'records.saveLogin', username: c.username, password: c.password })
          await bg({ kind: 'records.clearCapture' })
        },
        onUpdate: async (e, c) => {
          await bg({
            kind: 'records.updateLogin',
            id: e.id,
            expectedRevision: e.revision,
            username: c.username,
            password: c.password,
          })
          await bg({ kind: 'records.clearCapture' })
        },
        onSaveNew: async (c) => {
          await bg({ kind: 'records.saveLogin', username: c.username, password: c.password })
          await bg({ kind: 'records.clearCapture' })
        },
      },
    })
  } catch {
    // background not reachable; nothing to render.
  }
}

// ---- resume a stashed capture from the previous page ----------------------

async function resumeStash(): Promise<void> {
  let candidate: { username: string; password: string } | null = null
  try {
    candidate = await bg<{ username: string; password: string } | null>({
      kind: 'records.consumeCapture',
    })
  } catch {
    return
  }
  if (!candidate) return
  try {
    const st = await bg<{ paired?: boolean; unlocked?: boolean }>({ kind: 'status' })
    if (!st.paired || !st.unlocked) return
    const matches = await bg<ExistingRecord[]>({ kind: 'records.matchForTab' })
    if (document.getElementById('albear-save-bar')) return
    const decision = decideMode(matches, candidate.username)
    renderSaveBar({
      mode: decision.mode,
      existing: decision.existing,
      candidate,
      callbacks: {
        onSave: async (c) => {
          await bg({ kind: 'records.saveLogin', username: c.username, password: c.password })
          await bg({ kind: 'records.clearCapture' })
        },
        onUpdate: async (e, c) => {
          await bg({
            kind: 'records.updateLogin',
            id: e.id,
            expectedRevision: e.revision,
            username: c.username,
            password: c.password,
          })
          await bg({ kind: 'records.clearCapture' })
        },
        onSaveNew: async (c) => {
          await bg({ kind: 'records.saveLogin', username: c.username, password: c.password })
          await bg({ kind: 'records.clearCapture' })
        },
      },
    })
  } catch {
    // ignore
  }
}

document.addEventListener('submit', onSubmit, true)
document.addEventListener('click', onClick, true)
void resumeStash()
