// Lifecycle control for the one fixed per-user service Albear ships. The
// renderer never supplies a command, executable, argument or unit name.

import { execFile } from 'child_process';
import type { DaemonServiceStatus } from '../shared/vaultTypes';

const SYSTEMCTL = 'systemctl';
const UNIT = 'albear-vaultd.service';
const COMMAND_TIMEOUT_MS = 10_000;

const STATUS_ARGS = [
  '--user',
  'show',
  UNIT,
  '--property=LoadState',
  '--property=ActiveState',
  '--property=UnitFileState',
  '--no-pager',
] as const;

const SETUP_ARGS = ['--user', 'enable', '--now', UNIT] as const;

export interface ProcessOutput {
  stdout: string;
  stderr: string;
}

export type ProcessRunner = (
  executable: string,
  args: readonly string[],
) => Promise<ProcessOutput>;

interface ProcessFailure extends Error {
  code?: string | number;
  stdout?: string;
  stderr?: string;
}

function runProcess(
  executable: string,
  args: readonly string[],
): Promise<ProcessOutput> {
  return new Promise((resolve, reject) => {
    execFile(
      executable,
      [...args],
      { encoding: 'utf8', timeout: COMMAND_TIMEOUT_MS },
      (error, stdout, stderr) => {
        if (error) {
          const failure = error as ProcessFailure;
          failure.stdout = stdout;
          failure.stderr = stderr;
          reject(failure);
          return;
        }
        resolve({ stdout, stderr });
      },
    );
  });
}

function properties(output: string): Record<string, string> {
  const values: Record<string, string> = {};
  for (const line of output.split('\n')) {
    const separator = line.indexOf('=');
    if (separator <= 0) continue;
    values[line.slice(0, separator)] = line.slice(separator + 1);
  }
  return values;
}

function statusFrom(output: string): DaemonServiceStatus | undefined {
  const value = properties(output);
  if (!value.LoadState && !value.ActiveState && !value.UnitFileState) {
    return undefined;
  }
  if (value.LoadState === 'not-found') return { state: 'missing' };

  const enabled =
    value.UnitFileState === 'enabled' || value.UnitFileState === 'enabled-runtime';
  if (value.ActiveState === 'active') return { state: 'running', enabled };
  if (value.ActiveState === 'failed') return { state: 'failed', enabled };
  return { state: 'stopped', enabled };
}

function isUnsupported(error: ProcessFailure): boolean {
  if (error.code === 'ENOENT') return true;
  const detail = `${error.message}\n${error.stderr ?? ''}`.toLowerCase();
  return (
    detail.includes('failed to connect to bus') ||
    detail.includes('no medium found') ||
    detail.includes('not been booted with systemd')
  );
}

export class DaemonServiceError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = 'DaemonServiceError';
  }
}

export class DaemonServiceController {
  constructor(private readonly run: ProcessRunner = runProcess) {}

  async status(): Promise<DaemonServiceStatus> {
    try {
      const result = await this.run(SYSTEMCTL, STATUS_ARGS);
      return statusFrom(result.stdout) ?? { state: 'failed' };
    } catch (cause) {
      const failure = cause as ProcessFailure;
      const parsed = statusFrom(failure.stdout ?? '');
      if (parsed) return parsed;
      if (isUnsupported(failure)) return { state: 'unsupported' };
      return { state: 'failed' };
    }
  }

  async setup(): Promise<DaemonServiceStatus> {
    try {
      await this.run(SYSTEMCTL, SETUP_ARGS);
    } catch (cause) {
      if (isUnsupported(cause as ProcessFailure)) {
        throw new DaemonServiceError(
          'SERVICE_UNSUPPORTED',
          'systemd user services are unavailable',
        );
      }
      throw new DaemonServiceError(
        'SERVICE_START_FAILED',
        'could not start the Albear background service',
      );
    }

    const status = await this.status();
    if (status.state !== 'running') {
      throw new DaemonServiceError(
        'SERVICE_START_FAILED',
        'the Albear background service did not start',
      );
    }
    return status;
  }
}

export const daemonServiceCommand = {
  executable: SYSTEMCTL,
  statusArgs: STATUS_ARGS,
  setupArgs: SETUP_ARGS,
} as const;
