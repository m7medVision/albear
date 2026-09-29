/**
 * @jest-environment node
 */
import { defaultSocketPath, resolveEnvironment } from '../main/environment';

const HOME = '/home/u';

describe('resolveEnvironment', () => {
  it.each([
    [true, undefined, 'prod'],
    [false, undefined, 'dev'],
    // An empty value is unset, as it is in the daemon.
    [true, '', 'prod'],
    [false, '', 'dev'],
    [true, 'dev', 'dev'],
    [false, 'prod', 'prod'],
    [true, 'prod', 'prod'],
    [false, 'dev', 'dev'],
  ] as const)(
    'packaged=%s ALBEAR_ENV=%s -> %s',
    (isPackaged, override, expected) => {
      expect(resolveEnvironment(isPackaged, { ALBEAR_ENV: override })).toBe(
        expected,
      );
    },
  );

  it.each(['production', 'DEV', 'staging', ' dev'])(
    'reports an invalid ALBEAR_ENV %p instead of guessing',
    (override) => {
      for (const isPackaged of [true, false]) {
        expect(() =>
          resolveEnvironment(isPackaged, { ALBEAR_ENV: override }),
        ).toThrow(/invalid ALBEAR_ENV/);
      }
    },
  );
});

// These must match internal/infrastructure/system ResolvePaths, or the app
// dials a socket no daemon listens on — or the other environment's daemon.
describe('defaultSocketPath', () => {
  it.each([
    [
      'prod',
      { XDG_RUNTIME_DIR: '/run/user/1000' },
      '/run/user/1000/albear/vault.sock',
    ],
    [
      'dev',
      { XDG_RUNTIME_DIR: '/run/user/1000' },
      '/run/user/1000/albear-dev/vault.sock',
    ],
    ['prod', {}, '/home/u/.local/share/albear/run/albear/vault.sock'],
    ['dev', {}, '/home/u/.local/share/albear-dev/run/albear-dev/vault.sock'],
    ['prod', { XDG_DATA_HOME: '/data' }, '/data/albear/run/albear/vault.sock'],
    [
      'dev',
      { XDG_DATA_HOME: '/data' },
      '/data/albear-dev/run/albear-dev/vault.sock',
    ],
  ] as const)('%s with %j -> %s', (environment, env, expected) => {
    expect(defaultSocketPath(environment, env, HOME)).toBe(expected);
  });

  it.each([
    [true, undefined, '/run/user/1000/albear/vault.sock'],
    [false, undefined, '/run/user/1000/albear-dev/vault.sock'],
    [true, 'dev', '/run/user/1000/albear-dev/vault.sock'],
    [false, 'prod', '/run/user/1000/albear/vault.sock'],
  ] as const)(
    'packaged=%s ALBEAR_ENV=%s dials %s',
    (isPackaged, override, expected) => {
      const env = { XDG_RUNTIME_DIR: '/run/user/1000', ALBEAR_ENV: override };
      expect(
        defaultSocketPath(resolveEnvironment(isPackaged, env), env, HOME),
      ).toBe(expected);
    },
  );
});
