// VaultTransport: the extension's end-to-end encrypted channel to vaultd.
// Noise frames travel base64-wrapped through the vault-native blind relay
// (PRD 12.2). The relay sees ciphertext only.
import { CipherState, XXHandshake, type KeyPair } from '../noise/noise'

export const NATIVE_HOST = 'dev.albear.native'

export interface Port {
  postMessage(msg: unknown): void
  onMessage: { addListener(fn: (msg: unknown) => void): void }
  onDisconnect: { addListener(fn: () => void): void }
  disconnect(): void
}

interface WireMsg {
  frame?: string
  error?: string
}

export interface Envelope {
  protocolVersion: number
  requestId: string
  operation: string
  payload?: unknown
}

export interface ResponseEnvelope {
  protocolVersion: number
  requestId: string
  ok: boolean
  data?: unknown
  error?: { code: string; message: string }
}

export class VaultError extends Error {
  constructor(
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

const te = new TextEncoder()
const td = new TextDecoder()

export function b64encode(b: Uint8Array): string {
  let s = ''
  for (const x of b) s += String.fromCharCode(x)
  return btoa(s)
}

export function b64decode(s: string): Uint8Array {
  const raw = atob(s)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export type HelloMode = 'pair' | 'paired'

export interface TransportOptions {
  mode: HelloMode
  staticKey: KeyPair
  clientId?: string
  psk?: Uint8Array
  /** Pinned daemon static key (hex); enforced after message 2. */
  pinnedDaemonKey?: Uint8Array
}

/** Established encrypted session over a native-messaging port. */
export class VaultTransport {
  private send: CipherState
  private recv: CipherState
  private pending = new Map<string, { resolve: (r: ResponseEnvelope) => void; reject: (e: Error) => void }>()
  private counter = 0
  private closed = false
  readonly daemonStaticKey: Uint8Array

  private constructor(
    private port: Port,
    handshake: { send: CipherState; recv: CipherState },
    daemonStaticKey: Uint8Array,
  ) {
    this.send = handshake.send
    this.recv = handshake.recv
    this.daemonStaticKey = daemonStaticKey
  }

  /** Connects, runs the end-to-end handshake, and returns a live session. */
  static async connect(port: Port, opts: TransportOptions): Promise<VaultTransport> {
    const hello: Record<string, unknown> = { v: 1, mode: opts.mode }
    if (opts.clientId) hello['clientId'] = opts.clientId
    const helloRaw = te.encode(JSON.stringify(hello))

    const hs = new XXHandshake({
      staticKey: opts.staticKey,
      psk: opts.psk,
      prologue: helloRaw,
    })

    const frames = frameQueue(port)
    port.postMessage({ frame: b64encode(helloRaw) } satisfies WireMsg)
    port.postMessage({ frame: b64encode(hs.writeMessageA()) } satisfies WireMsg)
    hs.readMessageB(await frames.next())

    if (opts.pinnedDaemonKey) {
      const got = hs.remoteStatic
      if (!constantEqual(got, opts.pinnedDaemonKey)) {
        port.disconnect()
        throw new VaultError('STATIC_KEY_MISMATCH', 'daemon identity changed')
      }
    }
    port.postMessage({ frame: b64encode(hs.writeMessageC()) } satisfies WireMsg)

    const transport = new VaultTransport(port, hs.split(), hs.remoteStatic)
    frames.handoff((frame) => transport.onFrame(frame), (err) => transport.onClosed(err))
    return transport
  }

  private onFrame(frame: Uint8Array): void {
    let resp: ResponseEnvelope
    try {
      resp = JSON.parse(td.decode(this.recv.decryptWithAd(new Uint8Array(0), frame)))
    } catch {
      this.onClosed(new VaultError('TRANSPORT', 'transport authentication failed'))
      this.port.disconnect()
      return
    }
    const waiter = this.pending.get(resp.requestId)
    if (waiter) {
      this.pending.delete(resp.requestId)
      waiter.resolve(resp)
    }
  }

  private onClosed(err: Error): void {
    if (this.closed) return
    this.closed = true
    for (const [, waiter] of this.pending) waiter.reject(err)
    this.pending.clear()
  }

  get isClosed(): boolean {
    return this.closed
  }

  async call<T>(operation: string, payload?: unknown): Promise<T> {
    if (this.closed) throw new VaultError('DISCONNECTED', 'session closed')
    const requestId = `ext-${++this.counter}`
    const env: Envelope = { protocolVersion: 1, requestId, operation }
    if (payload !== undefined) env.payload = payload

    const resp = await new Promise<ResponseEnvelope>((resolve, reject) => {
      this.pending.set(requestId, { resolve, reject })
      const ct = this.send.encryptWithAd(new Uint8Array(0), te.encode(JSON.stringify(env)))
      this.port.postMessage({ frame: b64encode(ct) } satisfies WireMsg)
    })
    if (!resp.ok) {
      const e = resp.error ?? { code: 'INTERNAL', message: 'unknown failure' }
      throw new VaultError(e.code, e.message)
    }
    return resp.data as T
  }

  close(): void {
    this.onClosed(new VaultError('DISCONNECTED', 'session closed'))
    this.port.disconnect()
  }
}

/** Buffers incoming frames during the handshake, then hands off to a sink. */
function frameQueue(port: Port) {
  const queue: Uint8Array[] = []
  const waiters: Array<{ resolve: (f: Uint8Array) => void; reject: (e: Error) => void }> = []
  let sink: ((f: Uint8Array) => void) | null = null
  let closeSink: ((e: Error) => void) | null = null
  let closedErr: Error | null = null

  port.onMessage.addListener((raw) => {
    const msg = raw as WireMsg
    if (msg.error) {
      fail(new VaultError('RELAY', msg.error))
      return
    }
    if (!msg.frame) return
    const frame = b64decode(msg.frame)
    if (sink) sink(frame)
    else if (waiters.length > 0) waiters.shift()!.resolve(frame)
    else queue.push(frame)
  })
  port.onDisconnect.addListener(() => fail(new VaultError('DISCONNECTED', 'native host disconnected')))

  function fail(err: Error): void {
    closedErr = err
    while (waiters.length > 0) waiters.shift()!.reject(err)
    closeSink?.(err)
  }

  return {
    next(): Promise<Uint8Array> {
      const buffered = queue.shift()
      if (buffered) return Promise.resolve(buffered)
      if (closedErr) return Promise.reject(closedErr)
      return new Promise((resolve, reject) => waiters.push({ resolve, reject }))
    },
    handoff(onFrame: (f: Uint8Array) => void, onClose: (e: Error) => void): void {
      sink = onFrame
      closeSink = onClose
      while (queue.length > 0) onFrame(queue.shift()!)
      if (closedErr) onClose(closedErr)
    },
  }
}

function constantEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  let d = 0
  for (let i = 0; i < a.length; i++) d |= a[i]! ^ b[i]!
  return d === 0
}
