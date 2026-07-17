import * as React from 'react';
import { Loader2 } from 'lucide-react';
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
import { unwrap, messageOf } from '@/lib/api';

/**
 * The master-password policy, stated before submission.
 *
 * The daemon returns one generic error for every strength failure on purpose —
 * naming the rule that tripped invites nudging a bad password until it squeaks
 * past. That makes stating the rules up front necessary: a user rejected by a
 * generic error has nothing to act on otherwise.
 *
 * These are guidance only. The policy is NOT re-implemented here: no live
 * strength meter, no per-rule feedback. That would rebuild the very oracle the
 * daemon refuses to be, and fork a security rule across two languages. The
 * daemon remains the only judge.
 */
const POLICY = [
  'At least 12 characters.',
  'Either 16+ characters, or a mix of cases, digits and symbols.',
  'Not a common password, and not a repeated or sequential run.',
];

export function CreateVaultCard(): React.ReactElement {
  const { refresh } = useVault();
  const [password, setPassword] = React.useState('');
  const [confirm, setConfirm] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState<string | undefined>();

  const mismatch = confirm.length > 0 && password !== confirm;
  const canSubmit = password.length > 0 && password === confirm && !busy;

  async function create(): Promise<void> {
    if (!canSubmit) return;
    setErr(undefined);
    setBusy(true);
    try {
      unwrap(await window.albear.init(password));
      setPassword('');
      setConfirm('');
      await refresh();
    } catch (e) {
      setErr(messageOf(e, 'could not create the vault'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create your vault</CardTitle>
        <CardDescription>
          Choose a master password. It is the only thing protecting your
          secrets, and it cannot be recovered if you forget it.
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
          placeholder="Master password"
          autoFocus
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Input
          type="password"
          placeholder="Repeat master password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') void create();
          }}
        />
        {mismatch && (
          <p className="text-sm text-destructive">
            the two passwords do not match
          </p>
        )}
        <Button onClick={() => void create()} disabled={!canSubmit}>
          {busy && <Loader2 className="animate-spin" />}
          Create vault
        </Button>
        {err && (
          <Alert variant="destructive">
            <AlertTitle>Cannot create the vault</AlertTitle>
            <AlertDescription>{err}</AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
