// Save-bar controller + content-script save flow. The bar is the in-page
// offer rendered after a form submit; the content script decides which mode
// (save / update vs save-as-new) and gates on the vault being unlocked.
// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { detectLoginForms } from '../src/content/forms'
import {
  decideMode,
  renderSaveBar,
  type ExistingRecord,
  type RenderOpts,
} from '../src/content/save-bar'

// ---- helpers ---------------------------------------------------------------

function setBody(html: string): void {
  document.body.innerHTML = html
  Element.prototype.getClientRects = function () {
    return [{ width: 1, height: 1 }] as unknown as DOMRectList
  }
}

function dispatchSubmit(form: HTMLFormElement): void {
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
}

const candidate = { username: 'alice@example.com', password: 'hunter2' }

function optsFor(
  mode: 'save' | 'update',
  existing: ExistingRecord | null,
  cb: RenderOpts['callbacks'],
): RenderOpts {
  return { mode, existing, candidate, callbacks: cb }
}

// ---- decideMode ------------------------------------------------------------

describe('decideMode', () => {
  it('returns save when no existing records', () => {
    expect(decideMode([], 'alice')).toEqual({ mode: 'save', existing: null })
  })

  it('returns update with the matching username record', () => {
    const rec: ExistingRecord = {
      id: 'r1',
      revision: 3,
      name: 'Example',
      username: 'alice',
    }
    expect(decideMode([rec], 'alice')).toEqual({ mode: 'update', existing: rec })
  })

  it('returns update with the first record when usernames differ', () => {
    const a: ExistingRecord = { id: 'r1', revision: 1, name: 'Work', username: 'alice' }
    const b: ExistingRecord = { id: 'r2', revision: 2, name: 'Personal', username: 'bob' }
    expect(decideMode([a, b], 'carol')).toEqual({ mode: 'update', existing: a })
  })
})

// ---- renderSaveBar (save mode) --------------------------------------------

describe('renderSaveBar (save mode)', () => {
  it('renders Save and Dismiss buttons', () => {
    const opts = optsFor('save', null, {
      onSave: vi.fn(),
      onUpdate: vi.fn(),
      onSaveNew: vi.fn(),
    })
    const bar = renderSaveBar(opts)
    const buttons = Array.from(bar.el.querySelectorAll('button')).map((b) => b.textContent)
    expect(buttons).toEqual(['Save', 'Dismiss'])
    bar.remove()
  })

  it('Save click calls onSave with the candidate and removes the bar', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const opts = optsFor('save', null, {
      onSave,
      onUpdate: vi.fn(),
      onSaveNew: vi.fn(),
    })
    const bar = renderSaveBar(opts)
    const [save] = bar.el.querySelectorAll<HTMLButtonElement>('button')
    save!.click()
    await vi.waitFor(() => expect(bar.el.isConnected).toBe(false))
    expect(onSave).toHaveBeenCalledWith(candidate)
  })

  it('Save failure surfaces in the status line and keeps the bar visible', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('VAULT_LOCKED'))
    const opts = optsFor('save', null, {
      onSave,
      onUpdate: vi.fn(),
      onSaveNew: vi.fn(),
    })
    const bar = renderSaveBar(opts)
    const [save] = bar.el.querySelectorAll<HTMLButtonElement>('button')
    save!.click()
    await vi.waitFor(() => expect(bar.el.isConnected).toBe(true))
    expect(bar.el.textContent).toContain('VAULT_LOCKED')
    bar.remove()
  })

  it('Dismiss removes the bar without calling any save callback', () => {
    const onSave = vi.fn()
    const opts = optsFor('save', null, { onSave, onUpdate: vi.fn(), onSaveNew: vi.fn() })
    const bar = renderSaveBar(opts)
    const buttons = bar.el.querySelectorAll<HTMLButtonElement>('button')
    buttons[1]!.click()
    expect(bar.el.isConnected).toBe(false)
    expect(onSave).not.toHaveBeenCalled()
  })
})

// ---- renderSaveBar (update mode) ------------------------------------------

describe('renderSaveBar (update mode)', () => {
  const existing: ExistingRecord = {
    id: 'r1',
    revision: 7,
    name: 'Example',
    username: 'alice@example.com',
  }

  it('renders Update, Save as new, and Dismiss', () => {
    const opts = optsFor('update', existing, {
      onSave: vi.fn(),
      onUpdate: vi.fn(),
      onSaveNew: vi.fn(),
    })
    const bar = renderSaveBar(opts)
    const buttons = Array.from(bar.el.querySelectorAll('button')).map((b) => b.textContent)
    expect(buttons).toEqual(['Update', 'Save as new', 'Dismiss'])
    expect(bar.el.textContent).toContain('Example')
    expect(bar.el.textContent).toContain('alice@example.com')
    bar.remove()
  })

  it('Update click calls onUpdate with the existing record and candidate', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    const opts = optsFor('update', existing, {
      onSave: vi.fn(),
      onUpdate,
      onSaveNew: vi.fn(),
    })
    const bar = renderSaveBar(opts)
    const [update] = bar.el.querySelectorAll<HTMLButtonElement>('button')
    update!.click()
    await vi.waitFor(() => expect(bar.el.isConnected).toBe(false))
    expect(onUpdate).toHaveBeenCalledWith(existing, candidate)
  })

  it('Save as new click calls onSaveNew', async () => {
    const onSaveNew = vi.fn().mockResolvedValue(undefined)
    const opts = optsFor('update', existing, {
      onSave: vi.fn(),
      onUpdate: vi.fn(),
      onSaveNew,
    })
    const bar = renderSaveBar(opts)
    const [, saveNew] = bar.el.querySelectorAll<HTMLButtonElement>('button')
    saveNew!.click()
    await vi.waitFor(() => expect(bar.el.isConnected).toBe(false))
    expect(onSaveNew).toHaveBeenCalledWith(candidate)
  })
})

// ---- content script integration -------------------------------------------

type StubMessage = (msg: Record<string, unknown>) => Promise<unknown>

interface StubState {
  calls: Record<string, unknown>[]
  reply: (msg: Record<string, unknown>) => unknown
}

function installChrome(state: StubState): void {
  ;(globalThis as unknown as { chrome: unknown }).chrome = {
    runtime: {
      sendMessage: (msg: Record<string, unknown>) => {
        state.calls.push(msg)
        return Promise.resolve(state.reply(msg))
      },
      onMessage: { addListener: () => undefined },
    },
  }
}

function uninstallChrome(): void {
  delete (globalThis as unknown as { chrome?: unknown }).chrome
  document.documentElement.innerHTML = ''
  document.getElementById('albear-save-bar')?.remove()
}

async function loadContentScript(): Promise<void> {
  vi.resetModules()
  await import('../src/content/index')
}

describe('content script: save flow on submit', () => {
  let state: StubState

  beforeEach(() => {
    state = { calls: [], reply: () => ({ ok: true, data: null }) }
  })

  afterEach(() => {
    uninstallChrome()
  })

  it('does not show the bar when the vault is locked', async () => {
    setBody(`
      <form>
        <input type="text" name="user" value="alice" />
        <input type="password" name="pass" value="hunter2" />
      </form>`)
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: false } }
      return { ok: true, data: null }
    }
    installChrome(state)
    await loadContentScript()
    dispatchSubmit(document.querySelector('form')!)
    await new Promise((r) => setTimeout(r, 10))
    expect(document.getElementById('albear-save-bar')).toBeNull()
  })

  it('does not show the bar when the extension is unpaired', async () => {
    setBody(`
      <form>
        <input type="text" name="user" value="alice" />
        <input type="password" name="pass" value="hunter2" />
      </form>`)
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: false } }
      return { ok: true, data: null }
    }
    installChrome(state)
    await loadContentScript()
    dispatchSubmit(document.querySelector('form')!)
    await new Promise((r) => setTimeout(r, 10))
    expect(document.getElementById('albear-save-bar')).toBeNull()
  })

  it('shows the save bar on submit when no match exists, then calls records.saveLogin', async () => {
    setBody(`
      <form>
        <input type="text" name="user" value="alice" />
        <input type="password" name="pass" value="hunter2" />
      </form>`)
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') return { ok: true, data: [] }
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      return { ok: true, data: { id: 'new' } }
    }
    installChrome(state)
    await loadContentScript()
    dispatchSubmit(document.querySelector('form')!)
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).not.toBeNull())
    const buttons = Array.from(
      document.querySelectorAll('#albear-save-bar button'),
    ) as HTMLButtonElement[]
    expect(buttons.map((b) => b.textContent)).toEqual(['Save', 'Dismiss'])
    buttons[0]!.click()
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).toBeNull())
    const saveCall = state.calls.find((m) => m['kind'] === 'records.saveLogin')
    expect(saveCall).toBeDefined()
    expect(saveCall!['username']).toBe('alice')
    expect(saveCall!['password']).toBe('hunter2')
    // Clicking Save clears any stash so a subsequent navigation doesn't re-offer it.
    await vi.waitFor(() => expect(state.calls.some((m) => m['kind'] === 'records.clearCapture')).toBe(true))
  })

  it('stashes the capture immediately on submit, before the vault gate resolves', async () => {
    // Regression test: sites often navigate away the instant submit fires,
    // tearing down this document before the async gate/match/render chain
    // finishes. The stash must be dispatched synchronously on capture, not
    // after the gate check, or a fast navigation could race it out.
    setBody(`
      <form>
        <input type="text" name="user" value="alice" />
        <input type="password" name="pass" value="hunter2" />
      </form>`)
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') return { ok: true, data: [] }
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      return { ok: true, data: { id: 'new' } }
    }
    installChrome(state)
    await loadContentScript()
    dispatchSubmit(document.querySelector('form')!)
    // The stash call must already be in flight synchronously off the
    // submit event, well before the bar has had a chance to render.
    expect(state.calls.some((m) => m['kind'] === 'records.stashCapture')).toBe(true)
    const stashCall = state.calls.find((m) => m['kind'] === 'records.stashCapture')
    expect(stashCall!['username']).toBe('alice')
    expect(stashCall!['password']).toBe('hunter2')
  })

  it('shows the update bar when a record with the same username exists, then calls records.updateLogin', async () => {
    setBody(`
      <form>
        <input type="text" name="user" value="alice" />
        <input type="password" name="pass" value="hunter3" />
      </form>`)
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') {
        return {
          ok: true,
          data: [{ id: 'r1', revision: 4, name: 'Example', username: 'alice' }],
        }
      }
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      return { ok: true, data: { id: 'r1' } }
    }
    installChrome(state)
    await loadContentScript()
    dispatchSubmit(document.querySelector('form')!)
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).not.toBeNull())
    const buttons = Array.from(
      document.querySelectorAll('#albear-save-bar button'),
    ) as HTMLButtonElement[]
    expect(buttons.map((b) => b.textContent)).toEqual(['Update', 'Save as new', 'Dismiss'])
    buttons[0]!.click()
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).toBeNull())
    const updateCall = state.calls.find((m) => m['kind'] === 'records.updateLogin')
    expect(updateCall).toBeDefined()
    expect(updateCall!['id']).toBe('r1')
    expect(updateCall!['expectedRevision']).toBe(4)
    expect(updateCall!['username']).toBe('alice')
    expect(updateCall!['password']).toBe('hunter3')
  })

  it('surfaces save failure in the bar status line', async () => {
    setBody(`
      <form>
        <input type="text" name="user" value="alice" />
        <input type="password" name="pass" value="hunter2" />
      </form>`)
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') return { ok: true, data: [] }
      if (msg['kind'] === 'records.saveLogin')
        return { ok: false, error: { code: 'VAULT_LOCKED', message: 'VAULT_LOCKED' } }
      return { ok: true, data: null }
    }
    installChrome(state)
    await loadContentScript()
    dispatchSubmit(document.querySelector('form')!)
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).not.toBeNull())
    const [save] = Array.from(
      document.querySelectorAll('#albear-save-bar button'),
    ) as HTMLButtonElement[]
    save!.click()
    await vi.waitFor(() =>
      expect(document.getElementById('albear-save-bar')!.textContent).toContain('VAULT_LOCKED'),
    )
  })

  it('detects the form via the same detectLoginForms path the popup uses', () => {
    setBody(`
      <form>
        <input type="email" name="email" value="alice@x.com" />
        <input type="password" name="pwd" value="hunter2" />
      </form>`)
    const forms = detectLoginForms(document)
    expect(forms).toHaveLength(1)
    expect(forms[0]!.username?.value).toBe('alice@x.com')
    expect(forms[0]!.password.value).toBe('hunter2')
  })
})

// ---- content script: formless login (button click) ------------------------

describe('content script: formless login pattern', () => {
  let state: StubState

  beforeEach(() => {
    state = { calls: [], reply: () => ({ ok: true, data: null }) }
  })

  afterEach(() => {
    uninstallChrome()
  })

  function loadPracticeTestSite(): void {
    // Mirrors https://practicetestautomation.com/practice-test-login/: inputs
    // in <div>s, no <form> element, a plain <button> wired with onclick.
    setBody(`
      <div>
        <label for="username">Username</label>
        <input type="text" name="username" id="username" value="student" />
      </div>
      <div>
        <label for="password">Password</label>
        <input type="password" name="password" id="password" value="Password123" />
      </div>
      <button id="submit" class="btn">Submit</button>`)
  }

  it('shows the bar when the Submit button is clicked on a formless login', async () => {
    loadPracticeTestSite()
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') return { ok: true, data: [] }
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      return { ok: true, data: { id: 'new' } }
    }
    installChrome(state)
    await loadContentScript()
    const btn = document.querySelector<HTMLButtonElement>('#submit')!
    btn.click()
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).not.toBeNull())
    const buttons = Array.from(
      document.querySelectorAll<HTMLButtonElement>('#albear-save-bar button'),
    )
    expect(buttons.map((b) => b.textContent)).toEqual(['Save', 'Dismiss'])
    buttons[0]!.click()
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).toBeNull())
    const saveCall = state.calls.find((m) => m['kind'] === 'records.saveLogin')
    expect(saveCall).toBeDefined()
    expect(saveCall!['username']).toBe('student')
    expect(saveCall!['password']).toBe('Password123')
  })

  it('captures the live password value at click time, before it is later wiped', async () => {
    // Regression test: the wipe must not race ahead of the page's own
    // submit/click handling reading the field to perform the real login.
    loadPracticeTestSite()
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') return { ok: true, data: [] }
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      return { ok: true, data: { id: 'new' } }
    }
    installChrome(state)
    await loadContentScript()
    const password = document.querySelector<HTMLInputElement>('#password')!
    expect(password.value).toBe('Password123')
    document.querySelector<HTMLButtonElement>('#submit')!.click()
    // The value must still be live synchronously right after the click, so
    // a real page's own (synchronous) click handler can read it.
    expect(password.value).toBe('Password123')
    const stashCall = () => state.calls.find((m) => m['kind'] === 'records.stashCapture')
    await vi.waitFor(() => expect(stashCall()).toBeDefined())
    expect(stashCall()!['password']).toBe('Password123')
  })

  it('wipes the password field on the next tick after capture, so it does not linger', async () => {
    loadPracticeTestSite()
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') return { ok: true, data: [] }
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      return { ok: true, data: { id: 'new' } }
    }
    installChrome(state)
    await loadContentScript()
    const password = document.querySelector<HTMLInputElement>('#password')!
    document.querySelector<HTMLButtonElement>('#submit')!.click()
    await vi.waitFor(() => expect(password.value).toBe(''))
  })

  it('does not show the bar when an unrelated button is clicked (no password filled)', async () => {
    // The page has a formless password field but the user has not typed
    // anything yet; clicks on other buttons should not trigger the bar.
    loadPracticeTestSite()
    const menu = document.createElement('button')
    menu.id = 'menu-toggle'
    menu.textContent = 'Menu'
    document.body.prepend(menu)
    document.querySelector<HTMLInputElement>('#password')!.value = ''
    state.reply = (msg) => {
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      return { ok: true, data: null }
    }
    installChrome(state)
    await loadContentScript()
    document.querySelector<HTMLButtonElement>('#menu-toggle')!.click()
    await new Promise((r) => setTimeout(r, 10))
    expect(document.getElementById('albear-save-bar')).toBeNull()
  })
})

// ---- content script: resume a stashed capture on the next page load -------
//
// Regression coverage for the actual bug: a real login handler navigates
// the tab away immediately after submit, tearing down the document before
// the async gate/match/render chain can finish. The background stashes the
// capture (see capture.test.ts); the content script on the NEXT page's
// load must pick it back up via records.consumeCapture and offer the bar
// there, without the user having touched anything on the new page.

describe('content script: resumes a capture stashed by a previous page', () => {
  let state: StubState

  beforeEach(() => {
    state = { calls: [], reply: () => ({ ok: true, data: null }) }
  })

  afterEach(() => {
    uninstallChrome()
  })

  it('offers save on load when the background has a pending capture for this tab', async () => {
    setBody('<p>logged in successfully</p>')
    state.reply = (msg) => {
      if (msg['kind'] === 'records.consumeCapture') {
        return { ok: true, data: { username: 'alice', password: 'hunter2' } }
      }
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      if (msg['kind'] === 'records.matchForTab') return { ok: true, data: [] }
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      return { ok: true, data: { id: 'new' } }
    }
    installChrome(state)
    await loadContentScript()
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).not.toBeNull())
    const buttons = Array.from(
      document.querySelectorAll<HTMLButtonElement>('#albear-save-bar button'),
    )
    expect(buttons.map((b) => b.textContent)).toEqual(['Save', 'Dismiss'])
    buttons[0]!.click()
    await vi.waitFor(() => expect(document.getElementById('albear-save-bar')).toBeNull())
    const saveCall = state.calls.find((m) => m['kind'] === 'records.saveLogin')
    expect(saveCall!['username']).toBe('alice')
    expect(saveCall!['password']).toBe('hunter2')
  })

  it('does nothing on load when there is no pending capture', async () => {
    setBody('<p>some unrelated page</p>')
    state.reply = (msg) => {
      if (msg['kind'] === 'records.consumeCapture') return { ok: true, data: null }
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: true } }
      return { ok: true, data: null }
    }
    installChrome(state)
    await loadContentScript()
    await new Promise((r) => setTimeout(r, 10))
    expect(document.getElementById('albear-save-bar')).toBeNull()
  })

  it('does not offer save on load when the vault is locked, even with a pending capture', async () => {
    setBody('<p>logged in successfully</p>')
    state.reply = (msg) => {
      if (msg['kind'] === 'records.consumeCapture') {
        return { ok: true, data: { username: 'alice', password: 'hunter2' } }
      }
      if (msg['kind'] === 'status') return { ok: true, data: { paired: true, unlocked: false } }
      return { ok: true, data: null }
    }
    installChrome(state)
    await loadContentScript()
    await new Promise((r) => setTimeout(r, 10))
    expect(document.getElementById('albear-save-bar')).toBeNull()
  })

  it('does not blow up when records.consumeCapture itself errors', async () => {
    setBody('<p>logged in successfully</p>')
    state.reply = (msg) => {
      if (msg['kind'] === 'records.consumeCapture') {
        return { ok: false, error: { code: 'INTERNAL', message: 'boom' } }
      }
      return { ok: true, data: null }
    }
    installChrome(state)
    await expect(loadContentScript()).resolves.toBeUndefined()
    await new Promise((r) => setTimeout(r, 10))
    expect(document.getElementById('albear-save-bar')).toBeNull()
  })
})
