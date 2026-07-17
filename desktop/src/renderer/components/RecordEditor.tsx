// Create/edit form for a record.
//
// Two daemon behaviours drive this component's shape, and both fail silently if
// ignored:
//
//  1. records.update REPLACES the record. A secret field absent from the
//     payload is stored empty, over the old value. So editing an existing
//     record requires its revealed secrets to be loaded in here first, and save
//     submits all of them. The caller reveals; this component refuses to open
//     for an edit without them.
//  2. The daemon reads urlEntries; given a plain urls list it defaults every
//     entry to exact matching. So the subdomain opt-in is edited and written
//     back as entries, and `urls` is never sent.
import * as React from 'react';
import { Loader2, Plus, RefreshCw, Trash2, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { cn } from '@/lib/utils';
import { unwrap, isCode, messageOf, CONFLICT } from '@/lib/api';
import {
  RECORD_TYPES,
  type RecordFields,
  type RecordType,
  type RecordView,
  type SecretView,
  type UrlEntry,
} from '../../shared/vaultTypes';

export interface EditorSeed {
  record: RecordView;
  secret: SecretView;
}

interface Props {
  /** Absent for a new record; present (with revealed secrets) for an edit. */
  seed?: EditorSeed;
  onCancel: () => void;
  onSaved: () => void;
  /** Called when the daemon reports the record changed under us. */
  onConflict: () => void;
}

interface CustomField {
  key: string;
  value: string;
}

function toCustomFields(custom?: Record<string, string>): CustomField[] {
  return Object.entries(custom ?? {}).map(([key, value]) => ({ key, value }));
}

/**
 * What the daemon requires beyond a name, per type (Record.Validate). Stated
 * here so the form can explain itself rather than bouncing the user off an
 * opaque validation error.
 */
function requirementHint(type: RecordType): string | undefined {
  if (type === 'login') {
    return 'A login needs a password, a username, or at least one URL.';
  }
  if (type === 'api') return 'An API credential needs a key or a secret.';
  return undefined;
}

function meetsTypeRequirement(type: RecordType, f: RecordFields): boolean {
  if (type === 'login') {
    return (
      (f.password ?? '') !== '' ||
      (f.username ?? '') !== '' ||
      (f.urlEntries?.length ?? 0) > 0
    );
  }
  if (type === 'api') {
    return (f.apiKey ?? '') !== '' || (f.apiSecret ?? '') !== '';
  }
  return true;
}

export function RecordEditor({
  seed,
  onCancel,
  onSaved,
  onConflict,
}: Props): React.ReactElement {
  const editing = seed !== undefined;

  const [type, setType] = React.useState<RecordType>(
    (seed?.record.type as RecordType) ?? 'login',
  );
  const [name, setName] = React.useState(seed?.record.name ?? '');
  const [username, setUsername] = React.useState(seed?.record.username ?? '');
  const [service, setService] = React.useState(seed?.record.service ?? '');
  const [environment, setEnvironment] = React.useState(
    seed?.record.environment ?? '',
  );
  const [tags, setTags] = React.useState((seed?.record.tags ?? []).join(', '));
  // Prefer urlEntries: `urls` carries no policy, and falling back to it would
  // reset every opt-in to exact on save.
  const [urls, setUrls] = React.useState<UrlEntry[]>(
    seed?.record.urlEntries ?? [],
  );
  const [password, setPassword] = React.useState(seed?.secret.password ?? '');
  const [notes, setNotes] = React.useState(seed?.secret.notes ?? '');
  const [apiKey, setApiKey] = React.useState(seed?.secret.apiKey ?? '');
  const [apiSecret, setApiSecret] = React.useState(
    seed?.secret.apiSecret ?? '',
  );
  const [custom, setCustom] = React.useState<CustomField[]>(
    toCustomFields(seed?.secret.custom),
  );

  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState<string | undefined>();

  function buildFields(): RecordFields {
    const fields: RecordFields = { name: name.trim() };
    if (username.trim()) fields.username = username.trim();
    if (service.trim()) fields.service = service.trim();
    if (environment.trim()) fields.environment = environment.trim();

    const tagList = tags
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean);
    if (tagList.length) fields.tags = tagList;

    const entries = urls.filter((u) => u.url.trim() !== '');
    if (entries.length) {
      fields.urlEntries = entries.map((u) =>
        u.sub ? { url: u.url.trim(), sub: true } : { url: u.url.trim() },
      );
    }

    // Every secret goes on the payload, always. On update the daemon rebuilds
    // the record from exactly this — omitting an untouched field would wipe it.
    fields.password = password;
    fields.notes = notes;
    fields.apiKey = apiKey;
    fields.apiSecret = apiSecret;

    const customMap: Record<string, string> = {};
    for (const { key, value } of custom) {
      if (key.trim()) customMap[key.trim()] = value;
    }
    if (Object.keys(customMap).length) fields.custom = customMap;

    return fields;
  }

  const draft = buildFields();
  const nameOk = draft.name.length > 0;
  const typeOk = meetsTypeRequirement(type, draft);
  const canSave = nameOk && typeOk && !busy;

  async function save(): Promise<void> {
    if (!canSave) return;
    setErr(undefined);
    setBusy(true);
    try {
      const fields = buildFields();
      if (editing && seed) {
        unwrap(
          await window.albear.update({
            ...fields,
            id: seed.record.id,
            expectedRevision: seed.record.revision,
          }),
        );
      } else {
        unwrap(await window.albear.create({ ...fields, type }));
      }
      onSaved();
    } catch (e) {
      // A conflict is not this form's to resolve: overwriting would discard
      // the other writer's secret with no way back.
      if (isCode(e, CONFLICT)) {
        onConflict();
        return;
      }
      setErr(messageOf(e, 'could not save the record'));
    } finally {
      setBusy(false);
    }
  }

  async function generate(): Promise<void> {
    try {
      const { password: generated } = unwrap(await window.albear.generate());
      setPassword(generated);
    } catch (e) {
      setErr(messageOf(e, 'could not generate a password'));
    }
  }

  const hint = requirementHint(type);

  return (
    <Card>
      <CardHeader>
        <CardTitle>{editing ? 'Edit record' : 'New record'}</CardTitle>
        <CardDescription>
          {editing
            ? 'Saving replaces every field on this record.'
            : 'Secrets are encrypted by the daemon before they touch disk.'}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="record-type">Type</Label>
          <select
            id="record-type"
            className={cn(
              'flex h-9 w-full rounded-md border border-input bg-background px-3 text-sm',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
              'disabled:cursor-not-allowed disabled:opacity-50',
            )}
            value={type}
            disabled={editing}
            onChange={(e) => setType(e.target.value as RecordType)}
          >
            {RECORD_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          {editing && (
            // The daemon reads the stored type on update and ignores the one
            // sent, so offering to change it here would be a lie.
            <p className="text-xs text-muted-foreground">
              a record&apos;s type cannot be changed after it is created
            </p>
          )}
        </div>

        <Field label="Name" required>
          <Input
            autoFocus
            value={name}
            placeholder="What is this record for?"
            onChange={(e) => setName(e.target.value)}
          />
        </Field>

        {type !== 'note' && (
          <Field label="Username">
            <Input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </Field>
        )}

        <div className="grid grid-cols-2 gap-3">
          <Field label="Service">
            <Input
              value={service}
              onChange={(e) => setService(e.target.value)}
            />
          </Field>
          <Field label="Environment">
            <Input
              value={environment}
              onChange={(e) => setEnvironment(e.target.value)}
            />
          </Field>
        </div>

        {type === 'login' && (
          <Field label="Password">
            <div className="flex gap-2">
              <Input
                type="text"
                className="secret font-mono"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <Button
                type="button"
                variant="secondary"
                onClick={() => void generate()}
                title="Generate a password"
              >
                <RefreshCw />
                Generate
              </Button>
            </div>
          </Field>
        )}

        {type === 'api' && (
          <>
            <Field label="API key">
              <Input
                className="secret font-mono"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
              />
            </Field>
            <Field label="API secret">
              <Input
                className="secret font-mono"
                value={apiSecret}
                onChange={(e) => setApiSecret(e.target.value)}
              />
            </Field>
          </>
        )}

        {type !== 'note' && (
          <UrlEditor entries={urls} onChange={setUrls} />
        )}

        <Field label="Notes">
          <Textarea
            className="secret"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </Field>

        <Field label="Tags">
          <Input
            value={tags}
            placeholder="comma, separated"
            onChange={(e) => setTags(e.target.value)}
          />
        </Field>

        <CustomEditor fields={custom} onChange={setCustom} />

        {hint && !typeOk && (
          <p className="text-sm text-muted-foreground">{hint}</p>
        )}

        {err && (
          <Alert variant="destructive">
            <AlertTitle>Cannot save</AlertTitle>
            <AlertDescription>{err}</AlertDescription>
          </Alert>
        )}

        <div className="flex gap-2">
          <Button onClick={() => void save()} disabled={!canSave}>
            {busy && <Loader2 className="animate-spin" />}
            {editing ? 'Save changes' : 'Create record'}
          </Button>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function Field({
  label,
  required,
  children,
}: {
  label: string;
  required?: boolean;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-xs uppercase tracking-wide text-muted-foreground">
        {label}
        {required && <span className="text-destructive"> *</span>}
      </span>
      {children}
    </label>
  );
}

function UrlEditor({
  entries,
  onChange,
}: {
  entries: UrlEntry[];
  onChange: (next: UrlEntry[]) => void;
}): React.ReactElement {
  function update(i: number, patch: Partial<UrlEntry>): void {
    onChange(entries.map((e, idx) => (idx === i ? { ...e, ...patch } : e)));
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          URLs
        </span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => onChange([...entries, { url: '' }])}
        >
          <Plus />
          Add URL
        </Button>
      </div>
      {entries.length === 0 ? (
        <p className="text-sm text-muted-foreground">no URLs</p>
      ) : (
        entries.map((entry, i) => (
          // eslint-disable-next-line react/no-array-index-key
          <div key={i} className="flex flex-col gap-1.5">
            <div className="flex gap-2">
              <Input
                value={entry.url}
                placeholder="https://example.com"
                onChange={(e) => update(i, { url: e.target.value })}
              />
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => onChange(entries.filter((_, idx) => idx !== i))}
                aria-label="Remove URL"
              >
                <X />
              </Button>
            </div>
            {/* The only place this opt-in can be set. The extension never sends
                it: widening a record's matching is an editor decision, not one
                inferred from a page. */}
            <label className="flex items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                className="size-4 rounded border-input accent-primary"
                checked={entry.sub ?? false}
                onChange={(e) => update(i, { sub: e.target.checked })}
              />
              also match subdomains of this address
            </label>
          </div>
        ))
      )}
    </div>
  );
}

function CustomEditor({
  fields,
  onChange,
}: {
  fields: CustomField[];
  onChange: (next: CustomField[]) => void;
}): React.ReactElement {
  function update(i: number, patch: Partial<CustomField>): void {
    onChange(fields.map((f, idx) => (idx === i ? { ...f, ...patch } : f)));
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          Custom fields
        </span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => onChange([...fields, { key: '', value: '' }])}
        >
          <Plus />
          Add field
        </Button>
      </div>
      {fields.map((field, i) => (
        // eslint-disable-next-line react/no-array-index-key
        <div key={i} className="flex gap-2">
          <Input
            className="w-1/3"
            value={field.key}
            placeholder="name"
            onChange={(e) => update(i, { key: e.target.value })}
          />
          <Input
            className="secret flex-1"
            value={field.value}
            placeholder="value"
            onChange={(e) => update(i, { value: e.target.value })}
          />
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => onChange(fields.filter((_, idx) => idx !== i))}
            aria-label="Remove custom field"
          >
            <Trash2 />
          </Button>
        </div>
      ))}
    </div>
  );
}
