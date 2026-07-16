// Desktop vault UI, mirroring the Chrome extension popup UX
// (extension/src/popup/App.tsx): daemon status states, unlock form, record
// list with search, per-record reveal and copy, lock button.
import * as React from 'react';
import {
  Check,
  Copy,
  Eye,
  EyeOff,
  Loader2,
  Lock,
  RefreshCw,
  Search,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { UpdateBanner } from '@/components/UpdateBanner';
import type {
  AlbearResult,
  RecordView,
  SecretView,
} from '../shared/vaultTypes';
// The app mark itself, rather than a stand-in glyph. The 128px source keeps it
// crisp on HiDPI at its 28px display size.
import appIcon from '../../assets/icons/128x128.png';
import '@/styles/globals.css';

type Phase =
  'connecting' | 'unavailable' | 'uninitialized' | 'locked' | 'unlocked';

class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

function unwrap<T>(result: AlbearResult<T>): T {
  if (!result.ok) throw new ApiError(result.error.code, result.error.message);
  return result.data;
}

function badgeFor(phase: Phase): {
  text: string;
  variant: 'default' | 'secondary' | 'destructive' | 'success' | 'outline';
} {
  switch (phase) {
    case 'connecting':
      return { text: 'connecting…', variant: 'outline' };
    case 'unavailable':
      return { text: 'daemon unreachable', variant: 'destructive' };
    case 'uninitialized':
      return { text: 'no vault', variant: 'outline' };
    case 'locked':
      return { text: 'locked', variant: 'secondary' };
    // Lock state is the one thing worth spotting without reading: green
    // unlocked, grey locked, red unreachable.
    default:
      return { text: 'unlocked', variant: 'success' };
  }
}

const SECRET_LABELS: Array<[keyof SecretView & string, string]> = [
  ['password', 'password'],
  ['apiKey', 'api key'],
  ['apiSecret', 'api secret'],
  ['notes', 'notes'],
];

export default function App(): React.ReactElement {
  const [phase, setPhase] = React.useState<Phase>('connecting');
  const [records, setRecords] = React.useState<RecordView[]>([]);
  const [query, setQuery] = React.useState('');
  const [listErr, setListErr] = React.useState<string | undefined>();
  const [password, setPassword] = React.useState('');
  const [unlocking, setUnlocking] = React.useState(false);
  const [unlockErr, setUnlockErr] = React.useState<string | undefined>();
  const [revealedId, setRevealedId] = React.useState<string | null>(null);
  const [secret, setSecret] = React.useState<SecretView | null>(null);
  const [revealBusy, setRevealBusy] = React.useState<string | null>(null);
  const [copied, setCopied] = React.useState<string | null>(null);

  const copiedTimer = React.useRef<number | undefined>(undefined);

  const clearReveal = React.useCallback(() => {
    setRevealedId(null);
    setSecret(null);
  }, []);

  const refresh = React.useCallback(async (): Promise<void> => {
    // window.albear is absent outside Electron (e.g. jest/jsdom without a mock).
    if (!window.albear) {
      setPhase('unavailable');
      return;
    }
    try {
      const st = unwrap(await window.albear.status());
      if (!st.available) setPhase('unavailable');
      else if (!st.initialized) setPhase('uninitialized');
      else if (!st.unlocked) setPhase('locked');
      else setPhase('unlocked');
    } catch {
      setPhase('unavailable');
    }
  }, []);

  // Initial connect + keep retrying while the daemon is unreachable.
  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  React.useEffect(() => {
    if (phase !== 'unavailable' && phase !== 'connecting') return undefined;
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => window.clearInterval(timer);
  }, [phase, refresh]);

  // Load records whenever unlocked; live search with a small debounce.
  React.useEffect(() => {
    if (phase !== 'unlocked') {
      setRecords([]);
      clearReveal();
      return undefined;
    }
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      try {
        const q = query.trim();
        const res = unwrap(
          q ? await window.albear.search(q) : await window.albear.list(),
        );
        if (cancelled) return;
        setRecords(res.records);
        setListErr(undefined);
      } catch (e) {
        if (cancelled) return;
        if (e instanceof ApiError && e.code === 'VAULT_LOCKED') {
          void refresh();
          return;
        }
        setListErr(e instanceof Error ? e.message : 'failed to load records');
      }
    }, 150);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [phase, query, refresh, clearReveal]);

  async function unlock(): Promise<void> {
    if (!password || unlocking) return;
    setUnlockErr(undefined);
    setUnlocking(true);
    try {
      unwrap(await window.albear.unlock(password));
      setPassword('');
      await refresh();
    } catch (e) {
      if (e instanceof ApiError && e.code === 'RATE_LIMITED') {
        setUnlockErr('too many attempts — wait a moment');
      } else if (e instanceof ApiError && e.code === 'AUTH_FAILED') {
        setUnlockErr('wrong password');
      } else if (e instanceof ApiError && e.code === 'DAEMON_UNAVAILABLE') {
        setUnlockErr('lost connection to vaultd');
        void refresh();
      } else {
        setUnlockErr(e instanceof Error ? e.message : 'unlock failed');
      }
    } finally {
      setUnlocking(false);
    }
  }

  async function lock(): Promise<void> {
    clearReveal();
    try {
      unwrap(await window.albear.lock());
    } finally {
      void refresh();
    }
  }

  async function toggleReveal(id: string): Promise<void> {
    if (revealedId === id) {
      clearReveal();
      return;
    }
    setRevealBusy(id);
    try {
      const s = unwrap(await window.albear.reveal(id));
      setSecret(s);
      setRevealedId(id);
      setListErr(undefined);
    } catch (e) {
      if (e instanceof ApiError && e.code === 'VAULT_LOCKED') void refresh();
      else setListErr(e instanceof Error ? e.message : 'reveal failed');
    } finally {
      setRevealBusy(null);
    }
  }

  function markCopied(key: string): void {
    setCopied(key);
    window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => setCopied(null), 1500);
  }

  async function copyText(key: string, text: string): Promise<void> {
    try {
      unwrap(await window.albear.copyText(text));
      markCopied(key);
    } catch (e) {
      setListErr(e instanceof Error ? e.message : 'copy failed');
    }
  }

  // Copy a record's password without showing it on screen.
  async function copyPassword(record: RecordView): Promise<void> {
    try {
      const s = unwrap(await window.albear.reveal(record.id));
      const value = s.password ?? s.apiKey ?? s.apiSecret ?? s.notes;
      if (value === undefined) {
        setListErr('record has no secret to copy');
        return;
      }
      unwrap(await window.albear.copyText(value));
      markCopied(`row-${record.id}`);
      setListErr(undefined);
    } catch (e) {
      if (e instanceof ApiError && e.code === 'VAULT_LOCKED') void refresh();
      else setListErr(e instanceof Error ? e.message : 'copy failed');
    }
  }

  const badge = badgeFor(phase);

  return (
    <div className="min-h-screen flex flex-col">
      {/* Full-bleed rule, but the controls line up with the content column
          below rather than drifting out to the window edges. */}
      <header className="px-6 py-4 border-b border-border">
        <div className="w-full max-w-3xl mx-auto flex items-center gap-3">
          {/* The mark is drawn for a light field — its own background is
              transparent, so it muddies against a dark header. The white tile
              is fixed in both themes to keep the shield legible. */}
          <img
            src={appIcon}
            alt=""
            className="size-7 rounded-md shrink-0 bg-white p-0.5 ring-1 ring-border"
          />
          {/* No dir="rtl": the flex-1 heading would right-align the wordmark
              away from the logo. Bidi already shapes the word itself. */}
          <h1
            className="flex-1 font-arabic text-2xl font-bold leading-none"
            lang="ar"
          >
            البير
          </h1>
          {phase === 'unlocked' && (
            <Button variant="secondary" size="sm" onClick={() => void lock()}>
              <Lock />
              Lock
            </Button>
          )}
          <Badge variant={badge.variant}>{badge.text}</Badge>
        </div>
      </header>

      <main className="flex-1 px-6 py-5 w-full max-w-3xl mx-auto flex flex-col gap-4">
        <UpdateBanner />

        {phase === 'connecting' && (
          <p className="text-sm text-muted-foreground text-center py-10">
            connecting to vaultd…
          </p>
        )}

        {phase === 'unavailable' && (
          <Alert variant="destructive">
            <AlertTitle>Cannot reach the vault daemon</AlertTitle>
            <AlertDescription className="flex flex-col gap-2">
              <span>
                is vaultd running? Start it in a terminal, then retry.
              </span>
              <Button
                variant="secondary"
                size="sm"
                className="self-start"
                onClick={() => void refresh()}
              >
                <RefreshCw />
                Retry
              </Button>
            </AlertDescription>
          </Alert>
        )}

        {phase === 'uninitialized' && (
          <Card>
            <CardHeader>
              <CardTitle>No vault yet</CardTitle>
              <CardDescription>
                Create one by running <code>vault init</code> in a terminal,
                then retry.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void refresh()}
              >
                <RefreshCw />
                Retry
              </Button>
            </CardContent>
          </Card>
        )}

        {phase === 'locked' && (
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
              <Button onClick={() => void unlock()} disabled={unlocking}>
                {unlocking && <Loader2 className="animate-spin" />}
                Unlock
              </Button>
              {unlockErr && (
                <Alert variant="destructive">
                  <AlertTitle>Cannot unlock</AlertTitle>
                  <AlertDescription>{unlockErr}</AlertDescription>
                </Alert>
              )}
            </CardContent>
          </Card>
        )}

        {phase === 'unlocked' && (
          <>
            <div className="relative">
              <Search className="size-4 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="pl-9"
                placeholder="Search records…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>

            {listErr && (
              <Alert variant="destructive">
                <AlertDescription>{listErr}</AlertDescription>
              </Alert>
            )}

            <div className="flex flex-col gap-2">
              {records.length === 0 ? (
                <Card>
                  <CardContent className="text-sm text-muted-foreground text-center py-6">
                    {query.trim() ? 'no matching records' : 'vault is empty'}
                  </CardContent>
                </Card>
              ) : (
                records.map((r) => (
                  <Card
                    key={r.id}
                    className="transition-colors hover:border-primary/40"
                  >
                    <CardContent className="flex flex-col gap-3 p-4">
                      <div className="flex items-center gap-3">
                        <div className="flex-1 min-w-0">
                          <div className="text-base font-medium truncate">
                            {r.name}
                          </div>
                          <div className="text-sm text-muted-foreground truncate">
                            {[r.username, r.service, r.environment]
                              .filter(Boolean)
                              .join(' · ')}
                          </div>
                        </div>
                        {r.type !== 'login' && (
                          <Badge variant="outline">{r.type}</Badge>
                        )}
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={() => void copyPassword(r)}
                        >
                          {copied === `row-${r.id}` ? <Check /> : <Copy />}
                          Copy
                        </Button>
                        <Button
                          size="sm"
                          onClick={() => void toggleReveal(r.id)}
                          disabled={revealBusy === r.id}
                        >
                          {revealedId === r.id ? <EyeOff /> : <Eye />}
                          {revealedId === r.id ? 'Hide' : 'Reveal'}
                        </Button>
                      </div>

                      {revealedId === r.id && secret && (
                        <div className="flex flex-col gap-2 border-t border-border pt-3">
                          {SECRET_LABELS.filter(([field]) => secret[field]).map(
                            ([field, label]) => (
                              <div
                                key={field}
                                className="flex items-center gap-3"
                              >
                                <span className="text-xs uppercase tracking-wide text-muted-foreground w-20 shrink-0">
                                  {label}
                                </span>
                                <code className="secret flex-1 min-w-0 text-sm break-all select-all rounded bg-muted px-2 py-1">
                                  {secret[field] as string}
                                </code>
                                <Button
                                  size="sm"
                                  variant="ghost"
                                  onClick={() =>
                                    void copyText(
                                      `${r.id}-${field}`,
                                      secret[field] as string,
                                    )
                                  }
                                >
                                  {copied === `${r.id}-${field}` ? (
                                    <Check />
                                  ) : (
                                    <Copy />
                                  )}
                                </Button>
                              </div>
                            ),
                          )}
                          {secret.custom &&
                            Object.entries(secret.custom).map(([k, v]) => (
                              <div key={k} className="flex items-center gap-3">
                                <span className="text-xs uppercase tracking-wide text-muted-foreground w-20 shrink-0 truncate">
                                  {k}
                                </span>
                                <code className="secret flex-1 min-w-0 text-sm break-all select-all rounded bg-muted px-2 py-1">
                                  {v}
                                </code>
                                <Button
                                  size="sm"
                                  variant="ghost"
                                  onClick={() =>
                                    void copyText(`${r.id}-custom-${k}`, v)
                                  }
                                >
                                  {copied === `${r.id}-custom-${k}` ? (
                                    <Check />
                                  ) : (
                                    <Copy />
                                  )}
                                </Button>
                              </div>
                            ))}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                ))
              )}
            </div>
          </>
        )}
      </main>
    </div>
  );
}
