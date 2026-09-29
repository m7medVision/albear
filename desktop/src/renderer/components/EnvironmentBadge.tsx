// DEV marker for the unpackaged app. It is rendered beside the shell rather
// than inside it, so it shows on every phase — above all on the unlock screen,
// where it stops a real master password going into a dev vault.
import * as React from 'react';
import { Badge } from '@/components/ui/badge';

export function EnvironmentBadge(): React.ReactElement | null {
  // window.albear is absent outside Electron, and prod shows nothing.
  if (window.albear?.environment !== 'dev') return null;
  return (
    <Badge
      variant="destructive"
      className="fixed bottom-3 left-3 z-50 pointer-events-none select-none tracking-wider"
    >
      DEV
    </Badge>
  );
}
