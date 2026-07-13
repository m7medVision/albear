// Paired-client identity persistence in chrome.storage.local.
//
// Documented limitation (PRD 12.4): same-OS-user malware can read this
// storage. The credential grants a transport session, never vault decryption,
// and is revocable from the CLI.
import { keyPairFromPrivate, type KeyPair } from '../noise/noise'

export interface StoredIdentity {
  clientId: string
  credentialHex: string
  daemonStaticKeyHex: string
  staticPrivHex: string
}

const KEY = 'albear.identity'

export const toHex = (b: Uint8Array): string =>
  Array.from(b).map((x) => x.toString(16).padStart(2, '0')).join('')
export const fromHex = (s: string): Uint8Array =>
  new Uint8Array((s.match(/.{2}/g) ?? []).map((b) => parseInt(b, 16)))

export async function loadIdentity(): Promise<StoredIdentity | null> {
  const got = await chrome.storage.local.get(KEY)
  return (got[KEY] as StoredIdentity | undefined) ?? null
}

export async function saveIdentity(id: StoredIdentity): Promise<void> {
  await chrome.storage.local.set({ [KEY]: id })
}

export async function clearIdentity(): Promise<void> {
  await chrome.storage.local.remove(KEY)
}

export function identityStaticKey(id: StoredIdentity): KeyPair {
  return keyPairFromPrivate(fromHex(id.staticPrivHex))
}

/** PSK = SHA-256(credential), matching the daemon's stored verifier. */
export async function identityPSK(id: StoredIdentity): Promise<Uint8Array> {
  const digest = await crypto.subtle.digest('SHA-256', fromHex(id.credentialHex) as BufferSource)
  return new Uint8Array(digest)
}
