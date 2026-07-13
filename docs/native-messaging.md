# albear — Native Messaging & Transport

## Topology

```text
extension SW ══════════ end-to-end Noise session ══════════╗
     │ port.postMessage({frame: base64})                   ║
     ▼                                                     ▼
vault-native (blind relay) ── unix socket frames ──► vaultd
```

`vault-native` validates the exact calling extension ID (native-host manifest
allowlist AND binary-side check). The Chrome unpacked build pins its extension
ID with a manifest `key`, and `vault install chrome` writes the current-user
native-host manifest. `ALBEAR_EXTENSION_IDS` is only a developer override for
alternate local builds. It refuses to relay a hello whose mode is `cli` — same-user
auto-authorization is reserved for direct socket connections verified by
SO_PEERCRED (PRD 12.3).

## Wire format

- Native messaging: 4-byte little-endian length + JSON `{"frame": "<base64>"}`,
  1 MB cap; daemon frames capped at 768 KiB so relay overhead can never hit
  Chrome's limit.
- Socket: 4-byte big-endian length + Noise frame.
- Inside Noise payloads: the JSON envelopes of PRD 24
  (`protocolVersion`, `requestId`, `operation`, `payload` / `ok`, `data`, `error`).
  Protocol version mismatch refuses the request; there is no plaintext mode.

## Hello modes

| mode | pattern | who | granted |
|---|---|---|---|
| `cli` | `Noise_XX` | direct same-UID socket clients | full CLI capability set |
| `pair` | `Noise_XX` | unpaired extension | `clients.pair` / `clients.claim` / `vault.status` only |
| `paired` | `Noise_XXpsk3` | registered clients | the client row's capability mask |

The hello's exact bytes are the Noise prologue — a tampering relay breaks the
handshake (tested Go-side and TS-side).

## Pairing flow (PRD 13.5)

1. Extension → `pair` channel → `clients.pair{kind, label, staticKey}`;
   the submitted static key must equal the handshake key (anti-substitution).
2. Daemon returns `pairingId` + phrase = SHA-256 commitment over daemon static
   key ‖ extension static key ‖ pairing id.
3. `vault clients approve` (interactive terminal) shows the same phrase; the
   user compares. Mismatch = MITM bridge.
4. Approval stores the client (verifier hash, pinned static key, Chrome
   capability mask) — requires the vault unlocked (label is encrypted).
5. Extension polls `clients.claim` → receives clientId + credential + daemon
   static key exactly once; stores them in `chrome.storage.local`.
6. All later connections: `XXpsk3` with PSK = SHA-256(credential), both static
   keys pinned. Revocation kills sessions and future handshakes.
