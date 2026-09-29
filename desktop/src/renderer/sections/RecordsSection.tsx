// Record list, search, reveal/copy, and the entry points into the editor.
import * as React from 'react';
import {
  Check,
  Copy,
  Eye,
  EyeOff,
  Loader2,
  Pencil,
  Plus,
  Search,
  Trash2,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { RecordEditor, type EditorSeed } from '@/components/RecordEditor';
import { useVault } from '@/VaultContext';
import { unwrap, messageOf } from '@/lib/api';
import type { RecordView, SecretView } from '../../shared/vaultTypes';

const SECRET_LABELS: Array<[keyof SecretView & string, string]> = [
  ['password', 'Password'],
  ['apiKey', 'API key'],
  ['apiSecret', 'API secret'],
  ['notes', 'Notes'],
];

/**
 * The one secret a row's Copy button copies, per type, and what the button
 * calls it. Named, so the label says what lands on the clipboard; no fallback
 * chain, so a login without a password never quietly copies its notes.
 */
function primarySecret(
  type: string,
  s: SecretView,
): { label: string; value: string | undefined } {
  if (type === 'api') return { label: 'key', value: s.apiKey || s.apiSecret };
  if (type === 'note') return { label: 'note', value: s.notes };
  return { label: 'password', value: s.password };
}

function copyLabel(type: string): string {
  if (type === 'api') return 'Copy key';
  if (type === 'note') return 'Copy note';
  return 'Copy password';
}

/** A failure the list can show, with the one action that recovers from it. */
interface Problem {
  title: string;
  detail: string;
  retry?: () => void;
}

/** One revealed secret: its name, the value, and a named copy button. */
function SecretRow({
  label,
  value,
  copied,
  onCopy,
}: {
  label: string;
  value: string;
  copied: boolean;
  onCopy: () => void;
}): React.ReactElement {
  return (
    <div className="flex items-center gap-3">
      {/* Wraps instead of truncating: a custom field's name is user text and
          can be long. */}
      <span className="text-xs uppercase tracking-wide text-muted-foreground w-24 shrink-0 break-words">
        {label}
      </span>
      <code className="secret flex-1 min-w-0 text-sm break-all select-all rounded bg-muted px-2 py-1">
        {value}
      </code>
      <Button
        size="icon"
        variant="ghost"
        onClick={onCopy}
        aria-label={`Copy ${label}`}
        title={`Copy ${label}`}
      >
        {copied ? <Check /> : <Copy />}
      </Button>
    </div>
  );
}

export function RecordsSection(): React.ReactElement {
  const { refreshIfLocked } = useVault();
  const [records, setRecords] = React.useState<RecordView[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [query, setQuery] = React.useState('');
  const [problem, setProblem] = React.useState<Problem | undefined>();
  const [note, setNote] = React.useState<string | undefined>();
  const [announcement, setAnnouncement] = React.useState('');
  const [revealedId, setRevealedId] = React.useState<string | null>(null);
  const [secret, setSecret] = React.useState<SecretView | null>(null);
  const [revealBusy, setRevealBusy] = React.useState<string | null>(null);
  const [copied, setCopied] = React.useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = React.useState<string | null>(null);
  const [editor, setEditor] = React.useState<
    { mode: 'create' } | { mode: 'edit'; seed: EditorSeed } | null
  >(null);
  const [reload, setReload] = React.useState(0);

  const copiedTimer = React.useRef<number | undefined>(undefined);
  const searchRef = React.useRef<HTMLInputElement>(null);

  const clearReveal = React.useCallback(() => {
    setRevealedId(null);
    setSecret(null);
  }, []);

  const retryLoad = React.useCallback(() => setReload((n) => n + 1), []);

  // Live search with a small debounce. Re-runs on `reload` so a save or delete
  // shows up without waiting for the user to type.
  React.useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      try {
        const q = query.trim();
        const res = unwrap(
          q ? await window.albear.search(q) : await window.albear.list(),
        );
        if (cancelled) return;
        setRecords(res.records);
        setLoaded(true);
        setProblem(undefined);
      } catch (e) {
        if (cancelled || refreshIfLocked(e)) return;
        setProblem({
          title: 'Unable to load records',
          detail: messageOf(e, 'The Albear service did not answer.'),
          retry: retryLoad,
        });
      }
    }, 150);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query, reload, refreshIfLocked, retryLoad]);

  function markCopied(key: string, what: string): void {
    setCopied(key);
    // The icon swap is visual only; this is what a screen reader hears.
    setAnnouncement(`${what} copied to the clipboard.`);
    window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => {
      setCopied(null);
      setAnnouncement('');
    }, 1500);
  }

  async function copyText(
    key: string,
    what: string,
    text: string,
  ): Promise<void> {
    try {
      unwrap(await window.albear.copyText(text));
      markCopied(key, what);
    } catch (e) {
      setProblem({
        title: `Unable to copy the ${what.toLowerCase()}`,
        detail: messageOf(e, 'The clipboard did not accept it.'),
        retry: () => void copyText(key, what, text),
      });
    }
  }

  async function toggleReveal(record: RecordView): Promise<void> {
    if (revealedId === record.id) {
      clearReveal();
      return;
    }
    setRevealBusy(record.id);
    try {
      const s = unwrap(await window.albear.reveal(record.id));
      setSecret(s);
      setRevealedId(record.id);
      setProblem(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) {
        setProblem({
          title: `Unable to reveal ${record.name}`,
          detail: messageOf(e, 'The Albear service did not answer.'),
          retry: () => void toggleReveal(record),
        });
      }
    } finally {
      setRevealBusy(null);
    }
  }

  // Copy a record's main secret without showing it on screen.
  async function copyPrimary(record: RecordView): Promise<void> {
    try {
      const s = unwrap(await window.albear.reveal(record.id));
      const { label, value } = primarySecret(record.type, s);
      if (!value) {
        setProblem({
          title: `${record.name} has no ${label} to copy`,
          detail: `Edit the record to add a ${label}, or reveal it to copy another field.`,
        });
        return;
      }
      unwrap(await window.albear.copyText(value));
      markCopied(`row-${record.id}`, label[0].toUpperCase() + label.slice(1));
      setProblem(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) {
        setProblem({
          title: `Unable to copy from ${record.name}`,
          detail: messageOf(e, 'The Albear service did not answer.'),
          retry: () => void copyPrimary(record),
        });
      }
    }
  }

  /**
   * Open the editor on an existing record.
   *
   * The reveal is not optional: records.update replaces the record, so the form
   * has to hold every current secret or saving would write empties over them.
   * If the reveal fails, the editor does not open — better no editor than one
   * that silently discards secrets on save.
   */
  async function edit(record: RecordView): Promise<void> {
    setRevealBusy(record.id);
    try {
      const revealed = unwrap(await window.albear.reveal(record.id));
      setEditor({ mode: 'edit', seed: { record, secret: revealed } });
      setProblem(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) {
        setProblem({
          title: `Unable to open ${record.name} for editing`,
          detail: messageOf(e, 'The Albear service did not answer.'),
          retry: () => void edit(record),
        });
      }
    } finally {
      setRevealBusy(null);
    }
  }

  async function remove(record: RecordView): Promise<void> {
    try {
      unwrap(await window.albear.remove(record.id));
      setConfirmDelete(null);
      clearReveal();
      setReload((n) => n + 1);
      setProblem(undefined);
      setAnnouncement(`${record.name} deleted.`);
    } catch (e) {
      if (!refreshIfLocked(e)) {
        setProblem({
          title: `Unable to delete ${record.name}`,
          detail: messageOf(e, 'The Albear service did not answer.'),
          retry: () => void remove(record),
        });
      }
    }
  }

  if (editor) {
    return (
      <RecordEditor
        seed={editor.mode === 'edit' ? editor.seed : undefined}
        onCancel={() => setEditor(null)}
        onSaved={() => {
          setEditor(null);
          clearReveal();
          setReload((n) => n + 1);
        }}
        onConflict={() => {
          setEditor(null);
          clearReveal();
          setReload((n) => n + 1);
          setNote(
            'That record changed somewhere else while you were editing it, so your changes were not saved. It has been reloaded — reopen it and make your changes again.',
          );
        }}
      />
    );
  }

  const q = query.trim();

  return (
    <>
      {/* Rendered empty from the start so later messages are announced. */}
      <p role="status" className="sr-only">
        {announcement}
      </p>

      <div className="flex flex-wrap gap-2">
        <div className="relative flex-1 min-w-48">
          <Search className="size-4 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
          <Input
            ref={searchRef}
            type="search"
            className="pl-9"
            aria-label="Search records"
            placeholder="Search records…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        <Button onClick={() => setEditor({ mode: 'create' })}>
          <Plus />
          New record
        </Button>
      </div>

      {note && (
        <Alert>
          <AlertTitle>Your changes were not saved</AlertTitle>
          <AlertDescription className="flex flex-wrap items-start justify-between gap-3">
            <span className="flex-1 min-w-48">{note}</span>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setNote(undefined)}
            >
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {problem && (
        <Alert variant="destructive">
          <AlertTitle>{problem.title}</AlertTitle>
          <AlertDescription className="flex flex-col gap-2">
            <span>{problem.detail}</span>
            <div className="flex flex-wrap gap-2">
              {problem.retry && (
                <Button size="sm" variant="secondary" onClick={problem.retry}>
                  Try again
                </Button>
              )}
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setProblem(undefined)}
              >
                Dismiss
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}

      <div className="flex flex-col gap-2" aria-busy={!loaded}>
        {/* Until the first answer, "empty" would be a claim we cannot make. */}
        {!loaded && !problem && (
          <Card>
            <CardContent className="flex items-center justify-center gap-2 text-sm text-muted-foreground py-6">
              <Loader2 className="size-4 animate-spin" />
              Loading records…
            </CardContent>
          </Card>
        )}
        {loaded && records.length === 0 && (
          <Card>
            <CardContent className="flex flex-col items-center gap-2 text-center py-8">
              {q ? (
                <>
                  <p className="font-medium">No records match “{q}”</p>
                  <p className="text-sm text-muted-foreground text-pretty">
                    Check the spelling, or clear the search to see every record.
                  </p>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="mt-2"
                    onClick={() => {
                      setQuery('');
                      searchRef.current?.focus();
                    }}
                  >
                    Clear search
                  </Button>
                </>
              ) : (
                <>
                  <p className="font-medium">No records yet</p>
                  <p className="text-sm text-muted-foreground text-pretty max-w-sm">
                    Records keep your logins, API credentials and secure notes,
                    encrypted in this vault.
                  </p>
                  <Button
                    size="sm"
                    className="mt-2"
                    onClick={() => setEditor({ mode: 'create' })}
                  >
                    <Plus />
                    Create your first record
                  </Button>
                </>
              )}
            </CardContent>
          </Card>
        )}
        {loaded &&
          records.map((r) => {
            const revealed = revealedId === r.id && secret;
            const subtitle = [r.username, r.service, r.environment]
              .filter(Boolean)
              .join(' · ');
            return (
              <Card key={r.id}>
                <CardContent className="flex flex-col gap-3 p-4">
                  {/* Wraps at narrow widths: the actions drop below the name
                      instead of pushing past the card edge. */}
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                    <div className="flex-1 min-w-40">
                      <div className="flex items-center gap-2">
                        <div
                          className="text-base font-medium truncate"
                          title={r.name}
                        >
                          {r.name}
                        </div>
                        {r.type !== 'login' && (
                          <Badge variant="outline" className="shrink-0">
                            {r.type}
                          </Badge>
                        )}
                      </div>
                      {subtitle && (
                        <div
                          className="text-sm text-muted-foreground truncate"
                          title={subtitle}
                        >
                          {subtitle}
                        </div>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => void copyPrimary(r)}
                        aria-label={`${copyLabel(r.type)} for ${r.name}`}
                      >
                        {copied === `row-${r.id}` ? <Check /> : <Copy />}
                        {copyLabel(r.type)}
                      </Button>
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => void toggleReveal(r)}
                        disabled={revealBusy === r.id}
                        aria-expanded={Boolean(revealed)}
                        aria-label={`${revealed ? 'Hide' : 'Reveal'} ${r.name}`}
                      >
                        {revealBusy === r.id && (
                          <Loader2 className="animate-spin" />
                        )}
                        {revealBusy !== r.id &&
                          (revealed ? <EyeOff /> : <Eye />)}
                        {revealed ? 'Hide' : 'Reveal'}
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        onClick={() => void edit(r)}
                        disabled={revealBusy === r.id}
                        aria-label={`Edit ${r.name}`}
                        title="Edit"
                      >
                        <Pencil />
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        className="text-destructive hover:text-destructive hover:bg-destructive/10"
                        onClick={() => setConfirmDelete(r.id)}
                        aria-label={`Delete ${r.name}`}
                        title="Delete"
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </div>

                  {/* Deleting is irreversible — the daemon has no undo and no
                      trash — so it takes a second, deliberate action. Focus
                      lands on Cancel, the safe choice. */}
                  {confirmDelete === r.id && (
                    <div
                      role="group"
                      aria-labelledby={`delete-${r.id}-label`}
                      className="flex flex-wrap items-center gap-3 border-t border-border pt-3"
                    >
                      <span
                        id={`delete-${r.id}-label`}
                        className="flex-1 min-w-48 text-sm"
                      >
                        Delete <strong>{r.name}</strong>? This cannot be undone.
                      </span>
                      <Button
                        size="sm"
                        variant="ghost"
                        autoFocus
                        onClick={() => setConfirmDelete(null)}
                      >
                        Cancel
                      </Button>
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={() => void remove(r)}
                      >
                        <Trash2 />
                        Delete record
                      </Button>
                    </div>
                  )}

                  {revealed && (
                    <div className="flex flex-col gap-2 border-t border-border pt-3">
                      {SECRET_LABELS.filter(([field]) => secret[field]).map(
                        ([field, label]) => (
                          <SecretRow
                            key={field}
                            label={label}
                            value={secret[field] as string}
                            copied={copied === `${r.id}-${field}`}
                            onCopy={() =>
                              void copyText(
                                `${r.id}-${field}`,
                                label,
                                secret[field] as string,
                              )
                            }
                          />
                        ),
                      )}
                      {secret.custom &&
                        Object.entries(secret.custom).map(([k, v]) => (
                          <SecretRow
                            key={k}
                            label={k}
                            value={v}
                            copied={copied === `${r.id}-custom-${k}`}
                            onCopy={() =>
                              void copyText(`${r.id}-custom-${k}`, k, v)
                            }
                          />
                        ))}
                    </div>
                  )}
                </CardContent>
              </Card>
            );
          })}
      </div>
    </>
  );
}
