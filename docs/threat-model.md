# albear — Threat Model

## Assets

Record secrets (passwords, API keys, notes), record metadata, the root vault
key, the master password, client credentials.

## Trust boundaries

```text
Web pages ─╳─ content script ── service worker ══ Noise E2E ══╗
Chrome ────── native messaging ── vault-native (blind relay) ─╫─ vaultd ── SQLite
CLI ═══════════ Noise over unix socket ═══════════════════════╝
```

`vaultd` is the only component that opens the database or holds keys.

## What albear defends against

| Threat | Defense |
|---|---|
| Theft of `vault.db` / WAL / backups | Everything sensitive is XChaCha20-Poly1305 ciphertext; offline guessing gated by Argon2id |
| Ciphertext tampering / substitution | AEAD with identity-binding AAD; any failure ⇒ integrity error, fail closed |
| Malicious website | Content script never trusts page data; origin comes from the browser; exact-origin matching with IDN normalization and eTLD+1 policy rejects lookalikes |
| Rogue Chrome extension | Native host allowlists exact extension IDs; daemon capabilities restrict the extension (no export/restore/destroy/list/reveal) |
| Compromised or curious relay (`vault-native`) | End-to-end Noise: relay sees ciphertext; prologue binding + AEAD detect modification; pairing phrase commits to both static keys, defeating pairing-time MITM |
| Unauthorized local IPC client | SO_PEERCRED same-UID check, then Noise handshake bound to the client credential PSK and pinned static key |
| Stolen client credential | Grants a transport session only; the vault stays locked without the master password; revocable via CLI |
| Interactive brute force | Escalating unlock delays (PRD 19.3) |
| Anti-tamper abuse | Suspicious activity locks; nothing ever auto-deletes the vault (PRD 1.1) |

## What albear does NOT defend against (documented)

- Same-user malware while the vault is unlocked: can read daemon memory,
  credential files, and `chrome.storage.local`. Transport encryption removes
  passive observation, not active theft (PRD 18.4).
- Root/kernel compromise.
- A weak master password against offline guessing.
- Full-database rollback to an older authenticated snapshot (needs external
  monotonic state; out of MVP scope, PRD 30).
- Physical erasure guarantees on SSDs (`vault destroy` warns about this).

## Response levels (PRD 19)

1. Reject — invalid request/capability/revision: generic error, rate-limited event.
2. Disconnect — bad credential/handshake/AEAD/framing/oversize: drop the connection.
3. Panic lock — record or envelope integrity failure, impossible state, explicit request: wipe keys, kill sessions, keep data.
4. Destroy — only `vault destroy`: interactive terminal + master password + typed phrase.
