// Capture-storage round-trip tests. A submitted login candidate is stashed
// in chrome.storage.session (keyed by tab id) so it survives the full-page
// navigation a site's login handler triggers right after submit — the
// content script picks it back up on the next page load.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearCapture, consumeCapture, stashCapture } from '../src/messaging/capture'

const store = new Map<string, unknown>()

beforeEach(() => {
  store.clear()
  ;(globalThis as unknown as { chrome: unknown }).chrome = {
    storage: {
      session: {
        get: (k: string) => {
          const got: Record<string, unknown> = {}
          got[k] = store.get(k)
          return Promise.resolve(got)
        },
        set: (o: Record<string, unknown>) => {
          for (const [k, v] of Object.entries(o)) store.set(k, v)
          return Promise.resolve()
        },
        remove: (k: string) => {
          store.delete(k)
          return Promise.resolve()
        },
      },
    },
  }
})

afterEach(() => {
  delete (globalThis as unknown as { chrome?: unknown }).chrome
  vi.useRealTimers()
})

describe('capture storage', () => {
  it('round-trips a stashed capture for the same tab and origin', async () => {
    await stashCapture(7, { origin: 'https://example.com', username: 'alice', password: 'hunter2' })
    const got = await consumeCapture(7, 'https://example.com')
    expect(got).toEqual({ username: 'alice', password: 'hunter2' })
  })

  it('is consume-once: a second read returns null', async () => {
    await stashCapture(7, { origin: 'https://example.com', username: 'alice', password: 'hunter2' })
    await consumeCapture(7, 'https://example.com')
    expect(await consumeCapture(7, 'https://example.com')).toBeNull()
  })

  it('returns null when nothing was stashed for that tab', async () => {
    expect(await consumeCapture(99, 'https://example.com')).toBeNull()
  })

  it('is scoped per tab id', async () => {
    await stashCapture(1, { origin: 'https://a.com', username: 'a', password: 'pa' })
    await stashCapture(2, { origin: 'https://b.com', username: 'b', password: 'pb' })
    expect(await consumeCapture(1, 'https://a.com')).toEqual({ username: 'a', password: 'pa' })
    expect(await consumeCapture(2, 'https://b.com')).toEqual({ username: 'b', password: 'pb' })
  })

  it('returns null and clears the entry when the origin does not match', async () => {
    await stashCapture(7, { origin: 'https://example.com', username: 'alice', password: 'hunter2' })
    expect(await consumeCapture(7, 'https://evil.com')).toBeNull()
    // Dropped, not just origin-mismatched-once: a retry with the right
    // origin must not resurrect it.
    expect(await consumeCapture(7, 'https://example.com')).toBeNull()
  })

  it('returns null once the TTL has elapsed', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(0)
    await stashCapture(7, { origin: 'https://example.com', username: 'alice', password: 'hunter2' })
    vi.setSystemTime(121_000)
    expect(await consumeCapture(7, 'https://example.com')).toBeNull()
  })

  it('clearCapture removes a stash without needing to consume it', async () => {
    await stashCapture(7, { origin: 'https://example.com', username: 'alice', password: 'hunter2' })
    await clearCapture(7)
    expect(await consumeCapture(7, 'https://example.com')).toBeNull()
  })
})
