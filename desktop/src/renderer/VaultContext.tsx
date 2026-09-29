// Shell-level vault state: which phase the app is in, and how to re-check it.
//
// The daemon is the authority on lock state, so the phase is derived from
// vault.status and never assumed. Sections call `refreshIfLocked` when an
// operation comes back VAULT_LOCKED rather than rendering a stale unlocked view.
import * as React from 'react';
import { unwrap, isCode, VAULT_LOCKED } from '@/lib/api';

export type Phase =
  | 'connecting'
  | 'service-setup'
  | 'service-failed'
  | 'service-missing'
  | 'service-unsupported'
  | 'unavailable'
  | 'uninitialized'
  | 'locked'
  | 'unlocked';

/**
 * A message that has to outlive the screen that produced it. Restoring a
 * backup locks the vault, which unmounts the Backup section before its result
 * can be read; the unlock screen shows this instead.
 */
export interface Notice {
  title: string;
  detail: string;
  tone?: 'default' | 'destructive';
}

interface VaultState {
  phase: Phase;
  recordCount?: number;
  /** Re-checks status; resolves to the phase it settled on. */
  refresh: () => Promise<Phase>;
  notice?: Notice;
  setNotice: (notice: Notice | undefined) => void;
  setupDaemonService: () => Promise<void>;
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
  const [notice, setNotice] = React.useState<Notice | undefined>();

  const refresh = React.useCallback(async (): Promise<Phase> => {
    // window.albear is absent outside Electron (e.g. jest/jsdom without a mock).
    if (!window.albear) {
      setPhase('unavailable');
      return 'unavailable';
    }
    try {
      const st = unwrap(await window.albear.status());
      setRecordCount(st.recordCount);
      let next: Phase;
      if (st.available) {
        if (!st.initialized) next = 'uninitialized';
        else if (!st.unlocked) next = 'locked';
        else next = 'unlocked';
      } else {
        const service = unwrap(await window.albear.daemonServiceStatus());
        switch (service.state) {
          case 'stopped':
            next = 'service-setup';
            break;
          case 'failed':
            next = 'service-failed';
            break;
          case 'missing':
            next = 'service-missing';
            break;
          case 'unsupported':
            next = 'service-unsupported';
            break;
          default:
            // systemd sees a running service but its socket is not reachable
            // yet.
            next = 'unavailable';
        }
      }
      setPhase(next);
      return next;
    } catch {
      setPhase('unavailable');
      return 'unavailable';
    }
  }, []);

  const setupDaemonService = React.useCallback(async (): Promise<void> => {
    unwrap(await window.albear.daemonServiceSetup());
    await refresh();
  }, [refresh]);

  // A notice belongs to the locked screen it was handed to; once the vault
  // is open again it has been read and is dropped.
  React.useEffect(() => {
    if (phase === 'unlocked') setNotice(undefined);
  }, [phase]);

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
    const retrying: Phase[] = [
      'connecting',
      'service-setup',
      'service-failed',
      'service-missing',
      'service-unsupported',
      'unavailable',
    ];
    if (!retrying.includes(phase)) return undefined;
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => window.clearInterval(timer);
  }, [phase, refresh]);

  const value = React.useMemo(
    () => ({
      phase,
      recordCount,
      refresh,
      notice,
      setNotice,
      setupDaemonService,
      refreshIfLocked,
    }),
    [phase, recordCount, refresh, notice, setupDaemonService, refreshIfLocked],
  );

  return (
    <VaultContext.Provider value={value}>{children}</VaultContext.Provider>
  );
}
