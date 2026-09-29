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

/** What each type is called on screen; the wire value stays the option value. */
const TYPE_LABELS: Record<RecordType, string> = {
  login: 'Login',
  api: 'API credential',
  note: 'Secure note',
};

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
  const [projectId, setProjectId] = React.useState(
    seed?.record.projectId ?? '',
  );
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
  const [genErr, setGenErr] = React.useState<string | undefined>();
  const [submitted, setSubmitted] = React.useState(false);
  const [confirmDiscard, setConfirmDiscard] = React.useState(false);

  const nameRef = React.useRef<HTMLInputElement>(null);
  const requirementRef = React.useRef<HTMLInputElement>(null);

  function buildFields(): RecordFields {
    const fields: RecordFields = { name: name.trim() };
    if (username.trim()) fields.username = username.trim();
    if (service.trim()) fields.service = service.trim();
    if (environment.trim()) fields.environment = environment.trim();
    if (projectId.trim()) fields.projectId = projectId.trim();

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

  // Snapshot of the form as opened, so Cancel can tell whether it would throw
  // typed secrets away.
  const snapshot = JSON.stringify({ type, ...draft });
  const initial = React.useRef(snapshot);
  const dirty = snapshot !== initial.current;

  async function save(): Promise<void> {
    if (busy) return;
    // Validate on submit, not by disabling the button: a disabled Save gives
    // no reason, and keyboard users cannot even reach it to find out.
    setSubmitted(true);
    if (!nameOk) {
      nameRef.current?.focus();
      return;
    }
    if (!typeOk) {
      requirementRef.current?.focus();
      return;
    }
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
      setErr(messageOf(e, 'The record could not be saved.'));
    } finally {
      setBusy(false);
    }
  }

  async function generate(): Promise<void> {
    setGenErr(undefined);
    try {
      const { password: generated } = unwrap(await window.albear.generate());
      setPassword(generated);
    } catch (e) {
      setGenErr(
        `${messageOf(e, 'Unable to generate a password.')} Try again, or type one yourself.`,
      );
    }
  }

  function cancel(): void {
    if (dirty) setConfirmDiscard(true);
    else onCancel();
  }

  const hint = requirementHint(type);
  const showNameError = submitted && !nameOk;
  const showTypeError = submitted && !typeOk;
  const requirementDescribedBy =
    hint && !typeOk ? 'record-requirement' : undefined;

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
      <CardContent>
        <form
          noValidate
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <Field
            id="record-type"
            label="Type"
            hint={
              editing
                ? // The daemon reads the stored type on update and ignores the
                  // one sent, so offering to change it here would be a lie.
                  "A record's type cannot be changed after it is created."
                : undefined
            }
          >
            <select
              id="record-type"
              className={cn(
                'flex h-9 w-full rounded-md border border-input bg-background px-3 text-sm',
                'disabled:cursor-not-allowed disabled:opacity-50',
              )}
              value={type}
              disabled={editing}
              aria-describedby={editing ? 'record-type-hint' : undefined}
              onChange={(e) => setType(e.target.value as RecordType)}
            >
              {RECORD_TYPES.map((t) => (
                <option key={t} value={t}>
                  {TYPE_LABELS[t] ?? t}
                </option>
              ))}
            </select>
          </Field>

          <Field
            id="record-name"
            label="Name"
            required
            error={showNameError ? 'Enter a name for this record.' : undefined}
          >
            <Input
              ref={nameRef}
              id="record-name"
              autoFocus
              required
              value={name}
              placeholder="e.g. GitHub"
              aria-invalid={showNameError ? true : undefined}
              aria-describedby={showNameError ? 'record-name-error' : undefined}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>

          {type !== 'note' && (
            <Field id="record-username" label="Username">
              <Input
                id="record-username"
                autoComplete="off"
                spellCheck={false}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </Field>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field id="record-service" label="Service">
              <Input
                id="record-service"
                value={service}
                onChange={(e) => setService(e.target.value)}
              />
            </Field>
            <Field id="record-environment" label="Environment">
              <Input
                id="record-environment"
                value={environment}
                placeholder="e.g. production"
                onChange={(e) => setEnvironment(e.target.value)}
              />
            </Field>
          </div>

          {type === 'login' && (
            <Field id="record-password" label="Password" error={genErr}>
              <div className="flex flex-wrap gap-2">
                <Input
                  ref={requirementRef}
                  id="record-password"
                  type="text"
                  autoComplete="off"
                  spellCheck={false}
                  className="secret font-mono flex-1 min-w-40"
                  value={password}
                  aria-invalid={showTypeError ? true : undefined}
                  aria-describedby={
                    [requirementDescribedBy, genErr && 'record-password-error']
                      .filter(Boolean)
                      .join(' ') || undefined
                  }
                  onChange={(e) => setPassword(e.target.value)}
                />
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => void generate()}
                  title="Replace the password with a new random one"
                >
                  <RefreshCw />
                  Generate
                </Button>
              </div>
            </Field>
          )}

          {type === 'api' && (
            <>
              <Field id="record-api-key" label="API key">
                <Input
                  ref={requirementRef}
                  id="record-api-key"
                  autoComplete="off"
                  spellCheck={false}
                  className="secret font-mono"
                  value={apiKey}
                  aria-invalid={showTypeError ? true : undefined}
                  aria-describedby={requirementDescribedBy}
                  onChange={(e) => setApiKey(e.target.value)}
                />
              </Field>
              <Field id="record-api-secret" label="API secret">
                <Input
                  id="record-api-secret"
                  autoComplete="off"
                  spellCheck={false}
                  className="secret font-mono"
                  value={apiSecret}
                  onChange={(e) => setApiSecret(e.target.value)}
                />
              </Field>
            </>
          )}

          {hint && !typeOk && (
            <p
              id="record-requirement"
              className={cn(
                'text-sm',
                showTypeError ? 'text-destructive' : 'text-muted-foreground',
              )}
            >
              {hint}
            </p>
          )}

          {type !== 'note' && <UrlEditor entries={urls} onChange={setUrls} />}

          {/* Next to the URLs because it is the other matching rule: on a
              localhost page the extension matches this instead of the URL. */}
          <Field
            id="record-project-id"
            label="Project ID (local development)"
            hint={
              <>
                For apps you run on <code translate="no">localhost</code> or{' '}
                <code translate="no">127.0.0.1</code>, where the address alone
                cannot tell projects apart. The browser extension suggests this
                record on a local page that contains{' '}
                <code translate="no" className="secret">
                  albear-id=&quot;{projectId.trim() || 'my-app'}&quot;
                </code>
                . The value must match exactly. Leave it empty for ordinary
                websites.
              </>
            }
          >
            <Input
              id="record-project-id"
              className="font-mono"
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              translate="no"
              value={projectId}
              placeholder="my-app"
              aria-describedby="record-project-id-hint"
              onChange={(e) => setProjectId(e.target.value)}
            />
          </Field>

          <Field id="record-notes" label="Notes">
            <Textarea
              id="record-notes"
              className="secret"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </Field>

          <Field
            id="record-tags"
            label="Tags"
            hint="Separate tags with commas."
          >
            <Input
              id="record-tags"
              value={tags}
              placeholder="work, personal"
              aria-describedby="record-tags-hint"
              onChange={(e) => setTags(e.target.value)}
            />
          </Field>

          <CustomEditor fields={custom} onChange={setCustom} />

          {err && (
            <Alert variant="destructive">
              <AlertTitle>Cannot save</AlertTitle>
              <AlertDescription className="flex flex-col gap-1">
                <span>{err}</span>
                <span>
                  Your changes are still here. Fix the problem, then save again.
                </span>
              </AlertDescription>
            </Alert>
          )}

          {confirmDiscard ? (
            <div
              role="group"
              aria-labelledby="record-discard-label"
              className="flex flex-wrap items-center gap-3 border-t border-border pt-3"
            >
              <span id="record-discard-label" className="flex-1 text-sm">
                Discard your unsaved changes?
              </span>
              <Button
                type="button"
                variant="ghost"
                autoFocus
                onClick={() => setConfirmDiscard(false)}
              >
                Keep editing
              </Button>
              <Button type="button" variant="destructive" onClick={onCancel}>
                Discard changes
              </Button>
            </div>
          ) : (
            <div className="flex flex-wrap gap-2">
              <Button type="submit" disabled={busy}>
                {busy && <Loader2 className="animate-spin" />}
                {editing ? 'Save changes' : 'Create record'}
              </Button>
              <Button type="button" variant="ghost" onClick={() => cancel()}>
                Cancel
              </Button>
            </div>
          )}
        </form>
      </CardContent>
    </Card>
  );
}

/**
 * One labelled field. The label points at the control by id rather than
 * wrapping it, so hint and error text stay out of the control's name and are
 * attached as its description instead (`${id}-hint`, `${id}-error`).
 */
function Field({
  id,
  label,
  required,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  required?: boolean;
  hint?: React.ReactNode;
  error?: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>
        {label}
        {required && (
          <span aria-hidden className="text-destructive">
            {' '}
            *
          </span>
        )}
      </Label>
      {children}
      {hint && (
        <p
          id={`${id}-hint`}
          className="text-xs text-muted-foreground text-pretty"
        >
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-error`} className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
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
    <div
      role="group"
      aria-labelledby="record-urls-label"
      className="flex flex-col gap-2"
    >
      <div className="flex items-center justify-between gap-2">
        <span
          id="record-urls-label"
          className="text-xs uppercase tracking-wide text-muted-foreground"
        >
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
        <p className="text-sm text-muted-foreground">
          No URLs yet. Add the sign-in address so the browser extension can
          offer this record there.
        </p>
      ) : (
        entries.map((entry, i) => (
          // eslint-disable-next-line react/no-array-index-key
          <div key={i} className="flex flex-col gap-1.5">
            <div className="flex gap-2">
              <Input
                type="url"
                value={entry.url}
                placeholder="https://example.com"
                aria-label={`URL ${i + 1}`}
                spellCheck={false}
                onChange={(e) => update(i, { url: e.target.value })}
              />
              <Button
                type="button"
                size="icon"
                variant="ghost"
                onClick={() => onChange(entries.filter((_, idx) => idx !== i))}
                aria-label={`Remove URL ${i + 1}`}
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
              Also match subdomains of this address
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
    <div
      role="group"
      aria-labelledby="record-custom-label"
      className="flex flex-col gap-2"
    >
      <div className="flex items-center justify-between gap-2">
        <span
          id="record-custom-label"
          className="text-xs uppercase tracking-wide text-muted-foreground"
        >
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
            aria-label={`Custom field ${i + 1} name`}
            onChange={(e) => update(i, { key: e.target.value })}
          />
          <Input
            className="secret flex-1 min-w-0"
            value={field.value}
            placeholder="value"
            aria-label={`Custom field ${i + 1} value`}
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => update(i, { value: e.target.value })}
          />
          <Button
            type="button"
            size="icon"
            variant="ghost"
            onClick={() => onChange(fields.filter((_, idx) => idx !== i))}
            aria-label={`Remove custom field ${field.key.trim() || i + 1}`}
          >
            <Trash2 />
          </Button>
        </div>
      ))}
    </div>
  );
}
