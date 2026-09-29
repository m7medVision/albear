// Human copy for the error codes the background forwards from vaultd
// (internal/adapters/protocol) and from the transport. The popup never shows a
// raw code: every message says what happened and what to do next.
const MESSAGES: Record<string, string> = {
  DISCONNECTED: 'Unable to reach vaultd. Check that it is running, then try again.',
  RELAY: 'Unable to reach vaultd. Check that it is running, then try again.',
  VAULT_LOCKED: 'The vault is locked. Unlock it, then try again.',
  AUTH_FAILED: 'Wrong master password. Check it and try again.',
  RATE_LIMITED: 'Too many attempts. Wait a minute, then try again.',
  DENIED: 'vaultd refused this browser. If you removed it from your paired clients, pair it again.',
  ALREADY_EXISTS: 'A login with this name already exists for this site. Choose a different name.',
  INVALID_REQUEST: 'vaultd could not accept this. Check the fields, then try again.',
  NOT_INITIALIZED: 'There is no vault yet. Run `vault init` in a terminal, then try again.',
  CONFLICT: 'This login changed somewhere else. Reopen the popup, then try again.',
  INTEGRITY_FAILURE: 'vaultd found a problem and locked the vault. Unlock it to continue.',
}

const FALLBACK = 'Reopen the popup and try again. If it keeps failing, restart vaultd.'

/** Error code carried by a rejected background call (`Error(code)`), or the raw value. */
export function errorCode(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e)
  return raw.replace(/^Error:\s*/, '')
}

/** A recoverable, human sentence for any failure from the background. */
export function describeError(e: unknown): string {
  return MESSAGES[errorCode(e)] ?? FALLBACK
}
