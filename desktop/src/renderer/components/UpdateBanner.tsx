// Shows auto-update state pushed from the main process over IPC
// (see AppUpdater in src/main/main.ts) and offers a restart action
// once an update has been downloaded.
import * as React from 'react';
import { Download, RefreshCw } from 'lucide-react';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

type Phase = 'idle' | 'available' | 'downloading' | 'downloaded';

export function UpdateBanner(): React.ReactElement | null {
  const [phase, setPhase] = React.useState<Phase>('idle');
  const [version, setVersion] = React.useState<string | undefined>();
  const [percent, setPercent] = React.useState(0);

  React.useEffect(() => {
    // window.electron is absent outside Electron (e.g. jest/jsdom).
    if (!window.electron) return undefined;
    const unsubs = [
      window.electron.ipcRenderer.on('updater:update-available', (info) => {
        setVersion((info as { version?: string } | undefined)?.version);
        setPhase((p) => (p === 'downloaded' ? p : 'available'));
      }),
      window.electron.ipcRenderer.on('updater:download-progress', (prog) => {
        setPercent((prog as { percent?: number } | undefined)?.percent ?? 0);
        setPhase((p) => (p === 'downloaded' ? p : 'downloading'));
      }),
      window.electron.ipcRenderer.on('updater:update-downloaded', (info) => {
        const v = (info as { version?: string } | undefined)?.version;
        if (v) setVersion(v);
        setPhase('downloaded');
      }),
    ];
    return () => {
      unsubs.forEach((off) => off());
    };
  }, []);

  if (phase === 'idle') return null;

  const label = version ? `Albear ${version}` : 'A new version';

  if (phase === 'downloaded') {
    return (
      <Alert>
        <Download />
        <AlertTitle>Update ready</AlertTitle>
        <AlertDescription className="flex items-center justify-between gap-2">
          <span>{label} has been downloaded.</span>
          <Button
            size="sm"
            onClick={() =>
              window.electron.ipcRenderer.sendMessage(
                'updater:quit-and-install',
              )
            }
          >
            <RefreshCw />
            Restart to update
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <Alert>
      <Download />
      <AlertTitle>Update available</AlertTitle>
      <AlertDescription>
        {phase === 'downloading'
          ? `${label} is downloading… ${Math.round(percent)}%`
          : `${label} is available and will download in the background.`}
      </AlertDescription>
    </Alert>
  );
}
