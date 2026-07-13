import { keyPairFromPrivate, type KeyPair } from '../noise/noise'
import { fromHex, toHex } from './identity'

export interface StoredPairing {
  pairingId: string
  staticPrivHex: string
  phrase: string
}

const KEY = 'albear.pairing'

export async function loadPairing(): Promise<StoredPairing | null> {
  const got = await chrome.storage.local.get(KEY)
  return (got[KEY] as StoredPairing | undefined) ?? null
}

export async function savePairing(p: StoredPairing): Promise<void> {
  await chrome.storage.local.set({ [KEY]: p })
}

export async function clearPairing(): Promise<void> {
  await chrome.storage.local.remove(KEY)
}

export function pairingStaticKey(p: StoredPairing): KeyPair {
  return keyPairFromPrivate(fromHex(p.staticPrivHex))
}

export function pairingToStored(p: { pairingId: string; staticKey: KeyPair; phrase: string }): StoredPairing {
  return {
    pairingId: p.pairingId,
    staticPrivHex: toHex(p.staticKey.privateKey),
    phrase: p.phrase,
  }
}
