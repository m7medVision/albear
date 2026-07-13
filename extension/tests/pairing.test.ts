// Pairing-storage round-trip tests. The extension persists the in-progress
// pairing (pairingId, static priv, phrase) so claim survives MV3 SW death
// and popup reopens. This pins the storage shape that the background's
// re-handshake path depends on.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { generateKeyPair } from '../src/noise/noise'
import {
  clearPairing,
  loadPairing,
  pairingStaticKey,
  pairingToStored,
  savePairing,
} from '../src/messaging/pairing'

const store = new Map<string, unknown>()

beforeEach(() => {
  store.clear()
  ;(globalThis as unknown as { chrome: unknown }).chrome = {
    storage: {
      local: {
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
})

describe('pairing storage', () => {
  it('round-trips a stored pairing', async () => {
    const kp = generateKeyPair()
    const stored = pairingToStored({ pairingId: 'pid-123', staticKey: kp, phrase: 'abcd-efgh-ijkl' })
    await savePairing(stored)
    const got = await loadPairing()
    expect(got).toEqual(stored)
  })

  it('returns null when nothing is stored', async () => {
    expect(await loadPairing()).toBeNull()
  })

  it('clears on demand', async () => {
    const kp = generateKeyPair()
    await savePairing(pairingToStored({ pairingId: 'pid', staticKey: kp, phrase: 'x' }))
    await clearPairing()
    expect(await loadPairing()).toBeNull()
  })

  it('preserves the static key through pairingStaticKey', async () => {
    const kp = generateKeyPair()
    const stored = pairingToStored({ pairingId: 'pid', staticKey: kp, phrase: 'x' })
    const restored = pairingStaticKey(stored)
    expect(restored.publicKey).toEqual(kp.publicKey)
    expect(restored.privateKey).toEqual(kp.privateKey)
  })
})
