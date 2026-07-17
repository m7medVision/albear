// Backup create / verify / restore.
//
// No path ever appears in this file. Main opens the dialogs, keeps the chosen
// path, and — for restore — confirms natively before calling the daemon, so the
// renderer cannot aim an irreversible operation at a file of its choosing.
import * as React from 'react';
import { Archive, Loader2, ShieldCheck, Upload } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { useVault } from '@/VaultContext';
import { unwrap, messageOf } from '@/lib/api';
import { isCanceled, type BackupInfo } from '../../shared/vaultTypes';

type Busy = 'create' | 'verify' | 'restore' | null;

export function BackupSection(): React.ReactElement {
  const { refresh, refreshIfLocked } = useVault();
  const [busy, setBusy] = React.useState<Busy>(null);
  const [err, setErr] = React.useState<string | undefined>();
  const [ok, setOk] = React.useState<string | undefined>();
  const [verified, setVerified] = React.useState<BackupInfo | undefined>();

  function begin(what: Busy): void {
    setBusy(what);
    setErr(undefined);
    setOk(undefined);
    setVerified(undefined);
  }

  async function create(): Promise<void> {
    begin('create');
    try {
      const res = unwrap(await window.albear.backupCreate());
      // Dismissing the dialog is a success carrying "nothing happened".
      if (isCanceled(res)) return;
      setOk(`Backup written to ${res.path}`);
    } catch (e) {
      if (!refreshIfLocked(e)) setErr(messageOf(e, 'could not create a backup'));
    } finally {
      setBusy(null);
    }
  }

  async function verify(): Promise<void> {
    begin('verify');
    try {
      const res = unwrap(await window.albear.backupVerify());
      if (isCanceled(res)) return;
      setVerified(res);
    } catch (e) {
      // A failed HMAC is the check working, not the app breaking: say so
      // plainly rather than presenting the container as merely unavailable.
      if (!refreshIfLocked(e)) {
        setErr(
          'This file did not authenticate against your vault. It is damaged, tampered with, or belongs to a different vault — do not restore it.',
        );
      }
    } finally {
      setBusy(null);
    }
  }

  async function restore(): Promise<void> {
    begin('restore');
    try {
      const res = unwrap(await window.albear.backupRestore());
      if (isCanceled(res)) return;
      setOk('Vault restored. It has been locked — unlock it to continue.');
    } catch (e) {
      if (!refreshIfLocked(e)) setErr(messageOf(e, 'could not restore'));
    } finally {
      setBusy(null);
      // Restore locks the vault, so the phase moved regardless of the outcome.
      void refresh();
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {err && (
        <Alert variant="destructive">
          <AlertTitle>Backup failed</AlertTitle>
          <AlertDescription>{err}</AlertDescription>
        </Alert>
      )}
      {ok && (
        <Alert>
          <AlertDescription>{ok}</AlertDescription>
        </Alert>
      )}
      {verified && (
        <Alert>
          <AlertTitle>This backup is genuine</AlertTitle>
          <AlertDescription>
            Created {new Date(verified.createdAtMs).toLocaleString()}, and it
            authenticates against this vault.
          </AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Create a backup</CardTitle>
          <CardDescription>
            Writes an encrypted container. Your secrets stay encrypted inside
            it, so the file is only as recoverable as your master password.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button disabled={busy !== null} onClick={() => void create()}>
            {busy === 'create' ? <Loader2 className="animate-spin" /> : <Archive />}
            Create backup…
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Verify a backup</CardTitle>
          <CardDescription>
            Checks a container is intact and belongs to this vault, without
            touching your live data.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            variant="secondary"
            disabled={busy !== null}
            onClick={() => void verify()}
          >
            {busy === 'verify' ? (
              <Loader2 className="animate-spin" />
            ) : (
              <ShieldCheck />
            )}
            Verify backup…
          </Button>
        </CardContent>
      </Card>

      <Card className="border-destructive/40">
        <CardHeader>
          <CardTitle>Restore from a backup</CardTitle>
          <CardDescription>
            Replaces every record in this vault with the backup&apos;s contents.
            The current database is kept alongside it as a .recovery file, and
            the vault locks afterwards. You will be asked to confirm.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            variant="destructive"
            disabled={busy !== null}
            onClick={() => void restore()}
          >
            {busy === 'restore' ? (
              <Loader2 className="animate-spin" />
            ) : (
              <Upload />
            )}
            Restore backup…
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
