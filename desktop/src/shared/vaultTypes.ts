// Types shared between the Electron main process, the preload bridge and the
// renderer. They mirror the vaultd wire DTOs (internal/daemon/dto.go) and the
// protocol envelopes (internal/adapters/protocol/protocol.go).

/** JSON response envelope decrypted from a Noise frame. */
export interface WireResponse {
  protocolVersion: number;
  requestId: string;
  ok: boolean;
  data?: unknown;
  error?: { code: string; message: string };
}

export interface AlbearErrorInfo {
  code: string;
  message: string;
}

/**
 * Every IPC handler resolves to this shape instead of rejecting, so the
 * renderer gets clean, typed error codes rather than serialized exceptions.
 */
export type AlbearResult<T> =
  { ok: true; data: T } | { ok: false; error: AlbearErrorInfo };

/** vault.status data, extended with local daemon reachability. */
export interface DesktopStatus {
  available: boolean;
  initialized?: boolean;
  unlocked?: boolean;
  epoch?: number;
  recordCount?: number;
}

/** Redacted record metadata (records.list / records.search / records.show). */
export interface RecordView {
  id: string;
  type: string;
  revision: number;
  name: string;
  username?: string;
  service?: string;
  environment?: string;
  urls?: string[];
  tags?: string[];
  createdAtMs: number;
  updatedAtMs: number;
}

/** Revealed secret payload (records.reveal). */
export interface SecretView {
  password?: string;
  notes?: string;
  apiKey?: string;
  apiSecret?: string;
  custom?: Record<string, string>;
}

/** password.generate options; omit for daemon defaults. */
export interface GenerateOptions {
  length?: number;
  upper?: boolean;
  lower?: boolean;
  digits?: boolean;
  symbols?: boolean;
}

// Error codes produced locally (never by the daemon). Daemon codes are the
// PRD wire codes: VAULT_LOCKED, AUTH_FAILED, NOT_FOUND, DENIED,
// INTEGRITY_FAILURE, CONFLICT, INVALID_REQUEST, RATE_LIMITED, ALREADY_EXISTS,
// NOT_INITIALIZED, INTERNAL.
export const DAEMON_UNAVAILABLE = 'DAEMON_UNAVAILABLE';
export const TRANSPORT_FAILED = 'TRANSPORT';
export const REQUEST_TIMEOUT = 'TIMEOUT';
