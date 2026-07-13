// Noise Protocol Framework client for albear's transport encryption
// (PRD 12.4): Noise_XX_25519_ChaChaPoly_SHA256 for pairing and
// Noise_XXpsk3_25519_ChaChaPoly_SHA256 for paired sessions, initiator side.
//
// Implemented directly from the Noise specification (rev 34) on top of the
// audited @noble primitives. Interoperability with the Go daemon
// (flynn/noise) is pinned by cross-language test vectors generated from Go.

import { chacha20poly1305 } from '@noble/ciphers/chacha'
import { x25519 } from '@noble/curves/ed25519'
import { hmac } from '@noble/hashes/hmac'
import { sha256 } from '@noble/hashes/sha256'

export interface KeyPair {
  publicKey: Uint8Array
  privateKey: Uint8Array
}

export function generateKeyPair(): KeyPair {
  const privateKey = crypto.getRandomValues(new Uint8Array(32))
  return { privateKey, publicKey: x25519.getPublicKey(privateKey) }
}

export function keyPairFromPrivate(privateKey: Uint8Array): KeyPair {
  return { privateKey, publicKey: x25519.getPublicKey(privateKey) }
}

function dh(priv: Uint8Array, pub: Uint8Array): Uint8Array {
  return x25519.getSharedSecret(priv, pub)
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let off = 0
  for (const p of parts) {
    out.set(p, off)
    off += p.length
  }
  return out
}

// HKDF as defined by the Noise spec (HMAC-SHA256 chain).
function hkdf(chainingKey: Uint8Array, input: Uint8Array, outputs: 2 | 3): Uint8Array[] {
  const tempKey = hmac(sha256, chainingKey, input)
  const out1 = hmac(sha256, tempKey, new Uint8Array([0x01]))
  const out2 = hmac(sha256, tempKey, concat(out1, new Uint8Array([0x02])))
  if (outputs === 2) return [out1, out2]
  const out3 = hmac(sha256, tempKey, concat(out2, new Uint8Array([0x03])))
  return [out1, out2, out3]
}

// CipherState: ChaCha20-Poly1305 with the Noise nonce layout
// (4 zero bytes || 64-bit little-endian counter).
export class CipherState {
  private k: Uint8Array | null = null
  private n = 0n

  initializeKey(key: Uint8Array | null): void {
    this.k = key
    this.n = 0n
  }

  hasKey(): boolean {
    return this.k !== null
  }

  private nonce(): Uint8Array {
    const nonce = new Uint8Array(12)
    new DataView(nonce.buffer).setBigUint64(4, this.n, true)
    return nonce
  }

  encryptWithAd(ad: Uint8Array, plaintext: Uint8Array): Uint8Array {
    if (!this.k) return plaintext
    const ct = chacha20poly1305(this.k, this.nonce(), ad).encrypt(plaintext)
    this.n++
    this.maybeRekey()
    return ct
  }

  decryptWithAd(ad: Uint8Array, ciphertext: Uint8Array): Uint8Array {
    if (!this.k) return ciphertext
    const pt = chacha20poly1305(this.k, this.nonce(), ad).decrypt(ciphertext)
    this.n++
    this.maybeRekey()
    return pt
  }

  // Deterministic rekey every RekeyInterval messages, mirroring the daemon
  // (internal/infrastructure/transport/noise/conn.go).
  static readonly REKEY_INTERVAL = 4096n

  private maybeRekey(): void {
    if (this.n !== 0n && this.n % CipherState.REKEY_INTERVAL === 0n) this.rekey()
  }

  rekey(): void {
    if (!this.k) return
    // REKEY(k): encrypt 32 zero bytes with nonce 2^64-1, take first 32 bytes.
    const nonce = new Uint8Array(12)
    new DataView(nonce.buffer).setBigUint64(4, 0xffffffffffffffffn, true)
    const ct = chacha20poly1305(this.k, nonce, new Uint8Array(0)).encrypt(new Uint8Array(32))
    this.k = ct.slice(0, 32)
  }
}

class SymmetricState {
  ck: Uint8Array
  h: Uint8Array
  cipher = new CipherState()

  constructor(protocolName: string) {
    const name = new TextEncoder().encode(protocolName)
    this.h = name.length <= 32 ? concat(name, new Uint8Array(32 - name.length)) : sha256(name)
    this.ck = this.h.slice()
  }

  mixKey(input: Uint8Array): void {
    const [ck, tempK] = hkdf(this.ck, input, 2) as [Uint8Array, Uint8Array]
    this.ck = ck
    this.cipher.initializeKey(tempK)
  }

  mixHash(data: Uint8Array): void {
    this.h = sha256(concat(this.h, data))
  }

  mixKeyAndHash(input: Uint8Array): void {
    const [ck, tempH, tempK] = hkdf(this.ck, input, 3) as [Uint8Array, Uint8Array, Uint8Array]
    this.ck = ck
    this.mixHash(tempH)
    this.cipher.initializeKey(tempK)
  }

  encryptAndHash(plaintext: Uint8Array): Uint8Array {
    const ct = this.cipher.encryptWithAd(this.h, plaintext)
    this.mixHash(ct)
    return ct
  }

  decryptAndHash(ciphertext: Uint8Array): Uint8Array {
    const pt = this.cipher.decryptWithAd(this.h, ciphertext)
    this.mixHash(ciphertext)
    return pt
  }

  split(): [CipherState, CipherState] {
    const [k1, k2] = hkdf(this.ck, new Uint8Array(0), 2) as [Uint8Array, Uint8Array]
    const c1 = new CipherState()
    c1.initializeKey(k1)
    const c2 = new CipherState()
    c2.initializeKey(k2)
    return [c1, c2]
  }
}

export interface HandshakeOptions {
  /** Local static keypair. */
  staticKey: KeyPair
  /** PSK enables XXpsk3; omit for plain XX (pairing channel). */
  psk?: Uint8Array
  /** Prologue: the exact hello frame bytes. */
  prologue: Uint8Array
  /** Injected ephemeral for deterministic tests. */
  ephemeral?: KeyPair
}

export class HandshakeError extends Error {}

/**
 * Initiator-side XX / XXpsk3 handshake. Usage:
 *   const hs = new XXHandshake(opts)
 *   send(hs.writeMessageA())
 *   hs.readMessageB(recv())   // daemon static now available via remoteStatic
 *   send(hs.writeMessageC())
 *   const { send, recv } = hs.split()
 */
export class XXHandshake {
  private ss: SymmetricState
  private s: KeyPair
  private e: KeyPair | null = null
  private re: Uint8Array | null = null
  private rs: Uint8Array | null = null
  private psk: Uint8Array | null
  private ephemeralOverride: KeyPair | null

  constructor(opts: HandshakeOptions) {
    const name = opts.psk
      ? 'Noise_XXpsk3_25519_ChaChaPoly_SHA256'
      : 'Noise_XX_25519_ChaChaPoly_SHA256'
    this.ss = new SymmetricState(name)
    this.ss.mixHash(opts.prologue)
    this.s = opts.staticKey
    this.psk = opts.psk ?? null
    this.ephemeralOverride = opts.ephemeral ?? null
  }

  get remoteStatic(): Uint8Array {
    if (!this.rs) throw new HandshakeError('remote static not yet received')
    return this.rs
  }

  // -> e
  writeMessageA(payload: Uint8Array = new Uint8Array(0)): Uint8Array {
    this.e = this.ephemeralOverride ?? generateKeyPair()
    this.ss.mixHash(this.e.publicKey)
    if (this.psk) this.ss.mixKey(this.e.publicKey)
    const encPayload = this.ss.encryptAndHash(payload)
    return concat(this.e.publicKey, encPayload)
  }

  // <- e, ee, s, es
  readMessageB(message: Uint8Array): Uint8Array {
    if (!this.e) throw new HandshakeError('out of order')
    if (message.length < 32) throw new HandshakeError('short message')
    this.re = message.slice(0, 32)
    this.ss.mixHash(this.re)
    if (this.psk) this.ss.mixKey(this.re)
    this.ss.mixKey(dh(this.e.privateKey, this.re))

    const rest = message.slice(32)
    const encStaticLen = 32 + 16 // encrypted static key + tag
    if (rest.length < encStaticLen) throw new HandshakeError('short message')
    let rs: Uint8Array
    try {
      rs = this.ss.decryptAndHash(rest.slice(0, encStaticLen))
    } catch {
      throw new HandshakeError('handshake authentication failed')
    }
    this.rs = rs
    this.ss.mixKey(dh(this.e.privateKey, this.rs))
    try {
      return this.ss.decryptAndHash(rest.slice(encStaticLen))
    } catch {
      throw new HandshakeError('handshake authentication failed')
    }
  }

  // -> s, se (, psk)
  writeMessageC(payload: Uint8Array = new Uint8Array(0)): Uint8Array {
    if (!this.re) throw new HandshakeError('out of order')
    const encS = this.ss.encryptAndHash(this.s.publicKey)
    this.ss.mixKey(dh(this.s.privateKey, this.re))
    if (this.psk) this.ss.mixKeyAndHash(this.psk)
    const encPayload = this.ss.encryptAndHash(payload)
    return concat(encS, encPayload)
  }

  /** After message C: [initiator→responder, responder→initiator]. */
  split(): { send: CipherState; recv: CipherState } {
    const [c1, c2] = this.ss.split()
    return { send: c1, recv: c2 }
  }
}
