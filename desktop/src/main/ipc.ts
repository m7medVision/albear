// IPC surface for the vault: every renderer-facing operation is a
// `ipcMain.handle` channel that resolves to an AlbearResult — handlers never
// reject, so the renderer gets typed error codes instead of serialized
// exception text. Input from the renderer is validated here; the socket and
// Noise state never leave the main process.

import { ipcMain, clipboard, dialog, BrowserWindow } from 'electron';
import type { IpcMainInvokeEvent } from 'electron';
import path from 'path';
import { app } from 'electron';
import { VaultClient, VaultError } from './vaultClient';
import {
  AlbearResult,
  BackupInfo,
  BackupResult,
  Canceled,
  ClientView,
  DesktopStatus,
  EventView,
  Restored,
  GenerateOptions,
  PendingPairingView,
  RecordFields,
  RecordView,
  RECORD_TYPES,
  RecordType,
  SecretView,
  UpdateFields,
  UrlEntry,
  DAEMON_UNAVAILABLE,
  REQUEST_TIMEOUT,
} from '../shared/vaultTypes';

// Unlocking runs the Argon2 KDF in the daemon; give it more headroom.
const UNLOCK_TIMEOUT_MS = 60_000;

// Restore authenticates the container and installs a whole database snapshot.
const RESTORE_TIMEOUT_MS = 120_000;

// Clear the clipboard a while after copying a secret, unless the user has
// copied something else in the meantime.
const CLIPBOARD_CLEAR_MS = 45_000;

// events.recent is uncapped daemon-side; this is the client's own ceiling.
const MAX_EVENT_LIMIT = 500;

// Backup containers are `albear-backup-<unixMilli>.abk` (backup.DefaultBackupName).
const BACKUP_EXTENSION = 'abk';

function defaultBackupName(): string {
  return `albear-backup-${Date.now()}.${BACKUP_EXTENSION}`;
}

/**
 * Ask the user for a backup path. Filesystem paths are chosen here and never
 * come from the renderer: the renderer has no filesystem access, and a path it
 * composed would be a path it could aim.
 *
 * Returns undefined when the user cancels, which callers must treat as "do
 * nothing" rather than as an error.
 */
async function choosePath(
  event: IpcMainInvokeEvent,
  mode: 'save' | 'open',
): Promise<string | undefined> {
  const parent = BrowserWindow.fromWebContents(event.sender);
  const filters = [{ name: 'Albear backup', extensions: [BACKUP_EXTENSION] }];
  if (mode === 'save') {
    const res = await (parent
      ? dialog.showSaveDialog(parent, {
          defaultPath: path.join(app.getPath('documents'), defaultBackupName()),
          filters,
        })
      : dialog.showSaveDialog({
          defaultPath: path.join(app.getPath('documents'), defaultBackupName()),
          filters,
        }));
    return res.canceled ? undefined : res.filePath;
  }
  const res = await (parent
    ? dialog.showOpenDialog(parent, { properties: ['openFile'], filters })
    : dialog.showOpenDialog({ properties: ['openFile'], filters }));
  return res.canceled ? undefined : res.filePaths[0];
}

async function toResult<T>(work: () => Promise<T>): Promise<AlbearResult<T>> {
  try {
    return { ok: true, data: await work() };
  } catch (err) {
    if (err instanceof VaultError) {
      return { ok: false, error: { code: err.code, message: err.message } };
    }
    return {
      ok: false,
      error: { code: 'INTERNAL', message: 'unexpected failure' },
    };
  }
}

function requireString(value: unknown, what: string): string {
  if (typeof value !== 'string') {
    throw new VaultError('INVALID_REQUEST', `${what} must be a string`);
  }
  return value;
}

function optionalString(value: unknown, what: string): string | undefined {
  if (value === undefined) return undefined;
  return requireString(value, what);
}

function invalid(what: string): never {
  throw new VaultError('INVALID_REQUEST', what);
}

function requireStringArray(value: unknown, what: string): string[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value)) invalid(`${what} must be an array`);
  return value.map((v, i) => requireString(v, `${what}[${i}]`));
}

// URL entries carry each URL's subdomain policy. They are validated as a whole
// rather than flattened to strings: dropping the policy here would silently
// reset every opt-in to exact matching on the next save.
function requireUrlEntries(value: unknown): UrlEntry[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value)) invalid('urlEntries must be an array');
  return value.map((raw, i) => {
    if (typeof raw !== 'object' || raw === null) {
      invalid(`urlEntries[${i}] must be an object`);
    }
    const e = raw as Record<string, unknown>;
    if (e.sub !== undefined && typeof e.sub !== 'boolean') {
      invalid(`urlEntries[${i}].sub must be a boolean`);
    }
    const entry: UrlEntry = { url: requireString(e.url, `urlEntries[${i}].url`) };
    if (e.sub) entry.sub = true;
    return entry;
  });
}

function requireCustom(value: unknown): Record<string, string> | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    invalid('custom must be an object');
  }
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
    if (k.length === 0) invalid('custom keys must not be empty');
    out[k] = requireString(v, `custom[${k}]`);
  }
  return out;
}

function requireRecordType(value: unknown): RecordType | undefined {
  if (value === undefined) return undefined;
  const t = requireString(value, 'type');
  if (!RECORD_TYPES.includes(t as RecordType)) invalid('unknown record type');
  return t as RecordType;
}

/**
 * Shape-check a record payload from the renderer. This is defence in depth, not
 * the authorization boundary: the daemon validates everything again and is the
 * only authority. What it buys is that a malformed payload fails here with a
 * clear code instead of becoming a half-formed record request.
 *
 * Secret fields pass through as given — including empty strings, which are a
 * deliberate clear. The daemon replaces the whole record on update, so the
 * caller is responsible for sending every secret it wants kept.
 */
function requireRecordFields(value: unknown): RecordFields {
  if (typeof value !== 'object' || value === null) {
    invalid('record fields must be an object');
  }
  const f = value as Record<string, unknown>;
  const name = requireString(f.name, 'name');
  if (name.trim().length === 0) invalid('name is required');

  const fields: RecordFields = { name };
  const type = requireRecordType(f.type);
  if (type) fields.type = type;

  const username = optionalString(f.username, 'username');
  if (username !== undefined) fields.username = username;
  const service = optionalString(f.service, 'service');
  if (service !== undefined) fields.service = service;
  const environment = optionalString(f.environment, 'environment');
  if (environment !== undefined) fields.environment = environment;
  const password = optionalString(f.password, 'password');
  if (password !== undefined) fields.password = password;
  const notes = optionalString(f.notes, 'notes');
  if (notes !== undefined) fields.notes = notes;
  const apiKey = optionalString(f.apiKey, 'apiKey');
  if (apiKey !== undefined) fields.apiKey = apiKey;
  const apiSecret = optionalString(f.apiSecret, 'apiSecret');
  if (apiSecret !== undefined) fields.apiSecret = apiSecret;

  const urlEntries = requireUrlEntries(f.urlEntries);
  if (urlEntries !== undefined) fields.urlEntries = urlEntries;
  const tags = requireStringArray(f.tags, 'tags');
  if (tags !== undefined) fields.tags = tags;
  const custom = requireCustom(f.custom);
  if (custom !== undefined) fields.custom = custom;

  return fields;
}

function requireUpdateFields(value: unknown): UpdateFields {
  const fields = requireRecordFields(value);
  const f = value as Record<string, unknown>;
  const revision = f.expectedRevision;
  if (typeof revision !== 'number' || !Number.isInteger(revision) || revision < 0) {
    invalid('expectedRevision must be a non-negative integer');
  }
  return {
    ...fields,
    id: requireString(f.id, 'id'),
    expectedRevision: revision,
  };
}

export function registerVaultIpc(client: VaultClient): void {
  ipcMain.handle(
    'albear:status',
    (): Promise<AlbearResult<DesktopStatus>> =>
      toResult(async () => {
        try {
          const st = await client.call<{
            initialized: boolean;
            unlocked: boolean;
            epoch: number;
            recordCount: number;
          }>('vault.status');
          return { available: true, ...st };
        } catch (err) {
          if (
            err instanceof VaultError &&
            (err.code === DAEMON_UNAVAILABLE || err.code === REQUEST_TIMEOUT)
          ) {
            return { available: false };
          }
          throw err;
        }
      }),
  );

  ipcMain.handle(
    'albear:unlock',
    (_event, password: unknown): Promise<AlbearResult<unknown>> =>
      toResult(() => {
        const pw = requireString(password, 'password');
        if (pw.length === 0) {
          throw new VaultError('INVALID_REQUEST', 'password is required');
        }
        return client.call('vault.unlock', { password: pw }, UNLOCK_TIMEOUT_MS);
      }),
  );

  ipcMain.handle(
    'albear:lock',
    (): Promise<AlbearResult<unknown>> =>
      toResult(() => client.call('vault.lock')),
  );

  // Panic lock is vault.lock with a different audit code — the daemon routes
  // both through the same Lock(). The distinction is the recorded event, which
  // is exactly why a misclick matters: it writes a panic that never happened.
  ipcMain.handle(
    'albear:panic',
    (): Promise<AlbearResult<unknown>> =>
      toResult(() => client.call('vault.panic')),
  );

  // Creating the vault runs the same Argon2 derivation as unlock.
  ipcMain.handle(
    'albear:init',
    (_event, password: unknown): Promise<AlbearResult<unknown>> =>
      toResult(() => {
        const pw = requireString(password, 'password');
        if (pw.length === 0) invalid('password is required');
        return client.call('vault.init', { password: pw }, UNLOCK_TIMEOUT_MS);
      }),
  );

  // Changing the password re-wraps the vault key: two derivations, so it gets
  // the same headroom as unlock rather than the default timeout.
  ipcMain.handle(
    'albear:changePassword',
    (_event, current: unknown, next: unknown): Promise<AlbearResult<unknown>> =>
      toResult(() => {
        const cur = requireString(current, 'current password');
        const nxt = requireString(next, 'new password');
        if (cur.length === 0 || nxt.length === 0) {
          invalid('both the current and the new password are required');
        }
        return client.call(
          'vault.changePassword',
          { current: cur, next: nxt },
          UNLOCK_TIMEOUT_MS,
        );
      }),
  );

  ipcMain.handle(
    'albear:clientsPending',
    (): Promise<AlbearResult<{ pending: PendingPairingView[] }>> =>
      toResult(() => client.call('clients.pending')),
  );

  ipcMain.handle(
    'albear:clientsList',
    (): Promise<AlbearResult<{ clients: ClientView[] }>> =>
      toResult(() => client.call('clients.list')),
  );

  ipcMain.handle(
    'albear:clientsApprove',
    (_event, pairingId: unknown): Promise<AlbearResult<unknown>> =>
      toResult(() =>
        client.call('clients.approve', {
          pairingId: requireString(pairingId, 'pairingId'),
        }),
      ),
  );

  ipcMain.handle(
    'albear:clientsRevoke',
    (_event, id: unknown): Promise<AlbearResult<unknown>> =>
      toResult(() =>
        client.call('clients.revoke', { id: requireString(id, 'id') }),
      ),
  );

  // The daemon defaults to 50 and caps nothing, so the ceiling is ours to pick.
  ipcMain.handle(
    'albear:events',
    (_event, limit?: unknown): Promise<AlbearResult<{ events: EventView[] }>> =>
      toResult(() => {
        if (limit === undefined) return client.call('events.recent', {});
        if (
          typeof limit !== 'number' ||
          !Number.isInteger(limit) ||
          limit <= 0 ||
          limit > MAX_EVENT_LIMIT
        ) {
          invalid(`limit must be an integer between 1 and ${MAX_EVENT_LIMIT}`);
        }
        return client.call('events.recent', { limit });
      }),
  );

  ipcMain.handle(
    'albear:list',
    (): Promise<AlbearResult<{ records: RecordView[] }>> =>
      toResult(() => client.call('records.list')),
  );

  ipcMain.handle(
    'albear:search',
    (
      _event,
      query: unknown,
    ): Promise<AlbearResult<{ records: RecordView[] }>> =>
      toResult(() =>
        client.call('records.search', {
          query: requireString(query, 'query'),
        }),
      ),
  );

  ipcMain.handle(
    'albear:show',
    (_event, ref: unknown): Promise<AlbearResult<RecordView>> =>
      toResult(() =>
        client.call('records.show', { ref: requireString(ref, 'ref') }),
      ),
  );

  ipcMain.handle(
    'albear:reveal',
    (_event, ref: unknown): Promise<AlbearResult<SecretView>> =>
      toResult(() =>
        client.call('records.reveal', { ref: requireString(ref, 'ref') }),
      ),
  );

  ipcMain.handle(
    'albear:create',
    (_event, fields: unknown): Promise<AlbearResult<{ id: string }>> =>
      toResult(() => client.call('records.create', requireRecordFields(fields))),
  );

  // records.update replaces the record rather than patching it, so this payload
  // must carry every secret the record should keep. The renderer reveals first;
  // main does not merge on its behalf, because it cannot tell an untouched
  // field from a deliberately cleared one either.
  ipcMain.handle(
    'albear:update',
    (_event, fields: unknown): Promise<AlbearResult<unknown>> =>
      toResult(() => client.call('records.update', requireUpdateFields(fields))),
  );

  ipcMain.handle(
    'albear:delete',
    (_event, id: unknown): Promise<AlbearResult<unknown>> =>
      toResult(() =>
        client.call('records.delete', { id: requireString(id, 'id') }),
      ),
  );

  ipcMain.handle(
    'albear:generate',
    (
      _event,
      options?: GenerateOptions,
    ): Promise<AlbearResult<{ password: string }>> =>
      toResult(() =>
        client.call(
          'password.generate',
          options === undefined ? undefined : options,
        ),
      ),
  );

  ipcMain.handle(
    'albear:backupCreate',
    (event): Promise<AlbearResult<BackupResult | Canceled>> =>
      toResult(async () => {
        const dest = await choosePath(event, 'save');
        if (!dest) return { canceled: true as const };
        return client.call<BackupResult>('backup.create', { path: dest });
      }),
  );

  ipcMain.handle(
    'albear:backupVerify',
    (event): Promise<AlbearResult<BackupInfo | Canceled>> =>
      toResult(async () => {
        const src = await choosePath(event, 'open');
        if (!src) return { canceled: true as const };
        return client.call<BackupInfo>('backup.verify', { path: src });
      }),
  );

  // Restore replaces the whole vault database and then locks it, so the
  // confirmation lives here rather than in the renderer: the chosen path never
  // leaves this process, and a native modal for an irreversible action cannot
  // be dressed up by renderer DOM. The daemon re-authenticates the container
  // itself before installing a byte of it.
  ipcMain.handle(
    'albear:backupRestore',
    (event): Promise<AlbearResult<Restored | Canceled>> =>
      toResult(async () => {
        const src = await choosePath(event, 'open');
        if (!src) return { canceled: true as const };
        const parent = BrowserWindow.fromWebContents(event.sender);
        const opts = {
          type: 'warning' as const,
          buttons: ['Cancel', 'Replace vault'],
          defaultId: 0,
          cancelId: 0,
          title: 'Restore backup',
          message: `Replace this vault with ${path.basename(src)}?`,
          detail:
            'Every record in the current vault is replaced by the contents of the backup. ' +
            'The current database is kept alongside it as a .recovery file. ' +
            'The vault will lock, and you will need to unlock it again afterwards.',
        };
        const { response } = await (parent
          ? dialog.showMessageBox(parent, opts)
          : dialog.showMessageBox(opts));
        if (response !== 1) return { canceled: true as const };
        await client.call('backup.restore', { path: src }, RESTORE_TIMEOUT_MS);
        return { restored: true as const };
      }),
  );

  // Clipboard lives in the main process: sandboxed preloads cannot use the
  // Electron clipboard module, and navigator.clipboard needs focus + a
  // secure context. Auto-clears after a delay if untouched.
  ipcMain.handle(
    'albear:copy',
    (_event, text: unknown): Promise<AlbearResult<unknown>> =>
      toResult(async () => {
        const value = requireString(text, 'text');
        clipboard.writeText(value);
        setTimeout(() => {
          if (clipboard.readText() === value) clipboard.clear();
        }, CLIPBOARD_CLEAR_MS);
        return {};
      }),
  );
}
