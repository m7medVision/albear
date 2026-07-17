// Pairing approval and client revocation.
//
// This is the screen the pairing flow was always meant to have: approving a
// browser extension from a GUI rather than a terminal. The capability list is
// the point of it — an operator consents to the privilege, not to a label.
import * as React from 'react';
import { Check, RefreshCw, ShieldOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { useVault } from '@/VaultContext';
import { unwrap, messageOf } from '@/lib/api';
import {
  clientStatusName,
  STATUS_APPROVED,
  STATUS_REVOKED,
  type ClientView,
  type PendingPairingView,
} from '../../shared/vaultTypes';

function kindLabel(kind: number, fallback?: string): string {
  return fallback ?? `kind ${kind}`;
}

export function ClientsSection(): React.ReactElement {
  const { refreshIfLocked } = useVault();
  const [pending, setPending] = React.useState<PendingPairingView[]>([]);
  const [clients, setClients] = React.useState<ClientView[]>([]);
  const [err, setErr] = React.useState<string | undefined>();
  const [busy, setBusy] = React.useState<string | null>(null);
  const [confirmRevoke, setConfirmRevoke] = React.useState<string | null>(null);
  const [reload, setReload] = React.useState(0);

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [p, c] = await Promise.all([
          window.albear.clientsPending(),
          window.albear.clientsList(),
        ]);
        if (cancelled) return;
        setPending(unwrap(p).pending);
        setClients(unwrap(c).clients);
        setErr(undefined);
      } catch (e) {
        if (cancelled || refreshIfLocked(e)) return;
        setErr(messageOf(e, 'could not load clients'));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [reload, refreshIfLocked]);

  async function approve(pairingId: string): Promise<void> {
    setBusy(pairingId);
    try {
      unwrap(await window.albear.clientsApprove(pairingId));
      setReload((n) => n + 1);
      setErr(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) setErr(messageOf(e, 'could not approve'));
    } finally {
      setBusy(null);
    }
  }

  async function revoke(id: string): Promise<void> {
    setBusy(id);
    try {
      unwrap(await window.albear.clientsRevoke(id));
      setConfirmRevoke(null);
      setReload((n) => n + 1);
      setErr(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) setErr(messageOf(e, 'could not revoke'));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {err && (
        <Alert variant="destructive">
          <AlertDescription>{err}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Pending pairings</CardTitle>
          <CardDescription>
            Check the phrase matches the one shown by the client before you
            approve it.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {pending.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              nothing is waiting for approval
            </p>
          ) : (
            pending.map((p) => (
              <div
                key={p.pairingId}
                className="flex flex-col gap-3 rounded-md border border-border p-3"
              >
                <div className="flex items-center gap-3">
                  <div className="flex-1 min-w-0">
                    <div className="font-medium truncate">{p.label}</div>
                    <div className="text-sm text-muted-foreground">
                      {kindLabel(p.kind, p.kindName)}
                    </div>
                  </div>
                  <code className="rounded bg-muted px-2 py-1 text-sm tracking-widest">
                    {p.phrase}
                  </code>
                  <Button
                    size="sm"
                    disabled={busy === p.pairingId}
                    onClick={() => void approve(p.pairingId)}
                  >
                    <Check />
                    Approve
                  </Button>
                </div>
                {/* Approving grants exactly these. Showing them is the whole
                    point: consent to the privilege, not to the label. */}
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">
                    approving grants
                  </span>
                  <div className="flex flex-wrap gap-1">
                    {p.capabilities.map((c) => (
                      <Badge key={c} variant="secondary">
                        {c}
                      </Badge>
                    ))}
                  </div>
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Paired clients</CardTitle>
          <CardDescription>
            Revoking a client drops its sessions immediately.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          {clients.length === 0 ? (
            <p className="text-sm text-muted-foreground">no clients yet</p>
          ) : (
            clients.map((c) => (
              <div key={c.id} className="flex flex-col gap-2">
                <div className="flex items-center gap-3">
                  <div className="flex-1 min-w-0">
                    <div className="font-medium truncate">{c.label}</div>
                    <div className="text-sm text-muted-foreground">
                      {kindLabel(c.kind)}
                      {c.lastSeenMs
                        ? ` · last seen ${new Date(c.lastSeenMs).toLocaleString()}`
                        : ''}
                    </div>
                  </div>
                  <Badge
                    variant={
                      c.status === STATUS_APPROVED
                        ? 'success'
                        : c.status === STATUS_REVOKED
                          ? 'destructive'
                          : 'secondary'
                    }
                  >
                    {clientStatusName(c.status)}
                  </Badge>
                  {c.status !== STATUS_REVOKED && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-destructive hover:text-destructive hover:bg-destructive/10"
                      disabled={busy === c.id}
                      onClick={() => setConfirmRevoke(c.id)}
                    >
                      <ShieldOff />
                      Revoke
                    </Button>
                  )}
                </div>
                {confirmRevoke === c.id && (
                  <div className="flex items-center gap-3 rounded-md border border-border p-3">
                    <span className="flex-1 text-sm">
                      Revoke <strong>{c.label}</strong>? It will have to pair
                      again to reconnect.
                    </span>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setConfirmRevoke(null)}
                    >
                      Cancel
                    </Button>
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={() => void revoke(c.id)}
                    >
                      Revoke
                    </Button>
                  </div>
                )}
              </div>
            ))
          )}
        </CardContent>
      </Card>

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
