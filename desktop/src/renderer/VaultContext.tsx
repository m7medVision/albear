// Shell-level vault state: which phase the app is in, and how to re-check it.
//
// The daemon is the authority on lock state, so the phase is derived from
// vault.status and never assumed. Sections call `refreshIfLocked` when an
// operation comes back VAULT_LOCKED rather than rendering a stale unlocked view.
import * as React from 'react';
import { unwrap, isCode, VAULT_LOCKED } from '@/lib/api';

export type Phase =
  | 'connecting'
  | 'unavailable'
  | 'uninitialized'
  | 'locked'
  | 'unlocked';

interface VaultState {
  phase: Phase;
  recordCount?: number;
  refresh: () => Promise<void>;
  /**
   * Re-check status if this error means the vault locked under us. Returns
   * true when it handled the error, so callers can skip their own reporting:
   * a lock is not something to show as a failed operation.
   */
  refreshIfLocked: (err: unknown) => boolean;
}

const VaultContext = React.createContext<VaultState | null>(null);

export function useVault(): VaultState {
  const ctx = React.useContext(VaultContext);
  if (!ctx) throw new Error('useVault must be used inside VaultProvider');
  return ctx;
}

export function VaultProvider({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  const [phase, setPhase] = React.useState<Phase>('connecting');
  const [recordCount, setRecordCount] = React.useState<number | undefined>();

  const refresh = React.useCallback(async (): Promise<void> => {
    // window.albear is absent outside Electron (e.g. jest/jsdom without a mock).
    if (!window.albear) {
      setPhase('unavailable');
      return;
    }
    try {
      const st = unwrap(await window.albear.status());
      setRecordCount(st.recordCount);
      if (!st.available) setPhase('unavailable');
      else if (!st.initialized) setPhase('uninitialized');
      else if (!st.unlocked) setPhase('locked');
      else setPhase('unlocked');
    } catch {
      setPhase('unavailable');
    }
  }, []);

  const refreshIfLocked = React.useCallback(
    (err: unknown): boolean => {
      if (!isCode(err, VAULT_LOCKED)) return false;
      void refresh();
      return true;
    },
    [refresh],
  );

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  // Keep retrying while the daemon is unreachable. Status is exempt from the
  // idle auto-lock timer daemon-side, so polling cannot hold a vault open.
  React.useEffect(() => {
    if (phase !== 'unavailable' && phase !== 'connecting') return undefined;
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => window.clearInterval(timer);
  }, [phase, refresh]);

  const value = React.useMemo(
    () => ({ phase, recordCount, refresh, refreshIfLocked }),
    [phase, recordCount, refresh, refreshIfLocked],
  );

  return (
    <VaultContext.Provider value={value}>{children}</VaultContext.Provider>
  );
}
