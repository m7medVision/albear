// Turning the IPC result envelope into something a component can use.
//
// Every handler resolves to AlbearResult rather than rejecting, so the renderer
// sees typed error codes instead of serialized exception text. `unwrap` puts
// that back into the shape React code wants — throw on failure — while keeping
// the code, which is the part worth branching on.
import type { AlbearResult } from '../../shared/vaultTypes';

export class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export function unwrap<T>(result: AlbearResult<T>): T {
  if (!result.ok) throw new ApiError(result.error.code, result.error.message);
  return result.data;
}

export function isCode(err: unknown, code: string): boolean {
  return err instanceof ApiError && err.code === code;
}

export function messageOf(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

/** Daemon error codes the UI branches on. Others are shown as-is. */
export const VAULT_LOCKED = 'VAULT_LOCKED';
export const AUTH_FAILED = 'AUTH_FAILED';
export const RATE_LIMITED = 'RATE_LIMITED';
export const CONFLICT = 'CONFLICT';
export const DENIED = 'DENIED';
export const NOT_FOUND = 'NOT_FOUND';
export const INVALID_REQUEST = 'INVALID_REQUEST';
export const INTEGRITY_FAILURE = 'INTEGRITY_FAILURE';
