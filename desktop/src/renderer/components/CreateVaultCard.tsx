import * as React from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
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
  const [submitted, setSubmitted] = React.useState(false);
  const [err, setErr] = React.useState<string | undefined>();
  const passwordRef = React.useRef<HTMLInputElement>(null);
  const confirmRef = React.useRef<HTMLInputElement>(null);

  // Field problems. Mismatch shows as soon as the confirmation is typed; an
  // empty field only after a submit attempt, so the form never opens in red.
  const passwordMissing = submitted && password.length === 0;
  const confirmMissing =
    submitted && password.length > 0 && confirm.length === 0;
  const mismatch = confirm.length > 0 && password !== confirm;
  const confirmProblem = confirmMissing || mismatch;

  async function create(): Promise<void> {
    if (busy) return;
    setSubmitted(true);
    if (!password) {
      passwordRef.current?.focus();
      return;
    }
    if (password !== confirm) {
      confirmRef.current?.focus();
      return;
    }
    setErr(undefined);
    setBusy(true);
    try {
      unwrap(await window.albear.init(password));
      setPassword('');
      setConfirm('');
      await refresh();
    } catch (e) {
      setErr(messageOf(e, 'Unable to create the vault.'));
      passwordRef.current?.focus();
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
      <CardContent>
        <form
          noValidate
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <ul
            id="create-policy"
            className="text-sm text-muted-foreground list-disc ps-5 space-y-1"
          >
            {POLICY.map((rule) => (
              <li key={rule}>{rule}</li>
            ))}
          </ul>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="create-password">Master password</Label>
            <Input
              ref={passwordRef}
              id="create-password"
              type="password"
              autoComplete="new-password"
              autoFocus
              value={password}
              aria-invalid={passwordMissing || err ? true : undefined}
              aria-describedby={[
                'create-policy',
                passwordMissing && 'create-password-error',
                err && 'create-error',
              ]
                .filter(Boolean)
                .join(' ')}
              onChange={(e) => setPassword(e.target.value)}
            />
            {passwordMissing && (
              <p
                id="create-password-error"
                className="text-sm text-destructive"
              >
                Choose a master password that meets the rules above.
              </p>
            )}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="create-confirm">Confirm master password</Label>
            <Input
              ref={confirmRef}
              id="create-confirm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              aria-invalid={confirmProblem ? true : undefined}
              aria-describedby={
                confirmProblem ? 'create-confirm-error' : undefined
              }
              onChange={(e) => setConfirm(e.target.value)}
            />
            {confirmProblem && (
              <p id="create-confirm-error" className="text-sm text-destructive">
                {mismatch
                  ? 'The passwords do not match. Retype the same password in both fields.'
                  : 'Type the master password again to confirm it.'}
              </p>
            )}
          </div>
          <Button type="submit" disabled={busy}>
            {busy && <Loader2 className="animate-spin" />}
            Create vault
          </Button>
          {err && (
            <Alert id="create-error" variant="destructive">
              <AlertTitle>Cannot create the vault</AlertTitle>
              <AlertDescription className="flex flex-col gap-1">
                <span>{err}</span>
                <span>
                  Check the password against the rules above, then try again.
                </span>
              </AlertDescription>
            </Alert>
          )}
        </form>
      </CardContent>
    </Card>
  );
}
