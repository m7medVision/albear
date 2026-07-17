import * as React from 'react';
import { Loader2, Play } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { useVault } from '@/VaultContext';
import { messageOf } from '@/lib/api';

export function DaemonSetupCard(): React.ReactElement {
  const { setupDaemonService } = useVault();
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string>();

  async function setup(): Promise<void> {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await setupDaemonService();
    } catch (cause) {
      setError(
        messageOf(cause, 'could not start the Albear background service'),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Start Albear</CardTitle>
        <CardDescription>
          Albear needs its local background service before this app, the CLI,
          or the browser extension can reach your vault.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="text-sm text-muted-foreground space-y-1">
          <p>It runs only for your Linux account and never listens on a network.</p>
          <p>
            It will start when you sign in and stays locked until you enter your
            master password.
          </p>
        </div>
        <Button onClick={() => void setup()} disabled={busy}>
          {busy ? <Loader2 className="animate-spin" /> : <Play />}
          {busy ? 'Starting…' : 'Enable and start'}
        </Button>
        {error && (
          <Alert variant="destructive">
            <AlertTitle>Could not start Albear</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
