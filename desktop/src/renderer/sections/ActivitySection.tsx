// Recent security events.
//
// The daemon records events as numeric codes precisely so that nothing
// sensitive rides along, and it is bound by the rule that secrets never reach
// logs. So this view renders what it is given rather than filtering it: a
// client-side scrub would imply a distrust the architecture does not support,
// and would hide the very events an operator opens this screen to see.
import * as React from 'react';
import { Loader2, OctagonAlert, RefreshCw, TriangleAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
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
import {
  eventName,
  severityName,
  SEVERITY_CRITICAL,
  SEVERITY_WARNING,
  type EventView,
} from '../../shared/vaultTypes';

function severityVariant(
  severity: number,
): 'secondary' | 'destructive' | 'warning' {
  if (severity === SEVERITY_CRITICAL) return 'destructive';
  if (severity === SEVERITY_WARNING) return 'warning';
  return 'secondary';
}

// Severity is never colour alone: the icon (and the badge text) carry it too.
function SeverityIcon({
  severity,
}: {
  severity: number;
}): React.ReactElement | null {
  if (severity === SEVERITY_CRITICAL)
    return <OctagonAlert aria-hidden className="size-3" />;
  if (severity === SEVERITY_WARNING)
    return <TriangleAlert aria-hidden className="size-3" />;
  return null;
}

// Seconds matter in a security log; the locale decides order and separators.
const TIME_FORMAT = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'medium',
});

export function ActivitySection(): React.ReactElement {
  const { refreshIfLocked } = useVault();
  const [events, setEvents] = React.useState<EventView[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [loading, setLoading] = React.useState(true);
  const [err, setErr] = React.useState<string | undefined>();
  const [reload, setReload] = React.useState(0);

  React.useEffect(() => {
    let cancelled = false;
    setLoading(true);
    (async () => {
      try {
        // No limit: the daemon's own default (50) is the right ceiling for a
        // "recent activity" view, and it caps nothing itself.
        const res = unwrap(await window.albear.events());
        if (cancelled) return;
        setEvents(res.events);
        setLoaded(true);
        setErr(undefined);
      } catch (e) {
        if (cancelled || refreshIfLocked(e)) return;
        setErr(messageOf(e, 'The Albear service did not answer.'));
      } finally {
        if (!cancelled) setLoading(false);
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
          <AlertTitle>Unable to load recent activity</AlertTitle>
          <AlertDescription>{err} Use Refresh to try again.</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Recent activity</CardTitle>
          <CardDescription>
            The latest security events recorded by the vault, newest first.
          </CardDescription>
        </CardHeader>
        {/* Before the first answer an empty list means "not known yet", not
            "nothing happened": never claim a quiet log that was never read. */}
        {!loaded && (
          <CardContent className="text-sm text-muted-foreground">
            {err ? 'Unavailable until the log loads.' : 'Loading…'}
          </CardContent>
        )}
        {loaded && ordered.length === 0 && (
          <CardContent className="flex flex-col gap-1">
            <p className="text-sm font-medium">No security events yet</p>
            <p className="text-sm text-muted-foreground text-pretty">
              Unlocks, locks, pairings and backups are recorded here as they
              happen.
            </p>
          </CardContent>
        )}
        {ordered.length > 0 && (
          <CardContent className="p-0">
            <ol className="flex flex-col divide-y divide-border border-t border-border">
              {ordered.map((e) => (
                <li
                  key={e.sequence}
                  className="flex flex-wrap items-start gap-x-3 gap-y-1 px-3 py-2.5"
                >
                  <Badge
                    variant={severityVariant(e.severity)}
                    className="gap-1 shrink-0"
                  >
                    <SeverityIcon severity={e.severity} />
                    {severityName(e.severity)}
                  </Badge>
                  <div className="flex-1 min-w-40">
                    <div className="text-sm break-words">
                      {eventName(e.code)}
                    </div>
                    {e.details && (
                      <div className="text-xs text-muted-foreground break-words">
                        {e.details}
                      </div>
                    )}
                  </div>
                  <time
                    dateTime={new Date(e.occurredMs).toISOString()}
                    className="text-xs text-muted-foreground shrink-0 tabular-nums"
                  >
                    {TIME_FORMAT.format(e.occurredMs)}
                  </time>
                </li>
              ))}
            </ol>
          </CardContent>
        )}
      </Card>

      <Button
        variant="ghost"
        size="sm"
        className="self-start"
        disabled={loading}
        onClick={() => setReload((n) => n + 1)}
      >
        {loading ? <Loader2 className="animate-spin" /> : <RefreshCw />}
        Refresh
      </Button>
    </div>
  );
}
