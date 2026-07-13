// Capture storage: a submitted login candidate is stashed in
// chrome.storage.session (keyed by tab id) so it survives the full-page
// navigation a site's login handler triggers right after submit. The
// content script picks it back up on the next page load via consumeCapture.
//
// Stashes carry the origin they were captured from. consumeCapture only
// returns the entry to a caller on the same origin (defence in depth: a
// capture for example.com must not be redeemable from evil.com), and
// always drops the entry on read — consumed, mismatched, or expired.

const TTL_MS = 120_000

interface StoredCapture {
  origin: string
  username: string
  password: string
  expiresAt: number
}

function key(tabId: number): string {
  return String(tabId)
}

export async function stashCapture(
  tabId: number,
  capture: { origin: string; username: string; password: string },
): Promise<void> {
  const stored: StoredCapture = {
    ...capture,
    expiresAt: Date.now() + TTL_MS,
  }
  await chrome.storage.session.set({ [key(tabId)]: stored })
}

export async function consumeCapture(
  tabId: number,
  origin: string,
): Promise<{ username: string; password: string } | null> {
  const result = await chrome.storage.session.get(key(tabId))
  const stored = result[key(tabId)] as StoredCapture | undefined
  // ponytail: always drop on read. The next reader must not see a stale or
  // mismatched entry.
  if (stored !== undefined) await chrome.storage.session.remove(key(tabId))
  if (!stored) return null
  if (stored.expiresAt <= Date.now()) return null
  if (stored.origin !== origin) return null
  return { username: stored.username, password: stored.password }
}

export async function clearCapture(tabId: number): Promise<void> {
  await chrome.storage.session.remove(key(tabId))
}
