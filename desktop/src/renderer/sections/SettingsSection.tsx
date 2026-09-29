// Vault administration: master-password change.
//
// Vault creation lives in CreateVaultCard (the uninitialized phase) and panic
// lock lives in the shell header, since both need to be reachable when this
// section is not.
import * as React from 'react';
import { KeyRound, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { useVault } from '@/VaultContext';
import { unwrap, isCode, messageOf, AUTH_FAILED } from '@/lib/api';

/**
 * The same policy CreateVaultCard states, for the same reason: the daemon
 * answers every strength failure with one generic error on purpose, so the
 * rules have to be visible before submitting. Guidance only — the policy is not
 * re-implemented here, and the daemon remains the only judge.
 */
const POLICY = [
  'At least 12 characters.',
  'Either 16+ characters, or a mix of cases, digits and symbols.',
  'Not a common password, and not a repeated or sequential run.',
];

type Field = 'current' | 'next' | 'confirm';
type FieldErrors = Partial<Record<Field, string>>;

export function SettingsSection(): React.ReactElement {
  const { refresh, recordCount } = useVault();
  const [current, setCurrent] = React.useState('');
  const [next, setNext] = React.useState('');
  const [confirm, setConfirm] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [fieldErrs, setFieldErrs] = React.useState<FieldErrors>({});
  const [err, setErr] = React.useState<string | undefined>();
  const [ok, setOk] = React.useState(false);
  const refs = {
    current: React.useRef<HTMLInputElement>(null),
    next: React.useRef<HTMLInputElement>(null),
    confirm: React.useRef<HTMLInputElement>(null),
  };

  function fail(errors: FieldErrors): void {
    setFieldErrs(errors);
    const first = (['current', 'next', 'confirm'] as const).find(
      (f) => errors[f],
    );
    if (first) refs[first].current?.focus();
  }

  // Validate on submit, not by disabling the button: a disabled button says
  // nothing about what is missing.
  async function change(): Promise<void> {
    if (busy) return;
    setErr(undefined);
    setOk(false);
    const errors: FieldErrors = {};
    if (current.length === 0)
      errors.current = 'Enter your current master password.';
    if (next.length === 0) errors.next = 'Enter a new master password.';
    else if (next !== confirm)
      errors.confirm = 'Enter the same new password in both fields.';
    if (Object.keys(errors).length > 0) {
      fail(errors);
      return;
    }
    setFieldErrs({});
    setBusy(true);
    try {
      unwrap(await window.albear.changePassword(current, next));
      setCurrent('');
      setNext('');
      setConfirm('');
      setOk(true);
      void refresh();
    } catch (e) {
      if (isCode(e, AUTH_FAILED)) {
        fail({
          current:
            'That is not your current master password. Check it and try again.',
        });
      } else {
        setErr(
          messageOf(
            e,
            'The Albear service did not answer. Your password was not changed; try again.',
          ),
        );
      }
    } finally {
      setBusy(false);
    }
  }

  function field(
    id: Field,
    label: string,
    value: string,
    set: (v: string) => void,
    autoComplete: string,
  ): React.ReactElement {
    const error = fieldErrs[id];
    return (
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`pw-${id}`}>{label}</Label>
        <Input
          ref={refs[id]}
          id={`pw-${id}`}
          type="password"
          autoComplete={autoComplete}
          value={value}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? `pw-${id}-error` : undefined}
          onChange={(e) => {
            set(e.target.value);
            if (error) setFieldErrs((f) => ({ ...f, [id]: undefined }));
          }}
        />
        {error && (
          <p id={`pw-${id}-error`} className="text-sm text-destructive">
            {error}
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>Change master password</CardTitle>
          <CardDescription>
            Re-wraps the vault key. Your records are not re-encrypted, and every
            other client stays paired.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            noValidate
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              void change();
            }}
          >
            <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
              {POLICY.map((rule) => (
                <li key={rule}>{rule}</li>
              ))}
            </ul>
            {field(
              'current',
              'Current master password',
              current,
              setCurrent,
              'current-password',
            )}
            {field(
              'next',
              'New master password',
              next,
              setNext,
              'new-password',
            )}
            {field(
              'confirm',
              'Repeat new master password',
              confirm,
              setConfirm,
              'new-password',
            )}
            <Button type="submit" className="self-start" disabled={busy}>
              {busy ? <Loader2 className="animate-spin" /> : <KeyRound />}
              Change password
            </Button>
            {ok && (
              <Alert>
                <AlertDescription>
                  Master password changed. Use the new one from now on.
                </AlertDescription>
              </Alert>
            )}
            {err && (
              <Alert variant="destructive">
                <AlertTitle>Unable to change the password</AlertTitle>
                <AlertDescription>{err}</AlertDescription>
              </Alert>
            )}
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>This vault</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          <p>
            {recordCount === undefined
              ? 'Record count unavailable.'
              : `${recordCount} record${recordCount === 1 ? '' : 's'} stored.`}
          </p>
          {/* Destroying a vault is deliberately absent. It is irreversible, and
              the CLI already gates it behind an interactive password prompt —
              a button here would add risk without adding capability. */}
          <p className="mt-2">
            To destroy this vault permanently, use <code>vault destroy</code> in
            a terminal.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
