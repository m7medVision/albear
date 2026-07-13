// Login-form detection and fill tests (PRD 26.6): standard forms, hints,
// multiple forms, no-form pages, and framework-compatible filling.
// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import { detectLoginForms, fillField, fillLogin } from '../src/content/forms'

// jsdom has no layout: make everything "visible" via getClientRects.
beforeEach(() => {
  document.body.innerHTML = ''
  Element.prototype.getClientRects = function () {
    return [{ width: 1, height: 1 }] as unknown as DOMRectList
  }
})

function setBody(html: string): void {
  document.body.innerHTML = html
}

describe('detectLoginForms', () => {
  it('finds a standard login form', () => {
    setBody(`
      <form id="f">
        <input type="email" name="email" />
        <input type="password" name="pass" />
      </form>`)
    const forms = detectLoginForms(document)
    expect(forms).toHaveLength(1)
    expect(forms[0]!.username?.name).toBe('email')
    expect(forms[0]!.password.name).toBe('pass')
    expect(forms[0]!.form?.id).toBe('f')
  })

  it('prefers autocomplete=username', () => {
    setBody(`
      <form>
        <input type="text" name="misc" />
        <input type="text" name="who" autocomplete="username" />
        <input type="password" />
      </form>`)
    const forms = detectLoginForms(document)
    expect(forms[0]!.username?.name).toBe('who')
  })

  it('falls back to name hints', () => {
    setBody(`
      <form>
        <input type="text" name="captcha" />
        <input type="text" name="user_login" />
        <input type="password" />
      </form>`)
    expect(detectLoginForms(document)[0]!.username?.name).toBe('user_login')
  })

  it('uses the nearest preceding text input as a last resort', () => {
    setBody(`
      <form>
        <input type="text" name="alpha" />
        <input type="text" name="beta" />
        <input type="password" />
      </form>`)
    expect(detectLoginForms(document)[0]!.username?.name).toBe('beta')
  })

  it('handles formless password fields', () => {
    setBody(`<input type="email" name="e"/><input type="password" name="p"/>`)
    const forms = detectLoginForms(document)
    expect(forms).toHaveLength(1)
    expect(forms[0]!.form).toBeNull()
    expect(forms[0]!.username?.name).toBe('e')
  })

  it('finds multiple password forms', () => {
    setBody(`
      <form><input type="text" name="a"/><input type="password" name="p1"/></form>
      <form><input type="text" name="b"/><input type="password" name="p2"/></form>`)
    expect(detectLoginForms(document)).toHaveLength(2)
  })

  it('returns nothing on pages without password fields', () => {
    setBody(`<form><input type="text" name="q"/></form>`)
    expect(detectLoginForms(document)).toHaveLength(0)
  })

  it('detects forms inside open shadow DOM', () => {
    setBody(`<div id="host"></div>`)
    const host = document.getElementById('host')!
    const shadow = host.attachShadow({ mode: 'open' })
    shadow.innerHTML = `<form><input type="text" name="u"/><input type="password" name="p"/></form>`
    const forms = detectLoginForms(document)
    expect(forms).toHaveLength(1)
    expect(forms[0]!.username?.name).toBe('u')
  })
})

describe('fill', () => {
  it('fills fields and fires input/change events', () => {
    setBody(`<form><input type="text" name="u"/><input type="password" name="p"/></form>`)
    const form = detectLoginForms(document)[0]!
    const events: string[] = []
    form.password.addEventListener('input', () => events.push('input'))
    form.password.addEventListener('change', () => events.push('change'))

    fillLogin(form, 'mo', 'hunter2')
    expect(form.username!.value).toBe('mo')
    expect(form.password.value).toBe('hunter2')
    expect(events).toEqual(['input', 'change'])
  })

  it('fillField works without a username field', () => {
    setBody(`<input type="password" name="p"/>`)
    const form = detectLoginForms(document)[0]!
    fillLogin(form, '', 'pw')
    expect(form.password.value).toBe('pw')
  })

  it('fillField sets value through the native setter', () => {
    setBody(`<input type="text" id="x"/>`)
    const input = document.getElementById('x') as HTMLInputElement
    fillField(input, 'abc')
    expect(input.value).toBe('abc')
  })
})
