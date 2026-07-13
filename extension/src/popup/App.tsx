// Popup UI: connection/lock state, matching records for the current tab,
// explicit fill, pairing workflow (PRD 13.2).
import * as React from 'react'
import { KeyRound, Search, ShieldOff, Eye, Loader2, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardAction } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'

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
}

type View = 'pair' | 'locked' | 'unlocked' | 'none'

function statusFor(st: StatusData | null, err?: string): { text: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' } {
  if (err) return { text: err, variant: 'destructive' }
  if (!st) return { text: 'connecting…', variant: 'outline' }
  if (!st.paired) return { text: 'not paired', variant: 'outline' }
  if (!st.connected) return { text: 'native host down', variant: 'destructive' }
  if (!st.initialized) return { text: 'no vault', variant: 'outline' }
  if (!st.unlocked) return { text: 'locked', variant: 'secondary' }
  return { text: 'unlocked', variant: 'default' }
}

export function App(): React.ReactElement {
  const [status, setStatus] = React.useState<StatusData | null>(null)
  const [view, setView] = React.useState<View>('none')
  const [errText, setErrText] = React.useState<string | undefined>()
  const [records, setRecords] = React.useState<RecordView[]>([])
  const [origin, setOrigin] = React.useState<string>('')
  const [insecure, setInsecure] = React.useState(false)
  const [phrase, setPhrase] = React.useState<string>('')
  const [pairing, setPairing] = React.useState(false)
  const [password, setPassword] = React.useState('')
  const [unlockErr, setUnlockErr] = React.useState<string | undefined>()
  const [unlocking, setUnlocking] = React.useState(false)
  const [newOpen, setNewOpen] = React.useState(false)
  const [newName, setNewName] = React.useState('')
  const [newUser, setNewUser] = React.useState('')
  const [newPass, setNewPass] = React.useState('')
  const [newErr, setNewErr] = React.useState<string | undefined>()
  const [saving, setSaving] = React.useState(false)

  async function refresh(): Promise<void> {
    try {
      const st = await bg<StatusData>({ kind: 'status' })
      setStatus(st)
      setErrText(undefined)
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
        setView('none')
        return
      }
      if (!st.initialized) {
        setView('none')
        return
      }
      if (!st.unlocked) {
        setView('locked')
        return
      }
      setView('unlocked')
      await loadMatches()
    } catch (e) {
      setErrText(`error: ${e instanceof Error ? e.message : String(e)}`)
      setView('none')
    }
  }

  async function loadMatches(): Promise<void> {
    const tabs = await chrome.tabs.query({ active: true, currentWindow: true })
    const tab = tabs[0]
    if (!tab?.url || !/^https?:/.test(tab.url)) {
      setOrigin('')
      setInsecure(false)
      setRecords([])
      return
    }
    const o = new URL(tab.url).origin
    setOrigin(o)
    setInsecure(o.startsWith('http://'))
    if (o.startsWith('http://')) {
      setRecords([])
      return
    }
    const res = await bg<{ records: RecordView[] }>({ kind: 'match', origin: o })
    setRecords(res.records)
  }

  React.useEffect(() => {
    void refresh()
  }, [])

  const sb = statusFor(status, errText)

  async function startPair(): Promise<void> {
    try {
      const r = await bg<{ phrase: string }>({ kind: 'pair.start' })
      setPhrase(r.phrase)
      setPairing(true)
    } catch (e) {
      setErrText(`error: ${e instanceof Error ? e.message : String(e)}`)
    }
  }

  async function claimPair(): Promise<void> {
    try {
      const r = await bg<{ done: boolean }>({ kind: 'pair.claim' })
      if (r.done) await refresh()
      else setErrText('not approved yet — run `vault clients approve`')
    } catch (e) {
      setErrText(`error: ${e instanceof Error ? e.message : String(e)}`)
    }
  }

  async function cancelPair(): Promise<void> {
    try {
      await bg({ kind: 'pair.reset' })
      setPairing(false)
      setPhrase('')
    } catch (e) {
      setErrText(`error: ${e instanceof Error ? e.message : String(e)}`)
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
      setUnlockErr(
        e instanceof Error && e.message === 'RATE_LIMITED'
          ? 'too many attempts — wait a moment'
          : 'wrong password',
      )
    } finally {
      setUnlocking(false)
    }
  }

  async function lock(): Promise<void> {
    await bg({ kind: 'lock' })
    void refresh()
  }

  async function generate(): Promise<void> {
    try {
      const r = await bg<{ password: string }>({ kind: 'generate' })
      setNewPass(r.password)
    } catch (e) {
      setNewErr(e instanceof Error ? e.message : 'failed to generate')
    }
  }

  async function saveNew(): Promise<void> {
    if (!newUser || !newPass) {
      setNewErr('username and password are required')
      return
    }
    setSaving(true)
    setNewErr(undefined)
    try {
      await bg({
        kind: 'records.createForOrigin',
        origin,
        name: newName,
        username: newUser,
        password: newPass,
      })
      setNewOpen(false)
      setNewName('')
      setNewUser('')
      setNewPass('')
      await loadMatches()
    } catch (e) {
      setNewErr(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function fill(recordId: string): Promise<void> {
    const tabs = await chrome.tabs.query({ active: true, currentWindow: true })
    const tab = tabs[0]
    if (!tab?.id) return
    await chrome.tabs.sendMessage(tab.id, { kind: 'albear.fill', recordId })
    window.close()
  }

  return (
    <div className="w-[340px] min-h-[200px] flex flex-col">
      <header className="flex items-center gap-2 px-3 py-2.5 border-b border-border">
        <KeyRound className="size-4 text-muted-foreground" />
        <h1 className="text-sm font-semibold flex-1">
          albear <span className="text-muted-foreground font-normal">البير</span>
        </h1>
        <Badge variant={sb.variant}>{sb.text}</Badge>
      </header>

      <main className="p-3 flex flex-col gap-3">
        {view === 'pair' && (
          <Card>
            <CardHeader>
              <CardTitle>Pair with vaultd</CardTitle>
              <CardDescription>Not paired with the local vault yet.</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-2">
              {!pairing ? (
                <Button onClick={() => void startPair()}>Start pairing</Button>
              ) : (
                <>
                  <p className="text-xs text-muted-foreground">
                    Approve in your terminal with{' '}
                    <code className="font-mono text-foreground">vault clients approve</code> and confirm
                    the phrase matches:
                  </p>
                  <div className="font-mono text-[15px] tracking-widest text-center bg-muted rounded-md py-2">
                    {phrase}
                  </div>
                  <div className="flex gap-2">
                    <Button className="flex-1" onClick={() => void claimPair()}>
                      I approved it
                    </Button>
                    <Button variant="secondary" className="flex-1" onClick={() => void cancelPair()}>
                      Cancel
                    </Button>
                  </div>
                </>
              )}
            </CardContent>
          </Card>
        )}

        {view === 'locked' && (
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
                  if (e.key === 'Enter') void unlock()
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

        {view === 'unlocked' && (
          <>
            <div className="flex items-center gap-2">
              <div className="relative flex-1">
                <Search className="size-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
                <Input
                  readOnly
                  className="pl-7 font-mono text-xs"
                  value={origin}
                  placeholder="no site in this tab"
                />
              </div>
              <Button variant="secondary" onClick={() => void lock()}>
                Lock
              </Button>
            </div>

            {insecure && (
              <Alert variant="destructive">
                <ShieldOff />
                <AlertTitle>Insecure origin</AlertTitle>
                <AlertDescription>This page is not HTTPS. Filling is disabled.</AlertDescription>
              </Alert>
            )}

            <div className="flex flex-col gap-2">
              {records.length === 0 ? (
                <Card>
                  <CardContent className="text-xs text-muted-foreground text-center py-4">
                    no matching logins
                  </CardContent>
                </Card>
              ) : (
                records.map((r) => (
                  <Card key={r.id}>
                    <CardContent className="flex items-center gap-2 p-2.5">
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium truncate">{r.name}</div>
                        {r.username && (
                          <div className="text-xs text-muted-foreground truncate">{r.username}</div>
                        )}
                      </div>
                      <Button
                        size="sm"
                        onClick={() => void fill(r.id)}
                        disabled={insecure}
                      >
                        <Eye />
                        Fill
                      </Button>
                    </CardContent>
                  </Card>
                ))
              )}
            </div>

            <Button
              variant="secondary"
              onClick={() => {
                setNewErr(undefined)
                setNewOpen((v) => !v)
              }}
            >
              <Plus />
              New login
            </Button>

            {newOpen && (
              <Card>
                <CardHeader>
                  <CardTitle>New login</CardTitle>
                  <CardDescription>Save a credential without filling a form.</CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-2">
                  <Input
                    readOnly
                    value={origin}
                    placeholder="no site in this tab — navigate to one first"
                    className="font-mono text-xs"
                  />
                  <Input
                    placeholder="Name (optional)"
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                  />
                  <Input
                    placeholder="Username"
                    value={newUser}
                    onChange={(e) => setNewUser(e.target.value)}
                    autoFocus
                  />
                  <div className="flex gap-2">
                    <Input
                      type="text"
                      placeholder="Password"
                      value={newPass}
                      onChange={(e) => setNewPass(e.target.value)}
                      className="flex-1 font-mono"
                    />
                    <Button
                      variant="secondary"
                      onClick={() => void generate()}
                      disabled={!origin}
                    >
                      Generate
                    </Button>
                  </div>
                  {newErr && (
                    <Alert variant="destructive">
                      <AlertDescription>{newErr}</AlertDescription>
                    </Alert>
                  )}
                  <div className="flex gap-2">
                    <Button
                      onClick={() => void saveNew()}
                      disabled={saving || insecure || !origin}
                      className="flex-1"
                    >
                      {saving && <Loader2 className="animate-spin" />}
                      Save
                    </Button>
                    <Button
                      variant="secondary"
                      onClick={() => {
                        setNewOpen(false)
                        setNewErr(undefined)
                      }}
                      className="flex-1"
                    >
                      Cancel
                    </Button>
                  </div>
                </CardContent>
              </Card>
            )}
          </>
        )}

        {view === 'none' && (
          <p className="text-xs text-muted-foreground text-center py-2">
            {status && !status.connected
              ? 'native host unavailable — is vaultd running?'
              : status && !status.initialized
                ? 'no vault yet — run `vault init` in a terminal'
                : ''}
          </p>
        )}
      </main>
    </div>
  )
}
