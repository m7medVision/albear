// records.createForOrigin: popup-only handler that lets the user add a
// credential without performing a real login. Origin comes from the popup
// (the trusted surface) and is NOT derivable from a content script. This
// file pins the security boundary (the kind must never appear in
// contentAllowed) and the wire format the daemon sees.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

// Loaded dynamically after `chrome` is stubbed, because the background
// module's top level registers a chrome.runtime.onMessage listener.
let policy: typeof import('../src/background/index')

beforeEach(async () => {
  ;(globalThis as unknown as { chrome: unknown }).chrome = {
    runtime: {
      onMessage: { addListener: () => undefined },
      connectNative: () => ({}),
      sendMessage: () => Promise.resolve(),
    },
  }
  policy = await import('../src/background/index')
})

afterEach(() => {
  delete (globalThis as unknown as { chrome?: unknown }).chrome
})

describe('records.createForOrigin — security boundary', () => {
  it('is not callable from content scripts', () => {
    // If this assertion starts failing, the popup-only invariant has been
    // relaxed. Re-validate that the trust model still holds before shipping.
    expect(policy.contentAllowed.has('records.createForOrigin')).toBe(false)
  })
})

describe('validateCreateForOrigin', () => {
  it('returns a daemon payload for a valid https origin', () => {
    const r = policy.validateCreateForOrigin({
      origin: 'https://example.com',
      username: 'alice',
      password: 'hunter2',
    })
    expect(r).toEqual({
      ok: true,
      payload: {
        type: 'login',
        name: 'example.com',
        username: 'alice',
        password: 'hunter2',
        urls: ['https://example.com'],
      },
    })
  })

  it('uses the explicit name when provided, otherwise the hostname', () => {
    const named = policy.validateCreateForOrigin({
      origin: 'https://example.com',
      name: 'Work',
      username: 'alice',
      password: 'hunter2',
    })
    expect(named.ok && named.payload.name).toBe('Work')

    const anon = policy.validateCreateForOrigin({
      origin: 'https://example.com',
      username: 'alice',
      password: 'hunter2',
    })
    expect(anon.ok && anon.payload.name).toBe('example.com')
  })

  it('normalizes the origin (drops path/query/fragment)', () => {
    const r = policy.validateCreateForOrigin({
      origin: 'https://example.com/login?next=/x#frag',
      username: 'alice',
      password: 'hunter2',
    })
    expect(r.ok && r.payload.urls).toEqual(['https://example.com'])
  })

  it('rejects http origins with INVALID_REQUEST', () => {
    const r = policy.validateCreateForOrigin({
      origin: 'http://example.com',
      username: 'alice',
      password: 'hunter2',
    })
    expect(r).toEqual({
      ok: false,
      error: { code: 'INVALID_REQUEST', message: 'origin must be https' },
    })
  })

  it('rejects malformed origins with INVALID_REQUEST', () => {
    const r = policy.validateCreateForOrigin({ origin: 'not a url', username: 'a', password: 'b' })
    expect(r).toEqual({
      ok: false,
      error: { code: 'INVALID_REQUEST', message: 'bad origin' },
    })
  })

  it('rejects missing username or password', () => {
    expect(
      policy.validateCreateForOrigin({ origin: 'https://example.com', password: 'hunter2' }),
    ).toEqual({
      ok: false,
      error: { code: 'INVALID_REQUEST', message: 'username and password are required' },
    })
    expect(
      policy.validateCreateForOrigin({ origin: 'https://example.com', username: 'alice' }),
    ).toEqual({
      ok: false,
      error: { code: 'INVALID_REQUEST', message: 'username and password are required' },
    })
  })

  it('rejects empty string username or password', () => {
    expect(
      policy.validateCreateForOrigin({ origin: 'https://example.com', username: '', password: 'x' }),
    ).toEqual({
      ok: false,
      error: { code: 'INVALID_REQUEST', message: 'username and password are required' },
    })
  })
})
