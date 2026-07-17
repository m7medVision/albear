// Vault administration: master-password change.
//
// Vault creation lives in CreateVaultCard (the uninitialized phase) and panic
// lock lives in the shell header, since both need to be reachable when this
// section is not.
import * as React from 'react';
import { KeyRound, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { useVault } from '@/VaultContext';
import { unwrap, isCode, messageOf, AUTH_FAILED } from '@/lib/api';

/**
 * The same policy CreateVaultCard states, for the same reason: the daemon
 * answers every strength failure with one generic error on purpose, so the
 * rules have to be visible before submitting. Guidance only — the policy is not
 * re-implemented here, and the daemon remains the only judge.
 */
const POLICY = [
  'At least 12 characters.',
  'Either 16+ characters, or a mix of cases, digits and symbols.',
  'Not a common password, and not a repeated or sequential run.',
];

export function SettingsSection(): React.ReactElement {
  const { refresh, recordCount } = useVault();
  const [current, setCurrent] = React.useState('');
  const [next, setNext] = React.useState('');
  const [confirm, setConfirm] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState<string | undefined>();
  const [ok, setOk] = React.useState(false);

  const mismatch = confirm.length > 0 && next !== confirm;
  const canSubmit =
    current.length > 0 && next.length > 0 && next === confirm && !busy;

  async function change(): Promise<void> {
    if (!canSubmit) return;
    setErr(undefined);
    setOk(false);
    setBusy(true);
    try {
      unwrap(await window.albear.changePassword(current, next));
      setCurrent('');
      setNext('');
      setConfirm('');
      setOk(true);
      void refresh();
    } catch (e) {
      if (isCode(e, AUTH_FAILED)) setErr('that is not your current password');
      else setErr(messageOf(e, 'could not change the master password'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>Change master password</CardTitle>
          <CardDescription>
            Re-wraps the vault key. Your records are not re-encrypted, and every
            other client stays paired.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
            {POLICY.map((rule) => (
              <li key={rule}>{rule}</li>
            ))}
          </ul>
          <Input
            type="password"
            placeholder="Current master password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
          <Input
            type="password"
            placeholder="New master password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
          />
          <Input
            type="password"
            placeholder="Repeat new master password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void change();
            }}
          />
          {mismatch && (
            <p className="text-sm text-destructive">
              the two new passwords do not match
            </p>
          )}
          <Button
            className="self-start"
            disabled={!canSubmit}
            onClick={() => void change()}
          >
            {busy ? <Loader2 className="animate-spin" /> : <KeyRound />}
            Change password
          </Button>
          {ok && (
            <Alert>
              <AlertDescription>
                Master password changed. Use the new one from now on.
              </AlertDescription>
            </Alert>
          )}
          {err && (
            <Alert variant="destructive">
              <AlertTitle>Cannot change the password</AlertTitle>
              <AlertDescription>{err}</AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>This vault</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          <p>
            {recordCount === undefined
              ? 'record count unavailable'
              : `${recordCount} record${recordCount === 1 ? '' : 's'} stored.`}
          </p>
          {/* Destroying a vault is deliberately absent. It is irreversible, and
              the CLI already gates it behind an interactive password prompt —
              a button here would add risk without adding capability. */}
          <p className="mt-2">
            To destroy this vault permanently, use <code>vault destroy</code> in
            a terminal.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
