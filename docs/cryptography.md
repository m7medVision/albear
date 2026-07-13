# albear — Cryptographic Design

Documented per PRD 4.7: the design is auditable; only the key values are
secret. No custom algorithms; every construction is a published standard.

## Algorithms

| Purpose | Algorithm | Implementation |
|---|---|---|
| Password KDF | Argon2id (RFC 9106) | `golang.org/x/crypto/argon2` |
| Record encryption | XChaCha20-Poly1305 | `golang.org/x/crypto/chacha20poly1305` |
| Key separation | HKDF-SHA-256 | `golang.org/x/crypto/hkdf` |
| Transport encryption | Noise `XXpsk3`/`XX` `_25519_ChaChaPoly_SHA256` | Go: `flynn/noise`; TS: spec implementation over `@noble/*` |
| Randomness | OS CSPRNG | `crypto/rand`, `crypto.getRandomValues` |
| MACs / verifiers | HMAC-SHA-256 / SHA-256 | `crypto/hmac`, `crypto/sha256` |

## Key hierarchy

```text
Master password ──Argon2id(salt)──► KEK (32B, never stored)
KEK ──XChaCha20-Poly1305──► unwraps Root Vault Key (32B, stored wrapped only)
Root Vault Key ──HKDF──► metadata key │ secrets key │ audit key │ backup key
```

- The root key is random; a master-password change re-wraps only the root key
  (new salt, new envelope version, old envelope deleted atomically). Records
  are never re-encrypted (PRD 15.6).
- An encrypted canary (`albear-canary-v1`) verifies unwrap correctness; wrong
  password and corrupted envelope are indistinguishable to clients.

## Argon2id parameters

Default: 128 MiB, 3 iterations, 4 lanes. Hard minimums (enforced in code AND
by schema CHECK constraints): 64 MiB, 3 iterations. Parameters are stored in
the key envelope so future versions can raise costs.

## AEAD associated data

Every ciphertext binds its identity (`internal/infrastructure/crypto/aad.go`):

```text
vault_id ‖ record_id ‖ revision(u64) ‖ payload_kind(u8) ‖ format_version(u32) ‖ key_version(u32)
```

Payload kinds: metadata=1, secret=2, canary=3, client-label=4, event=5,
backup=6. Cross-record substitution, metadata/secret swaps, and revision
rollbacks all fail authentication (tested in `crypto_test.go`,
`records/application/service_test.go`).

## Nonces

Records: fresh 24-byte random nonce from `crypto/rand` per encryption, never
derived, never reused across updates, never accepted from clients.
Transport: Noise counter nonces within a session, with deterministic rekey
every 4096 messages per direction; sessions never persist.

## Transport encryption (PRD 12.4)

- Paired clients: `Noise_XXpsk3_25519_ChaChaPoly_SHA256`,
  PSK = SHA-256(client credential) — which is exactly the verifier stored in
  the database. The daemon never stores the raw credential; a database thief
  gains a transport PSK but no vault key material.
- Unpaired (pairing) channel and same-user CLI: `Noise_XX`.
- The plaintext hello frame is the Noise prologue: any relay tampering breaks
  the handshake.
- Static keys: daemon X25519 key at `$XDG_CONFIG_HOME/albear/daemon.key`
  (0600, transport identity only); client keys pinned at approval, checked on
  every handshake.
- Cross-language interop is pinned by generated vectors
  (`tools/noisevectors` → `extension/src/noise/testdata/vectors.json`).

## Memory handling limits

Go is garbage-collected: zeroization is best-effort (`crypto.Zero`,
buffer-wipe on lock, `PR_SET_DUMPABLE=0`, RLIMIT_CORE=0). Root or same-user
debugger access while unlocked can read process memory. Documented, not
claimed otherwise (PRD 16.8, 18.4).
