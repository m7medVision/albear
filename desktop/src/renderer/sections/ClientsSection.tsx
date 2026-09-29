// Pairing approval and client revocation.
//
// This is the screen the pairing flow was always meant to have: approving a
// browser extension from a GUI rather than a terminal. The capability list is
// the point of it — an operator consents to the privilege, not to a label.
import * as React from 'react';
import { Check, Loader2, RefreshCw, ShieldOff } from 'lucide-react';
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
  clientStatusName,
  STATUS_APPROVED,
  STATUS_REVOKED,
  type ClientView,
  type PendingPairingView,
} from '../../shared/vaultTypes';

/** ClientKind in internal/access/domain/client.go, named for people. */
const KIND_NAMES: Record<number, string> = {
  1: 'Command-line tool',
  2: 'Chrome extension',
  3: 'Chrome native host',
  4: 'Administrative tool',
};

function kindLabel(kind: number, fallback?: string): string {
  return KIND_NAMES[kind] ?? fallback ?? `Unknown client type (${kind})`;
}

/**
 * Plain-language readings of the daemon's capability names
 * (internal/access/domain/capability.go). They only describe what the daemon
 * sent: the list itself still comes from the daemon, the wire name is always
 * shown beside the description, and a name this build does not know is shown
 * as-is rather than dropped.
 */
const CAPABILITY_DESCRIPTIONS: Record<string, string> = {
  'vault.status': 'See whether the vault is locked',
  'vault.unlock': 'Unlock the vault with your master password',
  'vault.lock': 'Lock the vault',
  'records.list': 'List every record',
  'records.read': 'Read record details',
  'records.reveal': 'Reveal the secret of any record',
  'records.write': 'Create and edit any record',
  'records.delete': 'Delete records',
  'records.match': 'Find saved logins for the site you are on',
  'records.revealForOrigin':
    'Fill a saved password on the exact site it belongs to',
  'records.createLogin': 'Save new logins',
  'records.updateLogin': 'Update saved logins',
  'password.generate': 'Generate passwords',
  'backup.create': 'Create backups of the vault',
  'backup.restore': 'Replace the vault from a backup',
  'clients.admin': 'Approve and revoke other clients',
  'vault.changePassword': 'Change the master password',
  relay: 'Relay encrypted messages for the browser extension',
  pair: 'Ask to pair',
};

type Problem = { title: string; detail: string };

function statusVariant(
  status: number,
): 'success' | 'destructive' | 'secondary' {
  if (status === STATUS_APPROVED) return 'success';
  if (status === STATUS_REVOKED) return 'destructive';
  return 'secondary';
}

export function ClientsSection(): React.ReactElement {
  const { refreshIfLocked } = useVault();
  const [pending, setPending] = React.useState<PendingPairingView[]>([]);
  const [clients, setClients] = React.useState<ClientView[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [loading, setLoading] = React.useState(true);
  const [err, setErr] = React.useState<Problem | undefined>();
  const [busy, setBusy] = React.useState<string | null>(null);
  const [confirmRevoke, setConfirmRevoke] = React.useState<string | null>(null);
  const [reload, setReload] = React.useState(0);

  React.useEffect(() => {
    let cancelled = false;
    setLoading(true);
    (async () => {
      try {
        const [p, c] = await Promise.all([
          window.albear.clientsPending(),
          window.albear.clientsList(),
        ]);
        if (cancelled) return;
        setPending(unwrap(p).pending);
        setClients(unwrap(c).clients);
        setLoaded(true);
        setErr(undefined);
      } catch (e) {
        if (cancelled || refreshIfLocked(e)) return;
        setErr({
          title: 'Unable to load clients',
          detail: `${messageOf(e, 'The Albear service did not answer.')} Use Refresh to try again.`,
        });
      } finally {
        if (!cancelled) setLoading(false);
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
      if (!refreshIfLocked(e)) {
        setErr({
          title: 'Unable to approve the pairing',
          detail: `${messageOf(e, 'The Albear service did not answer.')} Start pairing again from the client, then approve the new request.`,
        });
      }
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
      if (!refreshIfLocked(e)) {
        setErr({
          title: 'Unable to revoke the client',
          detail: `${messageOf(e, 'The Albear service did not answer.')} Use Refresh to check its status, then try again.`,
        });
      }
    } finally {
      setBusy(null);
    }
  }

  // Before the first answer, an empty list means "not known yet", not "none":
  // saying "no clients" there would misstate who can reach the vault.
  const notLoaded = (
    <p className="text-sm text-muted-foreground">
      {err ? 'Unavailable until the list loads.' : 'Loading…'}
    </p>
  );

  return (
    <div className="flex flex-col gap-4">
      {err && (
        <Alert variant="destructive">
          <AlertTitle>{err.title}</AlertTitle>
          <AlertDescription>{err.detail}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Pending pairings</CardTitle>
          <CardDescription>
            Approve a request only if its pairing phrase matches the one the
            client shows you.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {!loaded && notLoaded}
          {loaded && pending.length === 0 && (
            <div className="flex flex-col gap-1">
              <p className="text-sm font-medium">No pairing requests</p>
              <p className="text-sm text-muted-foreground text-pretty">
                To connect the browser extension, open it and choose Start
                pairing. Its request appears here for you to approve.
              </p>
            </div>
          )}
          {pending.map((p) => (
            <div
              key={p.pairingId}
              className="flex flex-col gap-3 rounded-md border border-border p-3"
            >
              <div className="flex flex-wrap items-center gap-3">
                <div className="flex-1 min-w-48">
                  <div className="font-medium break-words">{p.label}</div>
                  <div className="text-sm text-muted-foreground">
                    {kindLabel(p.kind, p.kindName)}
                  </div>
                </div>
                <div className="flex flex-col gap-1">
                  <span className="text-xs uppercase tracking-wide text-muted-foreground">
                    Pairing phrase
                  </span>
                  <code className="rounded bg-muted px-2 py-1 text-sm tracking-widest">
                    {p.phrase}
                  </code>
                </div>
              </div>
              {/* Approving grants exactly these. Showing them is the whole
                    point: consent to the privilege, not to the label. */}
              <div className="flex flex-col gap-1.5">
                <span
                  id={`grants-${p.pairingId}`}
                  className="text-xs uppercase tracking-wide text-muted-foreground"
                >
                  If approved, this client can
                </span>
                <ul
                  aria-labelledby={`grants-${p.pairingId}`}
                  className="flex flex-col gap-1 text-sm"
                >
                  {p.capabilities.map((c) => (
                    <li
                      key={c}
                      className="flex flex-wrap items-baseline gap-x-2"
                    >
                      {CAPABILITY_DESCRIPTIONS[c] && (
                        <span>{CAPABILITY_DESCRIPTIONS[c]}</span>
                      )}
                      <code className="text-xs text-muted-foreground">{c}</code>
                    </li>
                  ))}
                </ul>
              </div>
              <Button
                size="sm"
                className="self-start"
                disabled={busy === p.pairingId}
                onClick={() => void approve(p.pairingId)}
              >
                {busy === p.pairingId ? (
                  <Loader2 className="animate-spin" />
                ) : (
                  <Check />
                )}
                Approve pairing
              </Button>
            </div>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Paired clients</CardTitle>
          <CardDescription>
            Revoking a client ends its sessions immediately.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          {!loaded && notLoaded}
          {loaded && clients.length === 0 && (
            <div className="flex flex-col gap-1">
              <p className="text-sm font-medium">No paired clients</p>
              <p className="text-sm text-muted-foreground text-pretty">
                Clients you approve above are listed here, where you can revoke
                them.
              </p>
            </div>
          )}
          {clients.map((c) => (
            <div key={c.id} className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center gap-3">
                <div className="flex-1 min-w-48">
                  <div className="font-medium break-words">{c.label}</div>
                  <div className="text-sm text-muted-foreground">
                    {kindLabel(c.kind)}
                    {c.lastSeenMs
                      ? ` · last seen ${new Date(c.lastSeenMs).toLocaleString()}`
                      : ''}
                  </div>
                </div>
                <Badge variant={statusVariant(c.status)}>
                  {clientStatusName(c.status)}
                </Badge>
                {c.status !== STATUS_REVOKED && (
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive hover:text-destructive hover:bg-destructive/10"
                    disabled={busy === c.id}
                    aria-expanded={confirmRevoke === c.id}
                    onClick={() => setConfirmRevoke(c.id)}
                    aria-label={`Revoke ${c.label}`}
                  >
                    <ShieldOff />
                    Revoke
                  </Button>
                )}
              </div>
              {confirmRevoke === c.id && (
                <div
                  role="group"
                  aria-labelledby={`revoke-${c.id}`}
                  className="flex flex-wrap items-center gap-3 rounded-md border border-destructive/40 p-3"
                >
                  <span
                    id={`revoke-${c.id}`}
                    className="flex-1 min-w-48 text-sm"
                  >
                    Revoke <strong>{c.label}</strong>? It loses access now and
                    has to pair again to reconnect.
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    // The safe choice takes focus, so a stray Enter cancels.
                    autoFocus
                    onClick={() => setConfirmRevoke(null)}
                  >
                    Cancel
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    disabled={busy === c.id}
                    onClick={() => void revoke(c.id)}
                  >
                    {busy === c.id && <Loader2 className="animate-spin" />}
                    Revoke client
                  </Button>
                </div>
              )}
            </div>
          ))}
        </CardContent>
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
