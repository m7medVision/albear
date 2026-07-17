/**
 * @jest-environment node
 */
import {
  DaemonServiceController,
  DaemonServiceError,
  daemonServiceCommand,
  type ProcessRunner,
} from '../main/daemonService';

function output(stdout: string) {
  return { stdout, stderr: '' };
}

function show(
  load: string,
  active: string,
  unitFile: string,
): { stdout: string; stderr: string } {
  return output(
    `LoadState=${load}\nActiveState=${active}\nUnitFileState=${unitFile}\n`,
  );
}

describe('DaemonServiceController', () => {
  it.each([
    ['active', 'enabled', { state: 'running', enabled: true }],
    ['inactive', 'disabled', { state: 'stopped', enabled: false }],
    ['failed', 'enabled', { state: 'failed', enabled: true }],
  ] as const)('maps %s service state', async (active, unitFile, expected) => {
    const run = jest.fn().mockResolvedValue(show('loaded', active, unitFile));
    await expect(
      new DaemonServiceController(run as ProcessRunner).status(),
    ).resolves.toEqual(expected);
    expect(run).toHaveBeenCalledWith(
      daemonServiceCommand.executable,
      daemonServiceCommand.statusArgs,
    );
  });

  it('recognizes a missing unit even when systemctl exits non-zero', async () => {
    const failure = Object.assign(new Error('not found'), {
      code: 4,
      stdout:
        'LoadState=not-found\nActiveState=inactive\nUnitFileState=\n',
      stderr: '',
    });
    const run = jest.fn().mockRejectedValue(failure);
    await expect(
      new DaemonServiceController(run as ProcessRunner).status(),
    ).resolves.toEqual({ state: 'missing' });
  });

  it('recognizes systems without systemctl or a user bus', async () => {
    for (const failure of [
      Object.assign(new Error('spawn systemctl ENOENT'), { code: 'ENOENT' }),
      Object.assign(new Error('systemctl failed'), {
        code: 1,
        stderr: 'Failed to connect to bus: No medium found',
      }),
    ]) {
      const run = jest.fn().mockRejectedValue(failure);
      await expect(
        new DaemonServiceController(run as ProcessRunner).status(),
      ).resolves.toEqual({ state: 'unsupported' });
    }
  });

  it('uses only the fixed setup command and confirms the service started', async () => {
    const run = jest
      .fn()
      .mockResolvedValueOnce(output(''))
      .mockResolvedValueOnce(show('loaded', 'active', 'enabled'));
    const controller = new DaemonServiceController(run as ProcessRunner);

    await expect(controller.setup()).resolves.toEqual({
      state: 'running',
      enabled: true,
    });
    expect(run.mock.calls).toEqual([
      [daemonServiceCommand.executable, daemonServiceCommand.setupArgs],
      [daemonServiceCommand.executable, daemonServiceCommand.statusArgs],
    ]);
  });

  it('returns sanitized setup failures', async () => {
    const run = jest.fn().mockRejectedValue(
      Object.assign(new Error('secret environment detail'), {
        code: 1,
        stderr: '/home/user/private: permission denied',
      }),
    );
    const controller = new DaemonServiceController(run as ProcessRunner);

    await expect(controller.setup()).rejects.toEqual(
      new DaemonServiceError(
        'SERVICE_START_FAILED',
        'could not start the Albear background service',
      ),
    );
  });
});
