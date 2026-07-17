// Record list, search, reveal/copy, and the entry points into the editor.
import * as React from 'react';
import { Check, Copy, Eye, EyeOff, Pencil, Plus, Search, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { RecordEditor, type EditorSeed } from '@/components/RecordEditor';
import { useVault } from '@/VaultContext';
import { unwrap, messageOf } from '@/lib/api';
import type { RecordView, SecretView } from '../../shared/vaultTypes';

const SECRET_LABELS: Array<[keyof SecretView & string, string]> = [
  ['password', 'password'],
  ['apiKey', 'api key'],
  ['apiSecret', 'api secret'],
  ['notes', 'notes'],
];

export function RecordsSection(): React.ReactElement {
  const { refreshIfLocked } = useVault();
  const [records, setRecords] = React.useState<RecordView[]>([]);
  const [query, setQuery] = React.useState('');
  const [err, setErr] = React.useState<string | undefined>();
  const [note, setNote] = React.useState<string | undefined>();
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

  const clearReveal = React.useCallback(() => {
    setRevealedId(null);
    setSecret(null);
  }, []);

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
        setErr(undefined);
      } catch (e) {
        if (cancelled || refreshIfLocked(e)) return;
        setErr(messageOf(e, 'failed to load records'));
      }
    }, 150);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query, reload, refreshIfLocked]);

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
      setErr(messageOf(e, 'copy failed'));
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
      setErr(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) setErr(messageOf(e, 'reveal failed'));
    } finally {
      setRevealBusy(null);
    }
  }

  // Copy a record's password without showing it on screen.
  async function copyPassword(record: RecordView): Promise<void> {
    try {
      const s = unwrap(await window.albear.reveal(record.id));
      const value = s.password ?? s.apiKey ?? s.apiSecret ?? s.notes;
      if (value === undefined) {
        setErr('record has no secret to copy');
        return;
      }
      unwrap(await window.albear.copyText(value));
      markCopied(`row-${record.id}`);
      setErr(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) setErr(messageOf(e, 'copy failed'));
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
      setErr(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) {
        setErr(messageOf(e, 'could not open the record for editing'));
      }
    } finally {
      setRevealBusy(null);
    }
  }

  async function remove(id: string): Promise<void> {
    try {
      unwrap(await window.albear.remove(id));
      setConfirmDelete(null);
      clearReveal();
      setReload((n) => n + 1);
      setErr(undefined);
    } catch (e) {
      if (!refreshIfLocked(e)) setErr(messageOf(e, 'delete failed'));
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

  return (
    <>
      <div className="flex gap-2">
        <div className="relative flex-1">
          <Search className="size-4 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-9"
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
          <AlertDescription className="flex items-start justify-between gap-3">
            <span>{note}</span>
            <Button size="sm" variant="ghost" onClick={() => setNote(undefined)}>
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {err && (
        <Alert variant="destructive">
          <AlertDescription>{err}</AlertDescription>
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
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void edit(r)}
                    disabled={revealBusy === r.id}
                    aria-label={`Edit ${r.name}`}
                  >
                    <Pencil />
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive hover:text-destructive hover:bg-destructive/10"
                    onClick={() => setConfirmDelete(r.id)}
                    aria-label={`Delete ${r.name}`}
                  >
                    <Trash2 />
                  </Button>
                </div>

                {/* Deleting is irreversible — the daemon has no undo and no
                    trash — so it takes a second, deliberate action. */}
                {confirmDelete === r.id && (
                  <div className="flex items-center gap-3 border-t border-border pt-3">
                    <span className="flex-1 text-sm">
                      Delete <strong>{r.name}</strong>? This cannot be undone.
                    </span>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setConfirmDelete(null)}
                    >
                      Cancel
                    </Button>
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={() => void remove(r.id)}
                    >
                      Delete
                    </Button>
                  </div>
                )}

                {revealedId === r.id && secret && (
                  <div className="flex flex-col gap-2 border-t border-border pt-3">
                    {SECRET_LABELS.filter(([field]) => secret[field]).map(
                      ([field, label]) => (
                        <div key={field} className="flex items-center gap-3">
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
                            onClick={() => void copyText(`${r.id}-custom-${k}`, v)}
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
  );
}
