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
import {
  unwrap,
  isCode,
  messageOf,
  AUTH_FAILED,
  RATE_LIMITED,
} from '@/lib/api';
import { DAEMON_UNAVAILABLE } from '../../shared/vaultTypes';

export function UnlockCard(): React.ReactElement {
  const { refresh } = useVault();
  const [password, setPassword] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState<string | undefined>();

  async function unlock(): Promise<void> {
    if (!password || busy) return;
    setErr(undefined);
    setBusy(true);
    try {
      unwrap(await window.albear.unlock(password));
      setPassword('');
      await refresh();
    } catch (e) {
      if (isCode(e, RATE_LIMITED)) setErr('too many attempts — wait a moment');
      else if (isCode(e, AUTH_FAILED)) setErr('wrong password');
      else if (isCode(e, DAEMON_UNAVAILABLE)) {
        setErr('lost connection to vaultd');
        void refresh();
      } else setErr(messageOf(e, 'unlock failed'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Unlock</CardTitle>
        <CardDescription>Enter your master password.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <Input
          type="password"
          placeholder="Master password"
          autoFocus
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') void unlock();
          }}
        />
        <Button onClick={() => void unlock()} disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          Unlock
        </Button>
        {err && (
          <Alert variant="destructive">
            <AlertTitle>Cannot unlock</AlertTitle>
            <AlertDescription>{err}</AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
