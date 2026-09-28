// The desktop app's environment, mirroring internal/version/environment.go:
// prod and dev keep separate vaults, so a dev build can never open the prod
// one. The daemon derives its environment from its version; the desktop app
// uses the closest equivalent it has — a packaged app is prod, an unpackaged
// one (`npm start`) is dev. ALBEAR_ENV overrides either.

import os from 'os';
import path from 'path';

export type Environment = 'prod' | 'dev';

export const ENV_VAR = 'ALBEAR_ENV';

/** The process environment variables this module reads. */
type Env = Record<string, string | undefined>;

/**
 * Resolves the environment. Throws on an ALBEAR_ENV that is neither "dev" nor
 * "prod": guessing wrong means dialing the other environment's vault, so a
 * typo is reported rather than ignored. An empty value counts as unset, as it
 * does in the daemon.
 */
export function resolveEnvironment(
  isPackaged: boolean,
  env: Env = process.env,
): Environment {
  const override = env[ENV_VAR];
  if (override === 'dev' || override === 'prod') return override;
  if (override) {
    throw new Error(`invalid ${ENV_VAR} "${override}" (want "dev" or "prod")`);
  }
  return isPackaged ? 'prod' : 'dev';
}

/**
 * Socket path resolution, mirroring
 * internal/infrastructure/system/paths.go ResolvePaths():
 * $XDG_RUNTIME_DIR/<name>/vault.sock, falling back to
 * $XDG_DATA_HOME(~/.local/share)/<name>/run/<name>/vault.sock, where <name>
 * is "albear" in prod and "albear-dev" in dev.
 */
export function defaultSocketPath(
  environment: Environment,
  env: Env = process.env,
  home: string = os.homedir(),
): string {
  const name = environment === 'dev' ? 'albear-dev' : 'albear';
  const data = env.XDG_DATA_HOME || path.join(home, '.local', 'share');
  const runtime = env.XDG_RUNTIME_DIR || path.join(data, name, 'run');
  return path.join(runtime, name, 'vault.sock');
}
