// IPC surface for the vault: every renderer-facing operation is a
// `ipcMain.handle` channel that resolves to an AlbearResult — handlers never
// reject, so the renderer gets typed error codes instead of serialized
// exception text. Input from the renderer is validated here; the socket and
// Noise state never leave the main process.

import { ipcMain, clipboard } from 'electron';
import { VaultClient, VaultError } from './vaultClient';
import {
  AlbearResult,
  DesktopStatus,
  GenerateOptions,
  RecordView,
  SecretView,
  DAEMON_UNAVAILABLE,
  REQUEST_TIMEOUT,
} from '../shared/vaultTypes';

// Unlocking runs the Argon2 KDF in the daemon; give it more headroom.
const UNLOCK_TIMEOUT_MS = 60_000;

// Clear the clipboard a while after copying a secret, unless the user has
// copied something else in the meantime.
const CLIPBOARD_CLEAR_MS = 45_000;

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
