// Popup UI: connection/lock state, matching records for the current tab,
// explicit fill, pairing workflow (PRD 13.2).
import * as React from 'react'
import { KeyRound, Loader2, Lock, Plus, ShieldCheck, ShieldOff, Tag } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Badge, type BadgeProps } from '@/components/ui/badge'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { isLoopbackHost } from '@/lib/origin'
import { describeError, errorCode } from './errors'

interface BgResponse<T> {
  ok: boolean
  data?: T
  error?: { code: string; message: string }
}

async function bg<T>(msg: Record<string, unknown>): Promise<T> {
  const resp = (await chrome.runtime.sendMessage(msg)) as BgResponse<T>
  if (!resp?.ok) throw new Error(resp?.error?.code ?? 'INTERNAL')
  return resp.data as T
}

interface StatusData {
  paired: boolean
  connected?: boolean
  initialized?: boolean
  unlocked?: boolean
}

interface RecordView {
  id: string
  name: string
  username?: string
  service?: string
  environment?: string
  tags?: string[]
  // Set only by records.match: "origin" for a verified real-origin match,
  // "project" for a loopback-only match by page-supplied project tag — a
  // materially weaker trust level the UI should never present identically.
  matchedBy?: 'origin' | 'project'
}

type View = 'loading' | 'pair' | 'locked' | 'unlocked' | 'offline' | 'novault' | 'error'

interface ActionError {
  title: string
  text: string
}

function statusFor(st: StatusData | null, view: View): { text: string; variant: BadgeProps['variant'] } {
  if (view === 'error') return { text: 'Error', variant: 'destructive' }
  if (!st || view === 'loading') return { text: 'Connecting…', variant: 'outline' }
  if (!st.paired) return { text: 'Not paired', variant: 'outline' }
  if (!st.connected) return { text: 'vaultd offline', variant: 'destructive' }
  if (!st.initialized) return { text: 'No vault', variant: 'outline' }
  if (!st.unlocked) return { text: 'Locked', variant: 'secondary' }
  return { text: 'Unlocked', variant: 'success' }
}

// The content script answers a fill with {ok:false, error:String(e)}; a tab
// with no content script rejects the sendMessage outright.
function fillErrorText(raw: string | undefined): string {
  const code = errorCode(raw ?? '')
  if (code === 'no login form on this page') {
    return 'No sign-in form found on this page. Open the page with the sign-in form, then select Fill again.'
  }
  return describeError(code)
}

const UNREACHABLE_PAGE = 'albear can’t reach this page. Reload the page, then try again.'

function matchSummary(n: number): string {
  return n === 1 ? '1 saved login for this site' : `${n} saved logins for this site`
}

function Field({
  id,
  label,
  error,
  children,
}: {
  id: string
  label: string
  error?: string
  children: React.ReactNode
}): React.ReactElement {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-xs font-medium">
        {label}
      </label>
      {children}
      {error && (
        <p id={`${id}-error`} className="text-xs text-destructive text-pretty">
          {error}
        </p>
      )}
    </div>
  )
}

function RecordRow({
  record,
  project,
  disabled,
  onFill,
}: {
  record: RecordView
  project: boolean
  disabled: boolean
  onFill: () => void
}): React.ReactElement {
  const meta: [string, string][] = []
  if (record.service) meta.push(['Service', record.service])
  if (record.environment) meta.push(['Environment', record.environment])
  if (record.tags?.length) meta.push(['Tags', record.tags.join(', ')])
  return (
    <li>
      <Card>
        <CardContent className="flex items-start gap-2 p-2.5">
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
              <p className="text-sm font-medium [overflow-wrap:anywhere]">{record.name}</p>
              {project && (
                <Badge variant="outline">
                  <Tag aria-hidden="true" />
                  Project tag
                </Badge>
              )}
            </div>
            {record.username && (
              <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{record.username}</p>
            )}
            {meta.length > 0 && (
              <dl className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs">
                {meta.map(([term, value]) => (
                  <div key={term} className="flex min-w-0 gap-1">
                    <dt className="text-muted-foreground">{term}</dt>
                    <dd className="[overflow-wrap:anywhere]">{value}</dd>
                  </div>
                ))}
              </dl>
            )}
          </div>
          {/* A project-tag match is a weaker claim than a verified origin, so
              its Fill is the quieter secondary button and points at the note
              explaining why. */}
          <Button
            size="sm"
            variant={project ? 'secondary' : 'default'}
            aria-label={`Fill ${record.name}`}
            aria-describedby={project ? 'project-note' : undefined}
            onClick={onFill}
            disabled={disabled}
          >
            <KeyRound aria-hidden="true" />
            Fill
          </Button>
        </CardContent>
      </Card>
    </li>
  )
}

export function App(): React.ReactElement {
  const [status, setStatus] = React.useState<StatusData | null>(null)
  const [view, setView] = React.useState<View>('loading')
  const [pageErr, setPageErr] = React.useState<string | undefined>()
  const [actionErr, setActionErr] = React.useState<ActionError | undefined>()
  const [records, setRecords] = React.useState<RecordView[]>([])
  const [matchesLoading, setMatchesLoading] = React.useState(false)
  const [origin, setOrigin] = React.useState<string>('')
  const [blocked, setBlocked] = React.useState(false)
  const [phrase, setPhrase] = React.useState<string>('')
  const [pairing, setPairing] = React.useState(false)
  const [pairNote, setPairNote] = React.useState<string | undefined>()
  const [pairErr, setPairErr] = React.useState<string | undefined>()
  const [password, setPassword] = React.useState('')
  const [unlockErr, setUnlockErr] = React.useState<string | undefined>()
  const [unlocking, setUnlocking] = React.useState(false)
  const [newOpen, setNewOpen] = React.useState(false)
  const [newName, setNewName] = React.useState('')
  const [newUser, setNewUser] = React.useState('')
  const [newPass, setNewPass] = React.useState('')
  const [newFieldErr, setNewFieldErr] = React.useState<{ username?: string; password?: string }>({})
  const [newErr, setNewErr] = React.useState<string | undefined>()
  const [saving, setSaving] = React.useState(false)
  const [filling, setFilling] = React.useState(false)

  const passwordRef = React.useRef<HTMLInputElement>(null)
  const newUserRef = React.useRef<HTMLInputElement>(null)
  const newPassRef = React.useRef<HTMLInputElement>(null)

  async function refresh(): Promise<void> {
    try {
      const st = await bg<StatusData>({ kind: 'status' })
      setStatus(st)
      setPageErr(undefined)
      if (!st.paired) {
        setView('pair')
        const stored = await bg<{ pairing: { phrase: string } | null }>({ kind: 'pair.get' })
        if (stored.pairing) {
          setPhrase(stored.pairing.phrase)
          setPairing(true)
        }
        return
      }
      if (!st.connected) {
        setView('offline')
        return
      }
      if (!st.initialized) {
        setView('novault')
        return
      }
      if (!st.unlocked) {
        setView('locked')
        return
      }
      setView('unlocked')
      await loadMatches()
    } catch (e) {
      setPageErr(describeError(e))
      setView('error')
    }
  }

  function retry(): void {
    setView('loading')
    void refresh()
  }

  async function loadMatches(): Promise<void> {
    setMatchesLoading(true)
    try {
      const tabs = await chrome.tabs.query({ active: true, currentWindow: true })
      const tab = tabs[0]
      if (!tab?.url || !/^https?:/.test(tab.url)) {
        setOrigin('')
        setBlocked(false)
        setRecords([])
        return
      }
      const url = new URL(tab.url)
      const loopback = isLoopbackHost(url.hostname)
      setOrigin(url.origin)
      // HTTP is only actually blocked off loopback — matches what the daemon
      // itself enforces (records.revealForOrigin allows loopback over HTTP).
      const httpBlocked = url.protocol === 'http:' && !loopback
      setBlocked(httpBlocked)
      if (httpBlocked) {
        setRecords([])
        return
      }
      const projectId = loopback ? await readTabProjectTag(tab.id) : undefined
      const res = await bg<{ records: RecordView[] }>({ kind: 'match', origin: url.origin, projectId })
      setRecords(res.records)
    } finally {
      setMatchesLoading(false)
    }
  }

  // The project tag only ever matters on loopback, so this is only called
  // there — no point asking every ordinary page's content script for one.
  // Failure (no content script reachable on this tab) just means no tag.
  async function readTabProjectTag(tabId: number | undefined): Promise<string | undefined> {
    if (tabId === undefined) return undefined
    try {
      const res = (await chrome.tabs.sendMessage(tabId, { kind: 'albear.getProjectTag' })) as {
        projectId: string | null
      }
      return res.projectId ?? undefined
    } catch {
      return undefined
    }
  }

  React.useEffect(() => {
    void refresh()
  }, [])

  const sb = statusFor(status, view)

  async function startPair(): Promise<void> {
    setPairErr(undefined)
    try {
      const r = await bg<{ phrase: string }>({ kind: 'pair.start' })
      setPhrase(r.phrase)
      setPairing(true)
    } catch (e) {
      setPairErr(describeError(e))
    }
  }

  async function claimPair(): Promise<void> {
    setPairErr(undefined)
    setPairNote(undefined)
    try {
      const r = await bg<{ done: boolean }>({ kind: 'pair.claim' })
      if (r.done) await refresh()
      else setPairNote('Not approved yet. Approve it in the terminal, then select Finish pairing again.')
    } catch (e) {
      setPairErr(describeError(e))
    }
  }

  async function cancelPair(): Promise<void> {
    setPairErr(undefined)
    setPairNote(undefined)
    try {
      await bg({ kind: 'pair.reset' })
      setPairing(false)
      setPhrase('')
    } catch (e) {
      setPairErr(describeError(e))
    }
  }

  async function unlock(): Promise<void> {
    setUnlockErr(undefined)
    setUnlocking(true)
    try {
      await bg({ kind: 'unlock', password })
      setPassword('')
      await refresh()
    } catch (e) {
      setUnlockErr(describeError(e))
      passwordRef.current?.focus()
    } finally {
      setUnlocking(false)
    }
  }

  async function lock(): Promise<void> {
    setActionErr(undefined)
    try {
      await bg({ kind: 'lock' })
      void refresh()
    } catch (e) {
      setActionErr({ title: 'Unable to lock', text: describeError(e) })
    }
  }

  async function generate(): Promise<void> {
    try {
      const r = await bg<{ password: string }>({ kind: 'generate' })
      setNewPass(r.password)
      setNewFieldErr((f) => ({ ...f, password: undefined }))
    } catch (e) {
      setNewErr(describeError(e))
    }
  }

  function closeNew(): void {
    setNewOpen(false)
    setNewErr(undefined)
    setNewFieldErr({})
  }

  async function saveNew(): Promise<void> {
    const fieldErr: { username?: string; password?: string } = {}
    if (!newUser.trim()) fieldErr.username = 'Enter the username for this site.'
    if (!newPass) fieldErr.password = 'Enter a password, or select Generate.'
    setNewFieldErr(fieldErr)
    if (fieldErr.username) {
      newUserRef.current?.focus()
      return
    }
    if (fieldErr.password) {
      newPassRef.current?.focus()
      return
    }
    setSaving(true)
    setNewErr(undefined)
    try {
      await bg({
        kind: 'records.createForOrigin',
        origin,
        name: newName,
        username: newUser.trim(),
        password: newPass,
      })
      closeNew()
      setNewName('')
      setNewUser('')
      setNewPass('')
      await loadMatches()
    } catch (e) {
      setNewErr(describeError(e))
    } finally {
      setSaving(false)
    }
  }

  async function fill(recordId: string): Promise<void> {
    setActionErr(undefined)
    const tabs = await chrome.tabs.query({ active: true, currentWindow: true })
    const tab = tabs[0]
    if (!tab?.id) {
      setActionErr({ title: 'Unable to fill', text: UNREACHABLE_PAGE })
      return
    }
    setFilling(true)
    try {
      const res = (await chrome.tabs.sendMessage(tab.id, { kind: 'albear.fill', recordId })) as
        | { ok: boolean; error?: string }
        | undefined
      if (!res?.ok) {
        setActionErr({ title: 'Unable to fill', text: fillErrorText(res?.error) })
        return
      }
      window.close()
    } catch {
      setActionErr({ title: 'Unable to fill', text: UNREACHABLE_PAGE })
    } finally {
      setFilling(false)
    }
  }

  const verified = records.filter((r) => r.matchedBy !== 'project')
  const projectMatches = records.filter((r) => r.matchedBy === 'project')
  const showMatches = view === 'unlocked' && !matchesLoading && origin !== '' && !blocked

  return (
    <div className="w-[340px] min-h-[200px] flex flex-col">
      <header className="flex items-center gap-2 px-3 py-2.5 border-b border-border">
        {/* Same mark and wordmark as the desktop app. The icon is drawn for a
            light field, so the white tile is fixed in both themes. */}
        <img
          src="/icons/32.png"
          alt=""
          className="size-6 rounded-md shrink-0 bg-white p-0.5 ring-1 ring-border"
        />
        <h1 className="flex-1 font-arabic text-lg font-bold leading-none" lang="ar">
          البير
        </h1>
        {/* Rendered from the first paint so each status change is announced. */}
        <span role="status">
          <Badge variant={sb.variant}>{sb.text}</Badge>
        </span>
      </header>

      <main className="p-3 flex flex-col gap-3">
        {view === 'loading' && (
          <p className="flex items-center justify-center gap-2 py-8 text-xs text-muted-foreground">
            <Loader2 className="size-4 motion-safe:animate-spin" aria-hidden="true" />
            Connecting to vaultd…
          </p>
        )}

        {view === 'error' && (
          <Card>
            <CardHeader>
              <CardTitle>Unable to load albear</CardTitle>
              <CardDescription>{pageErr}</CardDescription>
            </CardHeader>
            <CardContent>
              <Button className="w-full" onClick={retry}>
                Try again
              </Button>
            </CardContent>
          </Card>
        )}

        {view === 'offline' && (
          <Card>
            <CardHeader>
              <CardTitle>Unable to reach vaultd</CardTitle>
              <CardDescription>
                albear keeps your logins in vaultd on this computer. Start vaultd, then try again.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Button className="w-full" onClick={retry}>
                Try again
              </Button>
            </CardContent>
          </Card>
        )}

        {view === 'novault' && (
          <Card>
            <CardHeader>
              <CardTitle>No vault yet</CardTitle>
              <CardDescription>
                Create one by running <code className="font-mono text-foreground">vault init</code> in a
                terminal, then try again.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Button className="w-full" onClick={retry}>
                Try again
              </Button>
            </CardContent>
          </Card>
        )}

        {view === 'pair' && (
          <Card>
            <CardHeader>
              <CardTitle>Pair this browser</CardTitle>
              <CardDescription>
                albear needs to pair with vaultd on this computer before it can fill logins.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-2">
              {!pairing ? (
                <Button onClick={() => void startPair()}>Start pairing</Button>
              ) : (
                <>
                  <p className="text-xs text-muted-foreground text-pretty">
                    In a terminal, run{' '}
                    <code className="font-mono text-foreground">vault clients approve</code> and check it
                    shows this phrase:
                  </p>
                  <p className="font-mono text-[15px] tracking-widest text-center bg-muted rounded-md py-2 [overflow-wrap:anywhere]">
                    {phrase}
                  </p>
                  <div className="flex gap-2">
                    <Button className="flex-1" onClick={() => void claimPair()}>
                      Finish pairing
                    </Button>
                    <Button variant="secondary" className="flex-1" onClick={() => void cancelPair()}>
                      Cancel
                    </Button>
                  </div>
                </>
              )}
              <div role="status">
                {pairNote && <p className="text-xs text-muted-foreground text-pretty">{pairNote}</p>}
              </div>
              {pairErr && (
                <Alert variant="destructive">
                  <AlertTitle>Unable to pair</AlertTitle>
                  <AlertDescription>{pairErr}</AlertDescription>
                </Alert>
              )}
            </CardContent>
          </Card>
        )}

        {view === 'locked' && (
          <Card>
            <form
              noValidate
              onSubmit={(e) => {
                e.preventDefault()
                void unlock()
              }}
            >
              <CardHeader>
                <CardTitle>Unlock albear</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <Field id="master-password" label="Master password" error={unlockErr}>
                  <Input
                    id="master-password"
                    ref={passwordRef}
                    type="password"
                    autoFocus
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    aria-invalid={unlockErr ? true : undefined}
                    aria-describedby={unlockErr ? 'master-password-error' : undefined}
                  />
                </Field>
                <Button type="submit" disabled={unlocking}>
                  {unlocking && <Loader2 className="motion-safe:animate-spin" aria-hidden="true" />}
                  Unlock
                </Button>
              </CardContent>
            </form>
          </Card>
        )}

        {view === 'unlocked' && (
          <>
            <div className="flex items-start gap-2">
              <div className="min-w-0 flex-1">
                {origin ? (
                  <>
                    <p className="text-xs text-muted-foreground">Logins for</p>
                    <p className="font-mono text-xs break-all">{origin}</p>
                  </>
                ) : (
                  <p className="text-xs text-muted-foreground">No website in this tab</p>
                )}
              </div>
              <Button variant="secondary" onClick={() => void lock()}>
                <Lock aria-hidden="true" />
                Lock
              </Button>
            </div>

            {blocked && (
              <Alert variant="destructive" role="status">
                <ShieldOff aria-hidden="true" />
                <AlertTitle>Insecure page</AlertTitle>
                <AlertDescription>
                  This page doesn’t use HTTPS, so albear won’t fill or save logins here. Open the site’s
                  HTTPS address to use them.
                </AlertDescription>
              </Alert>
            )}

            {actionErr && (
              <Alert variant="destructive">
                <AlertTitle>{actionErr.title}</AlertTitle>
                <AlertDescription>{actionErr.text}</AlertDescription>
              </Alert>
            )}

            <p role="status" className="sr-only">
              {showMatches ? matchSummary(records.length) : ''}
            </p>

            {matchesLoading && (
              <p className="flex items-center justify-center gap-2 py-4 text-xs text-muted-foreground">
                <Loader2 className="size-4 motion-safe:animate-spin" aria-hidden="true" />
                Loading logins…
              </p>
            )}

            {!matchesLoading && !origin && (
              <Card>
                <CardContent className="py-4 text-center text-xs text-muted-foreground text-pretty">
                  Open a website in this tab to see its saved logins.
                </CardContent>
              </Card>
            )}

            {showMatches && records.length === 0 && (
              <Card>
                <CardContent className="flex flex-col gap-1 py-4 text-center">
                  <p className="text-sm font-medium">No saved logins for this site</p>
                  <p className="text-xs text-muted-foreground text-pretty">
                    Sign in on the page and albear offers to save it, or add one with New login.
                  </p>
                </CardContent>
              </Card>
            )}

            {showMatches && verified.length > 0 && (
              <section aria-labelledby={projectMatches.length > 0 ? 'verified-heading' : undefined} className="flex flex-col gap-2">
                {projectMatches.length > 0 && (
                  <h2 id="verified-heading" className="flex items-center gap-1.5 text-xs font-semibold">
                    <ShieldCheck className="size-3.5 shrink-0" aria-hidden="true" />
                    Saved for this site
                  </h2>
                )}
                <ul className="flex flex-col gap-2">
                  {verified.map((r) => (
                    <RecordRow
                      key={r.id}
                      record={r}
                      project={false}
                      disabled={blocked || filling}
                      onFill={() => void fill(r.id)}
                    />
                  ))}
                </ul>
              </section>
            )}

            {showMatches && projectMatches.length > 0 && (
              <section aria-labelledby="project-heading" className="flex flex-col gap-2">
                <div className="flex flex-col gap-0.5">
                  <h2 id="project-heading" className="flex items-center gap-1.5 text-xs font-semibold">
                    <Tag className="size-3.5 shrink-0" aria-hidden="true" />
                    Matched by project tag
                  </h2>
                  <p id="project-note" className="text-xs text-muted-foreground text-pretty">
                    This page’s albear-id tag matches these logins, but its address isn’t verified. Fill only
                    on dev servers you started.
                  </p>
                </div>
                <ul className="flex flex-col gap-2">
                  {projectMatches.map((r) => (
                    <RecordRow
                      key={r.id}
                      record={r}
                      project
                      disabled={blocked || filling}
                      onFill={() => void fill(r.id)}
                    />
                  ))}
                </ul>
              </section>
            )}

            <Button
              variant="secondary"
              aria-expanded={newOpen}
              aria-controls="new-login"
              onClick={() => (newOpen ? closeNew() : setNewOpen(true))}
            >
              <Plus aria-hidden="true" />
              New login
            </Button>

            {newOpen && (
              <Card id="new-login">
                <form
                  noValidate
                  onSubmit={(e) => {
                    e.preventDefault()
                    void saveNew()
                  }}
                >
                  <CardHeader>
                    <CardTitle>New login</CardTitle>
                    <CardDescription>
                      {origin ? (
                        <>
                          Saves a login for <span className="font-mono break-all">{origin}</span> without
                          signing in.
                        </>
                      ) : (
                        'Open a website in this tab first. New logins are saved for that site.'
                      )}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="flex flex-col gap-3">
                    <Field id="new-name" label="Name (optional)">
                      <Input id="new-name" value={newName} onChange={(e) => setNewName(e.target.value)} />
                    </Field>
                    <Field id="new-username" label="Username" error={newFieldErr.username}>
                      <Input
                        id="new-username"
                        ref={newUserRef}
                        autoFocus
                        autoComplete="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        value={newUser}
                        onChange={(e) => setNewUser(e.target.value)}
                        aria-invalid={newFieldErr.username ? true : undefined}
                        aria-describedby={newFieldErr.username ? 'new-username-error' : undefined}
                      />
                    </Field>
                    <Field id="new-password" label="Password" error={newFieldErr.password}>
                      <div className="flex gap-2">
                        <Input
                          id="new-password"
                          ref={newPassRef}
                          type="text"
                          autoComplete="off"
                          autoCapitalize="none"
                          spellCheck={false}
                          value={newPass}
                          onChange={(e) => setNewPass(e.target.value)}
                          className="secret flex-1"
                          aria-invalid={newFieldErr.password ? true : undefined}
                          aria-describedby={newFieldErr.password ? 'new-password-error' : undefined}
                        />
                        <Button type="button" variant="secondary" onClick={() => void generate()} disabled={!origin}>
                          Generate
                        </Button>
                      </div>
                    </Field>
                    {newErr && (
                      <Alert variant="destructive">
                        <AlertTitle>Unable to save</AlertTitle>
                        <AlertDescription>{newErr}</AlertDescription>
                      </Alert>
                    )}
                    <div className="flex gap-2">
                      <Button type="submit" disabled={saving || blocked || !origin} className="flex-1">
                        {saving && <Loader2 className="motion-safe:animate-spin" aria-hidden="true" />}
                        Save login
                      </Button>
                      <Button type="button" variant="secondary" onClick={closeNew} className="flex-1">
                        Cancel
                      </Button>
                    </div>
                  </CardContent>
                </form>
              </Card>
            )}
          </>
        )}
      </main>
    </div>
  )
}
