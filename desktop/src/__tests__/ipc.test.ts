/**
 * @jest-environment node
 */
// Renderer input validation at the IPC boundary.
//
// The daemon validates everything again and is the only authority here, so
// these tests are not proving authorization. What they pin is that a malformed
// payload is rejected with a typed code *before* it becomes a daemon request,
// and — for update — that the payload the daemon receives still carries every
// secret, since records.update replaces the record rather than patching it.

const handlers = new Map<string, (...args: unknown[]) => unknown>();

jest.mock('electron', () => ({
  ipcMain: {
    handle: (channel: string, fn: (...args: unknown[]) => unknown) => {
      handlers.set(channel, fn);
    },
  },
  clipboard: { writeText: jest.fn(), readText: jest.fn(), clear: jest.fn() },
  dialog: { showSaveDialog: jest.fn(), showOpenDialog: jest.fn() },
}));

// eslint-disable-next-line import/first
import { registerVaultIpc } from '../main/ipc';
// eslint-disable-next-line import/first
import type { VaultClient } from '../main/vaultClient';
// eslint-disable-next-line import/first
import type { AlbearResult } from '../shared/vaultTypes';

let call: jest.Mock;

function invoke(channel: string, ...args: unknown[]): Promise<AlbearResult<unknown>> {
  const fn = handlers.get(channel);
  if (!fn) throw new Error(`no handler registered for ${channel}`);
  return fn({}, ...args) as Promise<AlbearResult<unknown>>;
}

function expectRejected(result: AlbearResult<unknown>): void {
  expect(result.ok).toBe(false);
  if (!result.ok) expect(result.error.code).toBe('INVALID_REQUEST');
  expect(call).not.toHaveBeenCalled();
}

beforeEach(() => {
  handlers.clear();
  call = jest.fn().mockResolvedValue({});
  registerVaultIpc({ call } as unknown as VaultClient);
});

describe('albear:create validation', () => {
  it('rejects a payload that is not an object', async () => {
    expectRejected(await invoke('albear:create', 'not-a-record'));
  });

  it('rejects a missing name', async () => {
    expectRejected(await invoke('albear:create', { username: 'a' }));
  });

  it('rejects a blank name', async () => {
    expectRejected(await invoke('albear:create', { name: '   ' }));
  });

  it('rejects an unknown record type', async () => {
    expectRejected(await invoke('albear:create', { name: 'x', type: 'card' }));
  });

  it('rejects a non-string secret field', async () => {
    expectRejected(await invoke('albear:create', { name: 'x', password: 42 }));
  });

  it('rejects malformed url entries', async () => {
    expectRejected(await invoke('albear:create', { name: 'x', urlEntries: [{ sub: true }] }));
    call.mockClear();
    expectRejected(await invoke('albear:create', { name: 'x', urlEntries: 'https://a' }));
  });

  it('rejects a non-string custom value', async () => {
    expectRejected(await invoke('albear:create', { name: 'x', custom: { k: 1 } }));
  });

  it('accepts each valid record type', async () => {
    for (const type of ['login', 'note', 'api']) {
      call.mockClear();
      const res = await invoke('albear:create', { name: 'x', type });
      expect(res.ok).toBe(true);
      expect(call).toHaveBeenCalledWith('records.create', expect.objectContaining({ type }));
    }
  });

  it('preserves the subdomain policy it was given', async () => {
    await invoke('albear:create', {
      name: 'x',
      urlEntries: [{ url: 'https://a.test', sub: true }, { url: 'https://b.test' }],
    });
    expect(call).toHaveBeenCalledWith(
      'records.create',
      expect.objectContaining({
        urlEntries: [{ url: 'https://a.test', sub: true }, { url: 'https://b.test' }],
      }),
    );
  });

  it('never forwards a plain urls list, which the daemon would read as exact', async () => {
    await invoke('albear:create', {
      name: 'x',
      urls: ['https://a.test'],
      urlEntries: [{ url: 'https://a.test', sub: true }],
    });
    const [, payload] = call.mock.calls[0] as [string, Record<string, unknown>];
    expect(payload).not.toHaveProperty('urls');
  });
});

describe('albear:update validation', () => {
  const base = { id: 'r1', expectedRevision: 3, name: 'x' };

  it('rejects a missing id', async () => {
    expectRejected(await invoke('albear:update', { expectedRevision: 1, name: 'x' }));
  });

  it('rejects a missing expectedRevision', async () => {
    expectRejected(await invoke('albear:update', { id: 'r1', name: 'x' }));
  });

  it('rejects a non-integer expectedRevision', async () => {
    expectRejected(await invoke('albear:update', { ...base, expectedRevision: 1.5 }));
  });

  it('rejects a negative expectedRevision', async () => {
    expectRejected(await invoke('albear:update', { ...base, expectedRevision: -1 }));
  });

  it('forwards the revision unmodified so the daemon can detect a conflict', async () => {
    await invoke('albear:update', base);
    expect(call).toHaveBeenCalledWith(
      'records.update',
      expect.objectContaining({ id: 'r1', expectedRevision: 3 }),
    );
  });

  it('passes every secret through, including a deliberate clear', async () => {
    await invoke('albear:update', { ...base, password: '', notes: 'keep me' });
    const [, payload] = call.mock.calls[0] as [string, Record<string, unknown>];
    expect(payload.password).toBe('');
    expect(payload.notes).toBe('keep me');
  });
});

describe('albear:delete validation', () => {
  it('rejects a non-string id', async () => {
    expectRejected(await invoke('albear:delete', { id: 'r1' }));
  });

  it('forwards a valid id', async () => {
    const res = await invoke('albear:delete', 'r1');
    expect(res.ok).toBe(true);
    expect(call).toHaveBeenCalledWith('records.delete', { id: 'r1' });
  });
});
