// Background service worker: sole owner of the Noise session with vaultd.
// The popup and content scripts talk to this worker over extension messaging;
// secrets are returned only to the specific requesting tab (PRD 13.2).
import { generateKeyPair } from '../noise/noise'
import {
  clearIdentity,
  fromHex,
  identityPSK,
  identityStaticKey,
  loadIdentity,
  saveIdentity,
  toHex,
} from '../messaging/identity'
import { clearCapture, consumeCapture, stashCapture } from '../messaging/capture'
import {
  clearPairing,
  loadPairing,
  pairingStaticKey,
  pairingToStored,
  savePairing,
  type StoredPairing,
} from '../messaging/pairing'
import { NATIVE_HOST, VaultError, VaultTransport, type Port } from '../messaging/transport'

let session: VaultTransport | null = null
let connecting: Promise<VaultTransport> | null = null

function nativePort(): Port {
  return chrome.runtime.connectNative(NATIVE_HOST) as unknown as Port
}

async function connectPaired(): Promise<VaultTransport> {
  const id = await loadIdentity()
  if (!id) throw new VaultError('NOT_PAIRED', 'extension is not paired')
  return VaultTransport.connect(nativePort(), {
    mode: 'paired',
    clientId: id.clientId,
    staticKey: identityStaticKey(id),
    psk: await identityPSK(id),
    pinnedDaemonKey: fromHex(id.daemonStaticKeyHex),
  })
}

async function getSession(): Promise<VaultTransport> {
  if (session && !session.isClosed) return session
  connecting ??= connectPaired().finally(() => {
    connecting = null
  })
  session = await connecting
  return session
}

// ---- pairing -------------------------------------------------------------

interface PairingState {
  transport: VaultTransport
  pairingId: string
  staticPrivHex: string
}
let pairing: PairingState | null = null

async function startPairing(): Promise<{ phrase: string }> {
  const staticKey = generateKeyPair()
  const transport = await VaultTransport.connect(nativePort(), { mode: 'pair', staticKey })
  const resp = await transport.call<{ pairingId: string; phrase: string }>('clients.pair', {
    kind: 2, // ChromeExtension
    label: 'chrome-extension',
    staticKey: toHex(staticKey.publicKey),
  })
  pairing = {
    transport,
    pairingId: resp.pairingId,
    staticPrivHex: toHex(staticKey.privateKey),
  }
  // Persist so the claim survives SW death and popup reopens. Static priv
  // stays on disk; same trust class as the paired identity (PRD 12.4).
  await savePairing(pairingToStored({ pairingId: resp.pairingId, staticKey, phrase: resp.phrase }))
  return { phrase: resp.phrase }
}

async function openPairingTransport(stored: StoredPairing): Promise<VaultTransport> {
  return VaultTransport.connect(nativePort(), {
    mode: 'pair',
    staticKey: pairingStaticKey(stored),
  })
}

async function claimPairing(): Promise<boolean> {
  if (!pairing) {
    // SW was reaped (or popup reopened): recover the pairing from storage
    // and re-handshake with the same static key. The daemon's claim handler
    // looks up the pending entry by pairingId; the handshake's remote
    // static only matters for clients.pair, not claim.
    const stored = await loadPairing()
    if (!stored) throw new VaultError('NOT_PAIRED', 'no pairing in progress')
    const transport = await openPairingTransport(stored)
    pairing = { transport, pairingId: stored.pairingId, staticPrivHex: stored.staticPrivHex }
  }
  try {
    const resp = await pairing.transport.call<{
      clientId: string
      credential: string
      daemonStaticKey: string
    }>('clients.claim', { pairingId: pairing.pairingId })
    await saveIdentity({
      clientId: resp.clientId,
      credentialHex: resp.credential,
      daemonStaticKeyHex: resp.daemonStaticKey,
      staticPrivHex: pairing.staticPrivHex,
    })
    pairing.transport.close()
    pairing = null
    await clearPairing()
    return true
  } catch (e) {
    if (e instanceof VaultError && e.code === 'DENIED') return false // not approved yet
    throw e
  }
}

async function resetPairing(): Promise<void> {
  await clearIdentity()
  await clearPairing()
  session?.close()
  session = null
  if (pairing) {
    pairing.transport.close()
    pairing = null
    return
  }
  // No in-memory pairing: tell the daemon to drop any pending entry that
  // we persisted earlier (so the CLI's pending list stays clean).
  const stored = await loadPairing()
  if (!stored) return
  try {
    const transport = await openPairingTransport(stored)
    await transport.call('clients.cancel', { pairingId: stored.pairingId })
    transport.close()
  } catch {
    // Daemon unreachable or the pairing was already cleaned up; local
    // state is cleared regardless.
  }
}

// ---- request handling ------------------------------------------------------

interface BgRequest {
  kind: string
  [k: string]: unknown
}

// Content scripts may only ask for matches, fill reveals, save/update
// prompts, and the (non-secret) vault status check used to gate the save
// offer. The origin used is ALWAYS the sender's real tab origin, never a
// value the page supplied (PRD 13.2).
export const contentAllowed = new Set([
  'status',
  'records.matchForTab',
  'records.revealForFill',
  'records.saveLogin',
  'records.updateLogin',
  'records.stashCapture',
  'records.consumeCapture',
  'records.clearCapture',
])

chrome.runtime.onMessage.addListener(
  (msg: BgRequest, sender: chrome.runtime.MessageSender, sendResponse: (r: unknown) => void) => {
    handle(msg, sender)
      .then((data) => sendResponse({ ok: true, data }))
      .catch((e: unknown) => {
        const err =
          e instanceof VaultError
            ? { code: e.code, message: e.message }
            : { code: 'INTERNAL', message: 'internal failure' }
        sendResponse({ ok: false, error: err })
      })
    return true // async response
  },
)

function senderOrigin(sender: chrome.runtime.MessageSender): string {
  const url = sender.tab?.url ?? sender.url
  if (!url) throw new VaultError('DENIED', 'no sender origin')
  return new URL(url).origin
}

async function handle(msg: BgRequest, sender: chrome.runtime.MessageSender): Promise<unknown> {
  const fromContent = sender.tab !== undefined
  if (fromContent && !contentAllowed.has(msg.kind)) {
    throw new VaultError('DENIED', 'operation not allowed from content scripts')
  }

  switch (msg.kind) {
    // Popup-only operations.
    case 'status': {
      const id = await loadIdentity()
      if (!id) return { paired: false }
      try {
        const t = await getSession()
        const st = await t.call<{ initialized: boolean; unlocked: boolean }>('vault.status')
        return { paired: true, connected: true, ...st }
      } catch (e) {
        if (e instanceof VaultError && (e.code === 'DISCONNECTED' || e.code === 'RELAY')) {
          return { paired: true, connected: false }
        }
        throw e
      }
    }
    case 'pair.start':
      return startPairing()
    case 'pair.claim':
      return { done: await claimPairing() }
    case 'pair.reset':
      await resetPairing()
      return {}
    case 'pair.get': {
      const stored = await loadPairing()
      return { pairing: stored ? { phrase: stored.phrase } : null }
    }
    case 'unlock':
      return (await getSession()).call('vault.unlock', { password: msg['password'] })
    case 'lock':
      return (await getSession()).call('vault.lock')
    case 'match': {
      // Popup match: for the active tab's origin.
      const t = await getSession()
      return t.call('records.match', { origin: msg['origin'] })
    }
    case 'generate':
      return (await getSession()).call('password.generate', { default: true })
    case 'records.createForOrigin': {
      // Popup-only: lets the user add a credential without performing a
      // real login. The origin comes from the popup, not the page, and the
      // kind is intentionally absent from contentAllowed.
      const v = validateCreateForOrigin(msg)
      if (!v.ok) throw new VaultError(v.error.code, v.error.message)
      const t = await getSession()
      return t.call('records.createLogin', v.payload)
    }

    // Content-script operations: origin is derived from the sender.
    case 'records.matchForTab': {
      const t = await getSession()
      return t.call('records.match', { origin: senderOrigin(sender) })
    }
    case 'records.revealForFill': {
      const t = await getSession()
      // Daemon re-validates that the record actually matches this origin.
      return t.call('records.revealForOrigin', {
        id: msg['id'],
        origin: senderOrigin(sender),
      })
    }
    case 'records.saveLogin': {
      const t = await getSession()
      return t.call('records.createLogin', {
        type: 'login',
        name: msg['name'] || new URL(senderOrigin(sender)).hostname,
        username: msg['username'],
        password: msg['password'],
        urls: [senderOrigin(sender)],
      })
    }
    case 'records.updateLogin': {
      const t = await getSession()
      return t.call('records.updateLogin', {
        id: msg['id'],
        expectedRevision: msg['expectedRevision'],
        type: 'login',
        name: msg['name'] || new URL(senderOrigin(sender)).hostname,
        username: msg['username'],
        password: msg['password'],
        urls: [senderOrigin(sender)],
      })
    }

    // Survives a full-page navigation right after submit: the site's own
    // login handler often navigates away before the async save-offer chain
    // below would otherwise finish rendering into the (about to be torn
    // down) document.
    case 'records.stashCapture': {
      const tabId = sender.tab?.id
      if (tabId === undefined) throw new VaultError('DENIED', 'no sender tab')
      await stashCapture(tabId, {
        origin: senderOrigin(sender),
        username: String(msg['username'] ?? ''),
        password: String(msg['password'] ?? ''),
      })
      return {}
    }
    case 'records.consumeCapture': {
      const tabId = sender.tab?.id
      if (tabId === undefined) return null
      return consumeCapture(tabId, senderOrigin(sender))
    }
    case 'records.clearCapture': {
      const tabId = sender.tab?.id
      if (tabId !== undefined) await clearCapture(tabId)
      return {}
    }
  }
  throw new VaultError('INVALID_REQUEST', `unknown request ${msg.kind}`)
}

// ---- validation -----------------------------------------------------------
//
// Pure helpers, exported for unit testing. They never reach into chrome.* or
// the daemon — they only normalize a message into a daemon payload or an
// error, so the handlers stay thin and the wire format is pinned by tests.

export interface CreateLoginPayload {
  type: 'login'
  name: string
  username: string
  password: string
  urls: string[]
}

export type ValidationResult<T> =
  | { ok: true; payload: T }
  | { ok: false; error: { code: string; message: string } }

export function validateCreateForOrigin(msg: Record<string, unknown>): ValidationResult<CreateLoginPayload> {
  const origin = typeof msg['origin'] === 'string' ? msg['origin'] : ''
  let u: URL
  try {
    u = new URL(origin)
  } catch {
    return { ok: false, error: { code: 'INVALID_REQUEST', message: 'bad origin' } }
  }
  if (u.protocol !== 'https:') {
    return { ok: false, error: { code: 'INVALID_REQUEST', message: 'origin must be https' } }
  }
  const username = typeof msg['username'] === 'string' ? msg['username'] : ''
  const password = typeof msg['password'] === 'string' ? msg['password'] : ''
  if (!username || !password) {
    return { ok: false, error: { code: 'INVALID_REQUEST', message: 'username and password are required' } }
  }
  const name = typeof msg['name'] === 'string' && msg['name'] ? msg['name'] : u.hostname
  return {
    ok: true,
    payload: { type: 'login', name, username, password, urls: [u.origin] },
  }
}
