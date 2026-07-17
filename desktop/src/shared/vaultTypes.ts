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

/** The three record types the daemon accepts (internal/records/domain). */
export type RecordType = 'login' | 'note' | 'api';

export const RECORD_TYPES: RecordType[] = ['login', 'note', 'api'];

/**
 * One URL and its matching policy. `sub` opts the URL into matching hosts at or
 * under itself; absent means exact. The daemon defaults an entry with no policy
 * to exact, so an editor that saves plain `urls` silently resets every opt-in —
 * always round-trip entries.
 */
export interface UrlEntry {
  url: string;
  sub?: boolean;
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
  /** Addresses only. Read `urlEntries` when the matching policy matters. */
  urls?: string[];
  /** Each URL with its subdomain policy. This is what an editor must write back. */
  urlEntries?: UrlEntry[];
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

/**
 * Record fields submitted to records.create / records.update, mirroring
 * `recordFields` in internal/daemon/dto.go.
 *
 * The daemon rebuilds the whole record from this payload — it does not patch.
 * An omitted secret field is stored as empty, over the top of the old value, so
 * an update MUST carry every secret the record should keep. Reveal first.
 */
export interface RecordFields {
  type?: RecordType;
  name: string;
  username?: string;
  service?: string;
  environment?: string;
  urlEntries?: UrlEntry[];
  tags?: string[];
  password?: string;
  notes?: string;
  apiKey?: string;
  apiSecret?: string;
  custom?: Record<string, string>;
}

/** records.update payload: fields plus the revision the edit was based on. */
export interface UpdateFields extends RecordFields {
  id: string;
  expectedRevision: number;
}

/** password.generate options; omit for daemon defaults. */
export interface GenerateOptions {
  length?: number;
  upper?: boolean;
  lower?: boolean;
  digits?: boolean;
  symbols?: boolean;
}

/**
 * A client awaiting pairing approval (clients.pending). `capabilities` is what
 * approval would actually grant, resolved daemon-side from the kind — the
 * operator consents to the privilege, not to a label and a phrase.
 */
export interface PendingPairingView {
  pairingId: string;
  kind: number;
  kindName: string;
  label: string;
  phrase: string;
  capabilities: string[];
}

/** ClientStatus in internal/access/domain/client.go. */
export const STATUS_PENDING = 1;
export const STATUS_APPROVED = 2;
export const STATUS_REVOKED = 3;

export function clientStatusName(status: number): string {
  switch (status) {
    case STATUS_PENDING:
      return 'pending';
    case STATUS_APPROVED:
      return 'approved';
    case STATUS_REVOKED:
      return 'revoked';
    default:
      return 'unknown';
  }
}

/**
 * A registered client (clients.list). Unlike a pending pairing this carries no
 * capability list: the daemon does not project one here, and deriving it
 * client-side from `kind` would be the UI inventing an authorization fact.
 */
export interface ClientView {
  id: string;
  kind: number;
  status: number;
  label: string;
  lastSeenMs?: number;
}

/**
 * One recorded security event (events.recent). `severity` and `code` are the
 * daemon's integer enums. `details` carries no secret: events are bound by the
 * rule that secrets never reach logs, enforced where they are written.
 */
export interface EventView {
  sequence: number;
  occurredMs: number;
  severity: number;
  code: number;
  details?: string;
}

/** Severity in internal/security/domain/event.go. */
export const SEVERITY_INFO = 1;
export const SEVERITY_WARNING = 2;
export const SEVERITY_CRITICAL = 3;

export function severityName(severity: number): string {
  switch (severity) {
    case SEVERITY_INFO:
      return 'info';
    case SEVERITY_WARNING:
      return 'warning';
    case SEVERITY_CRITICAL:
      return 'critical';
    default:
      return 'unknown';
  }
}

/**
 * EventCode names, mirroring internal/security/domain/event.go. The codes are
 * iota-derived from 100 and documented there as append-only — history is stored
 * as integers, so a code never changes meaning and this map stays valid.
 * An unmapped code renders as its number rather than being hidden.
 */
const EVENT_NAMES: Record<number, string> = {
  100: 'vault created',
  101: 'vault unlocked',
  102: 'vault locked',
  103: 'vault panic-locked',
  104: 'master password changed',
  105: 'unlock failed',
  106: 'unlock rate limited',
  107: 'client pairing requested',
  108: 'client approved',
  109: 'client revoked',
  110: 'client auto-approved',
  111: 'unauthorized request',
  112: 'integrity failure',
  113: 'transport handshake failed',
  114: 'protocol violation',
  115: 'backup created',
  116: 'backup restored',
  117: 'vault destroyed',
  118: 'vault state bootstrapped',
};

export function eventName(code: number): string {
  return EVENT_NAMES[code] ?? `event ${code}`;
}

/** backup.verify result: the container's authenticated header. */
export interface BackupInfo {
  vaultId: string;
  createdAtMs: number;
  snapshotLen: number;
}

/** backup.create result. */
export interface BackupResult {
  path: string;
}

// Error codes produced locally (never by the daemon). Daemon codes are the
// PRD wire codes: VAULT_LOCKED, AUTH_FAILED, NOT_FOUND, DENIED,
// INTEGRITY_FAILURE, CONFLICT, INVALID_REQUEST, RATE_LIMITED, ALREADY_EXISTS,
// NOT_INITIALIZED, INTERNAL.
export const DAEMON_UNAVAILABLE = 'DAEMON_UNAVAILABLE';
export const TRANSPORT_FAILED = 'TRANSPORT';
export const REQUEST_TIMEOUT = 'TIMEOUT';
