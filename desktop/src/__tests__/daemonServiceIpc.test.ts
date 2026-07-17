/**
 * @jest-environment node
 */
const handlers = new Map<string, (...args: unknown[]) => unknown>();

jest.mock('electron', () => ({
  ipcMain: {
    handle: (channel: string, fn: (...args: unknown[]) => unknown) => {
      handlers.set(channel, fn);
    },
  },
}));

// eslint-disable-next-line import/first
import { registerDaemonServiceIpc } from '../main/daemonServiceIpc';
// eslint-disable-next-line import/first
import { DaemonServiceError } from '../main/daemonService';
// eslint-disable-next-line import/first
import type { AlbearResult } from '../shared/vaultTypes';

function invoke(channel: string, ...args: unknown[]) {
  const handler = handlers.get(channel);
  if (!handler) throw new Error(`no handler registered for ${channel}`);
  return handler({}, ...args) as Promise<AlbearResult<unknown>>;
}

describe('daemon service IPC', () => {
  beforeEach(() => handlers.clear());

  it('exposes only status and setup for the fixed controller', async () => {
    const controller = {
      status: jest.fn().mockResolvedValue({ state: 'stopped', enabled: false }),
      setup: jest.fn().mockResolvedValue({ state: 'running', enabled: true }),
    };
    registerDaemonServiceIpc(controller);

    await expect(invoke('albear:daemonServiceStatus')).resolves.toEqual({
      ok: true,
      data: { state: 'stopped', enabled: false },
    });
    await expect(
      invoke(
        'albear:daemonServiceSetup',
        'rm',
        '-rf',
        '/',
        'attacker.service',
      ),
    ).resolves.toEqual({
      ok: true,
      data: { state: 'running', enabled: true },
    });

    expect(controller.status).toHaveBeenCalledWith();
    expect(controller.setup).toHaveBeenCalledWith();
    expect([...handlers.keys()].sort()).toEqual([
      'albear:daemonServiceSetup',
      'albear:daemonServiceStatus',
    ]);
  });

  it('returns typed sanitized controller errors', async () => {
    registerDaemonServiceIpc({
      status: jest.fn(),
      setup: jest.fn().mockRejectedValue(
        new DaemonServiceError(
          'SERVICE_START_FAILED',
          'could not start the Albear background service',
        ),
      ),
    });

    await expect(invoke('albear:daemonServiceSetup')).resolves.toEqual({
      ok: false,
      error: {
        code: 'SERVICE_START_FAILED',
        message: 'could not start the Albear background service',
      },
    });
  });
});
