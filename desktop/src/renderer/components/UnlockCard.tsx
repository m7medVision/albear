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
import {
  unwrap,
  isCode,
  messageOf,
  AUTH_FAILED,
  RATE_LIMITED,
} from '@/lib/api';
import { DAEMON_UNAVAILABLE } from '../../shared/vaultTypes';

export function UnlockCard(): React.ReactElement {
  const { refresh, notice } = useVault();
  const [password, setPassword] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState<string | undefined>();
  const inputRef = React.useRef<HTMLInputElement>(null);

  async function unlock(): Promise<void> {
    if (busy) return;
    if (!password) {
      setErr('Enter your master password to unlock the vault.');
      inputRef.current?.focus();
      return;
    }
    setErr(undefined);
    setBusy(true);
    try {
      unwrap(await window.albear.unlock(password));
      setPassword('');
      await refresh();
    } catch (e) {
      if (isCode(e, RATE_LIMITED)) {
        setErr('Too many attempts. Wait a moment, then try again.');
      } else if (isCode(e, AUTH_FAILED)) {
        setErr(
          'That master password is incorrect. Check Caps Lock and try again.',
        );
      } else if (isCode(e, DAEMON_UNAVAILABLE)) {
        setErr('Lost connection to the Albear service. Reconnecting…');
        void refresh();
      } else {
        setErr(`${messageOf(e, 'Unable to unlock.')} Try again.`);
      }
      inputRef.current?.focus();
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Unlock your vault</CardTitle>
        <CardDescription>
          Your vault is locked. Enter your master password to open it.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {notice && (
          <Alert variant={notice.tone ?? 'default'}>
            <AlertTitle>{notice.title}</AlertTitle>
            <AlertDescription>{notice.detail}</AlertDescription>
          </Alert>
        )}
        {/* A real form: Enter submits natively and password managers see a
            proper login field. */}
        <form
          noValidate
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            void unlock();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="unlock-password">Master password</Label>
            <Input
              ref={inputRef}
              id="unlock-password"
              type="password"
              autoComplete="current-password"
              autoFocus
              value={password}
              aria-invalid={err ? true : undefined}
              aria-describedby={err ? 'unlock-error' : undefined}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <Button type="submit" disabled={busy}>
            {busy && <Loader2 className="animate-spin" />}
            Unlock
          </Button>
          {err && (
            <Alert id="unlock-error" variant="destructive">
              <AlertTitle>Cannot unlock</AlertTitle>
              <AlertDescription>{err}</AlertDescription>
            </Alert>
          )}
        </form>
      </CardContent>
    </Card>
  );
}
