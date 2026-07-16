// VaultTransport tests: a mock native port backed by a real responder-side
// Noise implementation (built from our own primitives, already pinned against
// Go by the vector tests). Exercises handshake, pinning, request/response,
// relay errors, and tamper handling.
import { describe, expect, it } from 'vitest'
import { hmac } from '@noble/hashes/hmac.js'
import { sha256 } from '@noble/hashes/sha2.js'
// See extension/src/noise/noise.ts for why these use explicit .js subpaths.
import { x25519 } from '@noble/curves/ed25519.js'
import { chacha20poly1305 } from '@noble/ciphers/chacha.js'
import { generateKeyPair, type KeyPair } from '../src/noise/noise'
import {
  VaultError,
  VaultTransport,
  b64decode,
  b64encode,
  type Port,
} from '../src/messaging/transport'

// ---- responder-side Noise (mirror of the daemon) ---------------------------

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let off = 0
  for (const p of parts) {
    out.set(p, off)
    off += p.length
  }
  return out
}

function hkdf(ck: Uint8Array, input: Uint8Array, n: 2 | 3): Uint8Array[] {
  const t = hmac(sha256, ck, input)
  const o1 = hmac(sha256, t, new Uint8Array([1]))
  const o2 = hmac(sha256, t, concat(o1, new Uint8Array([2])))
  if (n === 2) return [o1, o2]
  return [o1, o2, hmac(sha256, t, concat(o2, new Uint8Array([3])))]
}

class RespCipher {
  k: Uint8Array | null = null
  n = 0n
  init(k: Uint8Array | null) {
    this.k = k
    this.n = 0n
  }
  nonce(): Uint8Array {
    const b = new Uint8Array(12)
    new DataView(b.buffer).setBigUint64(4, this.n, true)
    return b
  }
  enc(ad: Uint8Array, pt: Uint8Array): Uint8Array {
    if (!this.k) return pt
    const ct = chacha20poly1305(this.k, this.nonce(), ad).encrypt(pt)
    this.n++
    if (this.n % 4096n === 0n) this.rekey()
    return ct
  }
  dec(ad: Uint8Array, ct: Uint8Array): Uint8Array {
    if (!this.k) return ct
    const pt = chacha20poly1305(this.k, this.nonce(), ad).decrypt(ct)
    this.n++
    if (this.n % 4096n === 0n) this.rekey()
    return pt
  }
  rekey() {
    if (!this.k) return
    const nonce = new Uint8Array(12)
    new DataView(nonce.buffer).setBigUint64(4, 0xffffffffffffffffn, true)
    this.k = chacha20poly1305(this.k, nonce, new Uint8Array(0)).encrypt(new Uint8Array(32)).slice(0, 32)
  }
}

class Responder {
  ck: Uint8Array
  h: Uint8Array
  cipher = new RespCipher()
  e: KeyPair | null = null
  re: Uint8Array | null = null
  rs: Uint8Array | null = null
  send = new RespCipher()
  recv = new RespCipher()

  constructor(
    readonly s: KeyPair,
    readonly psk: Uint8Array | null,
    prologue: Uint8Array,
  ) {
    const name = new TextEncoder().encode(
      psk ? 'Noise_XXpsk3_25519_ChaChaPoly_SHA256' : 'Noise_XX_25519_ChaChaPoly_SHA256',
    )
    this.h = name.length <= 32 ? concat(name, new Uint8Array(32 - name.length)) : sha256(name)
    this.ck = this.h.slice()
    this.mixHash(prologue)
  }
  mixHash(d: Uint8Array) {
    this.h = sha256(concat(this.h, d))
  }
  mixKey(i: Uint8Array) {
    const [ck, k] = hkdf(this.ck, i, 2) as [Uint8Array, Uint8Array]
    this.ck = ck
    this.cipher.init(k)
  }
  mixKeyAndHash(i: Uint8Array) {
    const [ck, th, k] = hkdf(this.ck, i, 3) as [Uint8Array, Uint8Array, Uint8Array]
    this.ck = ck
    this.mixHash(th)
    this.cipher.init(k)
  }
  encH(pt: Uint8Array): Uint8Array {
    const ct = this.cipher.enc(this.h, pt)
    this.mixHash(ct)
    return ct
  }
  decH(ct: Uint8Array): Uint8Array {
    const pt = this.cipher.dec(this.h, ct)
    this.mixHash(ct)
    return pt
  }

  readA(msg: Uint8Array): void {
    this.re = msg.slice(0, 32)
    this.mixHash(this.re)
    if (this.psk) this.mixKey(this.re)
    this.decH(msg.slice(32))
  }
  writeB(): Uint8Array {
    this.e = generateKeyPair()
    this.mixHash(this.e.publicKey)
    if (this.psk) this.mixKey(this.e.publicKey)
    this.mixKey(x25519.getSharedSecret(this.e.privateKey, this.re!))
    const encS = this.encH(this.s.publicKey)
    this.mixKey(x25519.getSharedSecret(this.s.privateKey, this.re!))
    const encPayload = this.encH(new Uint8Array(0))
    return concat(this.e.publicKey, encS, encPayload)
  }
  readC(msg: Uint8Array): void {
    this.rs = this.decH(msg.slice(0, 48))
    this.mixKey(x25519.getSharedSecret(this.e!.privateKey, this.rs))
    if (this.psk) this.mixKeyAndHash(this.psk)
    this.decH(msg.slice(48))
    const [k1, k2] = hkdf(this.ck, new Uint8Array(0), 2) as [Uint8Array, Uint8Array]
    this.recv.init(k1) // initiator→responder
    this.send.init(k2)
  }
}

// ---- mock port wired to a scripted daemon ----------------------------------

type Listener = (msg: unknown) => void

function mockDaemon(opts: {
  staticKey?: KeyPair
  psk?: Uint8Array | null
  onRequest?: (op: string, payload: unknown) => { ok: boolean; data?: unknown; error?: { code: string; message: string } }
  tamperTransport?: boolean
}) {
  const daemonKey = opts.staticKey ?? generateKeyPair()
  const listeners: Listener[] = []
  const disconnects: Array<() => void> = []
  let responder: Responder | null = null
  let step = 0

  const port: Port = {
    postMessage(raw: unknown) {
      const msg = raw as { frame?: string }
      if (!msg.frame) return
      const frame = b64decode(msg.frame)
      queueMicrotask(() => {
        if (step === 0) {
          responder = new Responder(daemonKey, opts.psk ?? null, frame)
          step = 1
          return
        }
        if (step === 1) {
          responder!.readA(frame)
          emit(responder!.writeB())
          step = 2
          return
        }
        if (step === 2) {
          responder!.readC(frame)
          step = 3
          return
        }
        // Transport request.
        const req = JSON.parse(new TextDecoder().decode(responder!.recv.dec(new Uint8Array(0), frame))) as {
          requestId: string
          operation: string
          payload?: unknown
        }
        const result = opts.onRequest?.(req.operation, req.payload) ?? { ok: true, data: {} }
        const resp = { protocolVersion: 1, requestId: req.requestId, ...result }
        let ct = responder!.send.enc(new Uint8Array(0), new TextEncoder().encode(JSON.stringify(resp)))
        if (opts.tamperTransport) ct[0]! ^= 1
        emit(ct)
      })
    },
    onMessage: { addListener: (fn: Listener) => listeners.push(fn) },
    onDisconnect: { addListener: (fn: () => void) => disconnects.push(fn) },
    disconnect: () => disconnects.forEach((fn) => fn()),
  }

  function emit(frame: Uint8Array): void {
    for (const l of listeners) l({ frame: b64encode(frame) })
  }

  return { port, daemonKey }
}

// ---- tests -----------------------------------------------------------------

describe('VaultTransport', () => {
  it('completes an XX pairing handshake and calls operations', async () => {
    const { port } = mockDaemon({
      onRequest: (op) => (op === 'vault.status' ? { ok: true, data: { unlocked: true } } : { ok: false, error: { code: 'INVALID_REQUEST', message: 'x' } }),
    })
    const t = await VaultTransport.connect(port, { mode: 'pair', staticKey: generateKeyPair() })
    const st = await t.call<{ unlocked: boolean }>('vault.status')
    expect(st.unlocked).toBe(true)
  })

  it('completes an XXpsk3 paired handshake with matching PSK', async () => {
    const psk = crypto.getRandomValues(new Uint8Array(32))
    const { port } = mockDaemon({ psk })
    const t = await VaultTransport.connect(port, {
      mode: 'paired',
      clientId: 'aa',
      staticKey: generateKeyPair(),
      psk,
    })
    await expect(t.call('anything')).resolves.toEqual({})
  })

  it('enforces daemon static key pinning', async () => {
    const { port } = mockDaemon({})
    const wrongPin = generateKeyPair().publicKey
    await expect(
      VaultTransport.connect(port, { mode: 'pair', staticKey: generateKeyPair(), pinnedDaemonKey: wrongPin }),
    ).rejects.toMatchObject({ code: 'STATIC_KEY_MISMATCH' })
  })

  it('accepts the correct pinned daemon key', async () => {
    const daemonKey = generateKeyPair()
    const { port } = mockDaemon({ staticKey: daemonKey })
    const t = await VaultTransport.connect(port, {
      mode: 'pair',
      staticKey: generateKeyPair(),
      pinnedDaemonKey: daemonKey.publicKey,
    })
    await expect(t.call('x')).resolves.toEqual({})
  })

  it('surfaces daemon errors as VaultError with code', async () => {
    const { port } = mockDaemon({
      onRequest: () => ({ ok: false, error: { code: 'VAULT_LOCKED', message: 'The vault is locked.' } }),
    })
    const t = await VaultTransport.connect(port, { mode: 'pair', staticKey: generateKeyPair() })
    await expect(t.call('records.match')).rejects.toMatchObject({ code: 'VAULT_LOCKED' })
  })

  it('fails closed on tampered transport frames', async () => {
    const { port } = mockDaemon({ tamperTransport: true })
    const t = await VaultTransport.connect(port, { mode: 'pair', staticKey: generateKeyPair() })
    await expect(t.call('x')).rejects.toBeInstanceOf(VaultError)
    expect(t.isClosed).toBe(true)
  })

  it('rejects pending calls when the relay reports an error', async () => {
    const listeners: Listener[] = []
    const port: Port = {
      postMessage() {
        queueMicrotask(() => listeners.forEach((l) => l({ error: 'daemon unavailable' })))
      },
      onMessage: { addListener: (fn: Listener) => listeners.push(fn) },
      onDisconnect: { addListener: () => {} },
      disconnect: () => {},
    }
    await expect(
      VaultTransport.connect(port, { mode: 'pair', staticKey: generateKeyPair() }),
    ).rejects.toMatchObject({ code: 'RELAY' })
  })

  it('base64 helpers round-trip', () => {
    const b = crypto.getRandomValues(new Uint8Array(64))
    expect(b64decode(b64encode(b))).toEqual(b)
  })
})
