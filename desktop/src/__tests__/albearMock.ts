// A stand-in for the preload bridge, shared by the renderer tests.
//
// It mirrors the real handler's contract: every method resolves to an
// AlbearResult and none of them reject, so a component under test sees typed
// codes rather than exceptions — the same as in the app.
import type { AlbearHandler } from '../main/preload';

const empty = { ok: true as const, data: {} };

export function ok<T>(data: T) {
  return { ok: true as const, data };
}

export function fail(code: string, message = code) {
  return { ok: false as const, error: { code, message } };
}

export function albearMock(
  overrides: Partial<Record<keyof AlbearHandler, unknown>> = {},
): AlbearHandler {
  return {
    status: jest.fn().mockResolvedValue(ok({ available: false })),
    daemonServiceStatus: jest
      .fn()
      .mockResolvedValue(ok({ state: 'unsupported' })),
    daemonServiceSetup: jest
      .fn()
      .mockResolvedValue(ok({ state: 'running', enabled: true })),
    unlock: jest.fn().mockResolvedValue(empty),
    lock: jest.fn().mockResolvedValue(empty),
    panic: jest.fn().mockResolvedValue(empty),
    init: jest.fn().mockResolvedValue(empty),
    changePassword: jest.fn().mockResolvedValue(empty),
    list: jest.fn().mockResolvedValue(ok({ records: [] })),
    search: jest.fn().mockResolvedValue(ok({ records: [] })),
    show: jest.fn(),
    reveal: jest.fn().mockResolvedValue(ok({})),
    create: jest.fn().mockResolvedValue(ok({ id: 'new' })),
    update: jest.fn().mockResolvedValue(empty),
    remove: jest.fn().mockResolvedValue(empty),
    generate: jest.fn().mockResolvedValue(ok({ password: 'generated' })),
    copyText: jest.fn().mockResolvedValue(empty),
    clientsPending: jest.fn().mockResolvedValue(ok({ pending: [] })),
    clientsList: jest.fn().mockResolvedValue(ok({ clients: [] })),
    clientsApprove: jest.fn().mockResolvedValue(empty),
    clientsRevoke: jest.fn().mockResolvedValue(empty),
    events: jest.fn().mockResolvedValue(ok({ events: [] })),
    backupCreate: jest.fn().mockResolvedValue(ok({ canceled: true })),
    backupVerify: jest.fn().mockResolvedValue(ok({ canceled: true })),
    backupRestore: jest.fn().mockResolvedValue(ok({ canceled: true })),
    ...overrides,
  } as unknown as AlbearHandler;
}

/** Status shorthand for a vault that is up, created and open. */
export const UNLOCKED = ok({
  available: true,
  initialized: true,
  unlocked: true,
  recordCount: 1,
});
