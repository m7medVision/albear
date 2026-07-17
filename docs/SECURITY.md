# Security model

What albear defends against, what it does not, and why. The honest limits are
the point of this document — a password manager that oversells its boundary is
worse than one that states it.

## The short version

Albear protects your secrets from **anyone who is not you on this machine**: a
stolen disk, a stolen backup, a lost laptop, a snooping process running as a
different user, a malicious web page trying to phish a fill.

It does **not** protect them from **code already running as your user
account**. That is not a bug to be fixed later; it is where the boundary sits.

## The trust boundary: your UID

Every process running as your user can, today, without exploiting anything:

- **Connect to the daemon socket.** It lives under `$XDG_RUNTIME_DIR` with
  owner-only permissions, and the daemon checks peer credentials (`SO_PEERCRED`)
  — but those checks pass for *any* process of yours. A same-UID process that
  speaks the protocol gets a CLI-mode session.
- **`ptrace` the daemon** and read the vault key straight out of its memory
  while the vault is unlocked. `PR_SET_DUMPABLE=0` stops a core dump from
  landing on disk and raises the bar (a non-root attacker cannot attach to a
  non-dumpable process on most configurations), but `ptrace_scope` is a system
  setting, not something albear controls.
- **Read `vault.db`.** The contents are encrypted, so this yields ciphertext —
  but it is the input to an offline guessing attack on your master password.
  That is what Argon2id (128 MiB, 3 iterations) is there to make expensive, and
  why the password policy insists on a passphrase.
- **Modify `vault.db`.** Detected at unlock (see *Rollback protection*), but
  detection is not prevention.
- **Replace the `vault`/`vaultd` binaries, your shell, or your desktop
  session.**

If an attacker is running as you, they do not need to break albear. They can
wait for you to unlock it and ask nicely over the socket. Defending against
that requires an OS-enforced boundary — a separate UID, a hardware token — that
albear does not have and does not claim.

**What follows from this:** the vault protects data at rest and in transit
between local processes. It is not a sandbox against local malware.

## What is defended

| Threat | Defence |
| --- | --- |
| Stolen disk or backup file | Everything at rest is encrypted under a key derived from your master password (Argon2id → root key → per-purpose subkeys). Backups are additionally HMAC'd and bound to the vault ID. |
| Another user on the machine | Owner-only file modes on everything the daemon creates (a process-wide `umask(0o077)`, not a hopeful `chmod` after the fact), plus peer-credential checks on the socket. |
| A malicious web page | Fill and reveal require an exact origin match, verified by the daemon against the stored record — never trusted from the extension. Subdomain matching is off unless a record explicitly opts in. |
| A compromised browser extension | It holds browser-only capabilities: match, fill, create/update **logins**. It cannot list, read arbitrary records, back up, revoke clients, change the password, or destroy the vault. The type scope is enforced daemon-side. |
| The native-messaging relay | It forwards opaque Noise frames. It cannot read or forge traffic — the Noise session runs end to end between the extension and the daemon, through it. |
| A tampered database | An authenticated state root over the whole catalog, checked at unlock. |
| An unattended unlocked vault | Idle auto-lock after 5 minutes of no capability-using request. Status polling does not count as activity. |
| Secrets reaching swap | Best-effort `mlockall(MCL_CURRENT|MCL_FUTURE)` at startup. |

## Known exposures

These are real, and we would rather you knew.

### The clipboard is not private

Copying a secret puts it on the X11/Wayland clipboard, which **every process in
your session can read**, and clipboard managers routinely log history to disk.

Albear clears it 45 seconds after a copy (`CLIPBOARD_CLEAR_MS`,
`desktop/src/main/ipc.ts`), and only if the contents are still what it put
there. That is a mitigation against shoulder-surfing and forgetting, not a
confidentiality guarantee. If a clipboard manager captured it, it is captured.

Prefer autofill over copy where you can.

### Revealed secrets live in the renderer

When the desktop app shows a password, that string is in the renderer process:
in its heap, in the DOM, and in JavaScript strings that no amount of zeroing
can reach — the GC may have copied them already.

The renderer is locked down accordingly: `default-src 'none'` with
`connect-src 'none'` (no fetch, XHR, WebSocket or beacon can leave it),
`contextIsolation`, `sandbox`, no `nodeIntegration`, and a navigation guard.
A compromised renderer has no network to exfiltrate to and no path to Node.
It is still, however, holding your secret in memory.

### Go strings

Secrets that transit a Go `string` (a reveal response, for example) leave
copies the garbage collector may have moved and that `crypto.Zero` cannot
find. The `[]byte` + explicit-zero discipline covers key material; it cannot
cover everything. `mlockall` at least keeps those copies out of swap.

## Rollback protection, and its limit

Each payload is bound by its AEAD to `(vault, record, revision, kind, format,
keyVersion)`, so no ciphertext can be forged, or moved between records, or
replayed at a different revision. But that says nothing about the *set* of
rows. Deleting a record, flipping a client from revoked back to approved, or
restoring an old `key_envelopes` row to resurrect a retired master password all
leave every surviving row perfectly valid.

**Tier 1 (shipped).** The daemon keeps an HMAC-SHA256 root over the whole
catalog — records, clients, and the active key envelope — plus a counter that
only ever increases. The key is a root-key subkey, so it exists only while the
vault is unlocked and cannot be derived from the file alone. Root and counter
are rewritten inside the same transaction as every mutating write, and checked
at unlock. A mismatch panic-locks the vault and records an integrity event.

It **never destroys anything.** A false positive from a bug of ours must not
cost you your vault.

**The limit — read this part.** The anchor is stored *in the database it
protects*. An attacker who replaces `vault.db` wholesale with an earlier,
self-consistent copy of your own vault — old root, old counter, old rows —
produces a file that verifies, because it was genuine once. Within a single
daemon run this is caught: the process remembers the highest counter it has
seen, and a lower one is treated as rollback. **Across a daemon restart, that
memory is gone.**

Defeating cross-restart rollback needs trusted monotonic state *outside* the
file — a TPM NV counter, or an `O_EXCL` sidecar somewhere the attacker cannot
rewrite. That is scoped as follow-up work (Tier 2). It is not implemented, and
this document is where we say so rather than letting the feature name imply
more than it delivers.

**What is caught today:** row deletion, row tampering, revocation reverts,
key-envelope downgrades, and in-run rollback. **What is not:** whole-file
rollback across a restart.

### Legacy vaults bootstrap on trust

A vault created before the state table has no anchor. On its first unlock the
daemon adopts the current state as the baseline and records an informational
event. Tampering that happened *before* that first unlock is what gets adopted:
the key needed to have anchored it earlier did not exist on disk to be used.
Vaults created by this version onwards are anchored from `vault init`, so they
never take this path.

## Origin matching

Fill and reveal require an **exact** match of scheme, punycode host, and
effective port, checked by the daemon against the record's own stored URLs.

A shared registrable domain is not a trust boundary. On an apex that hands out
subdomains — `github.io`, the big cloud app domains, most universities —
"shares an eTLD+1" means "someone else's site", so `evil.example.com` must not
be offered the credential for `accounts.example.com`.

A record URL may opt in to also matching hosts *at or under itself*
(`vault edit <record> --allow-subdomains <url>`). Even then, scheme and port
equality remain mandatory, and the relation is one-way: a record for
`example.com` can cover `www.example.com`, but a record for
`accounts.example.com` never reaches the apex or a sibling. The flag lives in
the encrypted metadata, so the set of sites with relaxed matching is not
readable from the database file.

**Upgrading:** records saved before this existed match exactly. If one of them
relied on the old implicit cross-subdomain behaviour, turn the opt-in on for
that URL.

## Master password

There is **no recovery**. No backdoor, no escrow, no reset. Forgetting it means
the vault is gone, and that is the design.

Argon2id raises the cost of each guess; it does not shrink the guess space. So
the policy asks for at least 12 characters, and either roughly 60 bits of
estimated variety or a length of 16+ — the passphrase path is the easy one, and
the one to take.

Argon2 cost parameters are read back from the database at unlock, and are
bounded on both sides (≤ 1 GiB memory, ≤ 20 iterations, ≤ 16 lanes) so a
tampered envelope cannot turn an unlock attempt into an out-of-memory kill.

## Reporting

Found something? Open a GitHub security advisory rather than a public issue.
