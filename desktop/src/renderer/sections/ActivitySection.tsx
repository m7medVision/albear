// Recent security events.
//
// The daemon records events as numeric codes precisely so that nothing
// sensitive rides along, and it is bound by the rule that secrets never reach
// logs. So this view renders what it is given rather than filtering it: a
// client-side scrub would imply a distrust the architecture does not support,
// and would hide the very events an operator opens this screen to see.
import * as React from 'react';
import { RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { useVault } from '@/VaultContext';
import { unwrap, messageOf } from '@/lib/api';
import {
  eventName,
  severityName,
  SEVERITY_CRITICAL,
  SEVERITY_WARNING,
  type EventView,
} from '../../shared/vaultTypes';

function severityVariant(
  severity: number,
): 'secondary' | 'destructive' | 'outline' {
  if (severity === SEVERITY_CRITICAL) return 'destructive';
  if (severity === SEVERITY_WARNING) return 'outline';
  return 'secondary';
}

export function ActivitySection(): React.ReactElement {
  const { refreshIfLocked } = useVault();
  const [events, setEvents] = React.useState<EventView[]>([]);
  const [err, setErr] = React.useState<string | undefined>();
  const [reload, setReload] = React.useState(0);

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        // No limit: the daemon's own default (50) is the right ceiling for a
        // "recent activity" view, and it caps nothing itself.
        const res = unwrap(await window.albear.events());
        if (cancelled) return;
        setEvents(res.events);
        setErr(undefined);
      } catch (e) {
        if (cancelled || refreshIfLocked(e)) return;
        setErr(messageOf(e, 'could not load recent activity'));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [reload, refreshIfLocked]);

  // The daemon returns these newest-first; sort defensively rather than rely on
  // it, since the sequence is what actually orders them.
  const ordered = [...events].sort((a, b) => b.sequence - a.sequence);

  return (
    <div className="flex flex-col gap-4">
      {err && (
        <Alert variant="destructive">
          <AlertDescription>{err}</AlertDescription>
        </Alert>
      )}

      {ordered.length === 0 ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground text-center py-6">
            no recent activity
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="flex flex-col divide-y divide-border p-0">
            {ordered.map((e) => (
              <div
                key={e.sequence}
                className="flex items-center gap-3 px-4 py-2.5"
              >
                <Badge variant={severityVariant(e.severity)}>
                  {severityName(e.severity)}
                </Badge>
                <div className="flex-1 min-w-0">
                  <div className="text-sm truncate">{eventName(e.code)}</div>
                  {e.details && (
                    <div className="text-xs text-muted-foreground truncate">
                      {e.details}
                    </div>
                  )}
                </div>
                <time className="text-xs text-muted-foreground shrink-0 tabular-nums">
                  {new Date(e.occurredMs).toLocaleString()}
                </time>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <Button
        variant="ghost"
        size="sm"
        className="self-start"
        onClick={() => setReload((n) => n + 1)}
      >
        <RefreshCw />
        Refresh
      </Button>
    </div>
  );
}
