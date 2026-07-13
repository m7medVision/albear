// Login-form detection and filling. Pure DOM logic, unit-testable.

export interface LoginForm {
  form: HTMLFormElement | null
  username: HTMLInputElement | null
  password: HTMLInputElement
}

const USERNAME_HINTS = ['user', 'email', 'login', 'account', 'identifier']

function isVisible(el: HTMLElement): boolean {
  return el.offsetParent !== null || el.getClientRects().length > 0
}

/** Finds candidate login forms: every visible password field anchors one. */
export function detectLoginForms(root: Document | ShadowRoot): LoginForm[] {
  const out: LoginForm[] = []
  const passwords = root.querySelectorAll<HTMLInputElement>('input[type="password"]')
  for (const password of passwords) {
    if (!isVisible(password)) continue
    const form = password.closest('form')
    out.push({ form, password, username: findUsernameField(password, form) })
  }
  // Shadow DOM (PRD 26.6): one level of open shadow roots.
  if (root instanceof Document) {
    for (const host of root.querySelectorAll('*')) {
      if (host.shadowRoot) out.push(...detectLoginForms(host.shadowRoot))
    }
  }
  return out
}

function findUsernameField(
  password: HTMLInputElement,
  form: HTMLFormElement | null,
): HTMLInputElement | null {
  const scope: ParentNode = form ?? password.ownerDocument
  const inputs = Array.from(
    scope.querySelectorAll<HTMLInputElement>(
      'input[type="text"], input[type="email"], input:not([type])',
    ),
  ).filter(isVisible)

  // Prefer autocomplete markers, then name/id hints, then the nearest text
  // input before the password field.
  const byAutocomplete = inputs.find((i) =>
    ['username', 'email'].includes(i.getAttribute('autocomplete') ?? ''),
  )
  if (byAutocomplete) return byAutocomplete

  const byHint = inputs.find((i) => {
    const label = `${i.name} ${i.id} ${i.getAttribute('placeholder') ?? ''}`.toLowerCase()
    return USERNAME_HINTS.some((h) => label.includes(h))
  })
  if (byHint) return byHint

  let best: HTMLInputElement | null = null
  for (const input of inputs) {
    const pos = password.compareDocumentPosition(input)
    if (pos & Node.DOCUMENT_POSITION_PRECEDING) best = input
  }
  return best
}

/** Fills a field the way a user would, so framework listeners fire. */
export function fillField(input: HTMLInputElement, value: string): void {
  input.focus()
  // React and friends track the native value setter.
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  if (setter) setter.call(input, value)
  else input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
  input.blur()
}

export function fillLogin(form: LoginForm, username: string, password: string): void {
  if (form.username && username) fillField(form.username, username)
  fillField(form.password, password)
}
