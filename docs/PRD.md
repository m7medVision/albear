# Product Requirements Document

## albear (البير) — Go Daemon, CLI, and Chrome Extension

**Document version:** 0.2
**Status:** Product baseline with end-to-end transport encryption
**Platform:** Linux-first, with later macOS and Windows support
**Architecture:** Local-only, Domain-Driven Design
**Primary implementation language:** Go
**Clients:** CLI and Google Chrome extension

albear (البير, "the well") is a local-only encrypted secrets manager: a deep, dark well that holds your secrets on your own machine.

**Changes in 0.2:** all client↔daemon communication is now encrypted end-to-end with the Noise Protocol Framework, including the path from the Chrome extension through the native bridge to the daemon. The bridge becomes a blind relay. See sections 12.4, 13.5, 16, 18.3, 24, 27.

---

# 1. Important security decisions

Two requested behaviors need to be changed to avoid making the product less secure.

## 1.1 No automatic destruction of the vault

The product must **not delete the persistent vault automatically** when it detects debugging, malformed requests, or unauthorized access.

Automatic deletion would allow an attacker to destroy the user’s vault simply by triggering the detection mechanism. Debugging detection can also produce false positives.

Suspicious activity will instead trigger:

1. Immediate vault locking.
2. Invalidation of active sessions.
3. Best-effort removal of decrypted keys and metadata from memory.
4. Closure of client connections.
5. A local security event.
6. Optional termination of the daemon in high-security mode.

Persistent deletion is permitted only through an explicit command requiring strong confirmation:

```bash
vault destroy
```

## 1.2 Key protection, not secret storage locations

The application’s developers must understand and document exactly:

* Where the encrypted database is stored.
* Where wrapped keys are stored.
* How keys are generated.
* When keys exist in memory.
* How backup and recovery work.

Security must not depend on hiding filenames or confusing programmers.

The database contents will be cryptographically opaque, but the architecture itself must be understandable and auditable.

The system guarantees:

* The master password is never stored.
* The password-derived key is never stored.
* The root vault key is stored only in encrypted form.
* Derived encryption keys are never persisted.
* Plaintext secrets are never stored on disk.
* The database location may be known without revealing the data.

This is cryptographic protection rather than security through obscurity.

---

# 2. Product summary

albear is a local password and secrets manager consisting of:

* A Go background daemon.
* A Go CLI.
* A Google Chrome extension.
* An encrypted SQLite database.

The daemon is the only component allowed to:

* Open the database.
* Derive encryption keys.
* Decrypt records.
* Encrypt records.
* Manage lock state.
* Authorize clients.
* Perform backup and recovery.

The CLI and Chrome extension are clients of the daemon. They never open the database directly and never persist vault keys.

All client↔daemon traffic is encrypted with the Noise Protocol Framework. The Chrome extension establishes its own Noise session directly with the daemon; the native bridge relays opaque ciphertext frames and cannot read them.

```text
┌──────────────────────────┐
│ Chrome Extension         │
│                          │
│ Popup                    │
│ Background worker        │◄─── Noise session endpoint (end-to-end)
│ Content scripts          │
└────────────┬─────────────┘
             │ Chrome Native Messaging
             │ (opaque Noise ciphertext frames)
             ▼
┌──────────────────────────┐
│ Native Messaging Bridge  │
│ vault-native             │  Blind relay — cannot decrypt
└────────────┬─────────────┘
             │ Unix domain socket
             │ (opaque Noise ciphertext frames)
             ▼
┌──────────────────────────┐
│ Go Vault Daemon          │
│ vaultd                   │◄─── Noise session endpoint (end-to-end)
│                          │
│ Application services     │
│ Domain model             │
│ Encryption               │
│ Authorization            │
│ In-memory search index   │
└────────────┬─────────────┘
             │
             ▼
┌──────────────────────────┐
│ Encrypted SQLite Vault   │
└──────────────────────────┘
             ▲
             │ Unix domain socket
             │ (Noise-encrypted)
┌────────────┴─────────────┐
│ Go CLI                   │
│ vault                    │
└──────────────────────────┘
```

---

# 3. Problem statement

Developers and technical users need a safe local location for:

* Website credentials.
* API keys.
* Access tokens.
* Secure notes.
* Recovery codes.
* SSH-related metadata.
* Database credentials.
* Other small sensitive values.

Existing products may require cloud accounts, external infrastructure, subscriptions, or large applications.

albear must provide a focused local-only alternative that:

* Works without an internet connection.
* Has no cloud account.
* Has no telemetry.
* Exposes browser integration.
* Supports terminal workflows.
* Uses modern authenticated encryption.
* Encrypts every local IPC channel end-to-end.
* Remains fast with thousands of records.
* Fails closed when suspicious activity occurs.

---

# 4. Product principles

## 4.1 Local-only

The product must perform no network communication except communication inside the local machine.

The production daemon must not listen on:

```text
0.0.0.0
```

It should not expose a TCP port by default.

## 4.2 Fail closed

When the daemon cannot establish that an operation is valid, it rejects the operation.

Unexpected states must lead to locking rather than attempting unsafe recovery.

## 4.3 Encryption before persistence

Sensitive information must be encrypted before it reaches SQLite.

SQLite, its WAL file, temporary files, backups, and crash recovery files must contain ciphertext rather than plaintext user data.

## 4.4 Least privilege

The Chrome extension does not receive administrative capabilities.

For example, it cannot:

* Export the vault.
* Import a backup.
* Destroy the vault.
* Change cryptographic parameters.
* Manage clients.
* Rotate the master password without additional authorization.

## 4.5 Explicit user action

Passwords must not automatically appear on arbitrary pages.

Revealing, copying, or filling credentials requires a deliberate user action.

## 4.6 Recoverability

Security protections must not make normal crashes, browser updates, or operating-system changes destroy the user’s vault.

## 4.7 Auditable design

The cryptographic design, database format, key lifecycle, transport encryption, and authorization model must be documented.

No custom cryptographic algorithm may be introduced. The Noise Protocol Framework is a published, formally analyzed framework with published test vectors and satisfies this rule; ad-hoc handshake or framing constructions do not.

---

# 5. Goals

The first production release must:

1. Create and manage one local encrypted vault.
2. Store login credentials, secure notes, and API credentials.
3. Provide full CRUD operations through the CLI.
4. Match website credentials through the Chrome extension.
5. Fill credentials after explicit user approval.
6. Save and update credentials from Chrome.
7. Lock automatically after inactivity.
8. Change the master password without re-encrypting every record.
9. Detect altered ciphertext.
10. Export and restore encrypted backups.
11. Support at least 10,000 records with responsive search.
12. Operate without internet access.
13. Follow Domain-Driven Design.
14. Isolate the domain from SQLite, HTTP, Chrome, and command-line implementation details.
15. Encrypt all client↔daemon traffic end-to-end with the Noise Protocol Framework, including the Chrome extension path.

---

# 6. Non-goals

The following are outside the initial scope:

* Cloud synchronization.
* Peer-to-peer synchronization.
* Mobile applications.
* Multi-user vaults.
* Team sharing.
* Remote access.
* Firefox and Safari extensions.
* Browser account synchronization.
* Large file storage.
* Password autofill without user action.
* Passkey storage.
* SSH agent functionality.
* Hardware-security-module integration.
* Protection after complete operating-system compromise.
* Protection against root or administrator access while the vault is unlocked.

---

# 7. Users

## 7.1 Primary user

A technical user who works from both the terminal and Chrome and wants a local-only secrets manager.

Typical expectations:

* Fast keyboard-driven interaction.
* No cloud dependency.
* Predictable storage and backup behavior.
* Strong encryption.
* Clear error messages.
* Scriptable, redacted CLI output.

## 7.2 Security-conscious user

A user who values:

* Short automatic lock time.
* Manual panic locking.
* Restricted browser access.
* No clipboard use.
* Local security-event history.
* Strong master-password derivation.
* Encrypted local IPC, even against a compromised relay process.

---

# 8. Supported record types

## 8.1 Login

```text
Name
Username
Password
URLs
Notes
Tags
Custom fields
Creation time
Modification time
```

## 8.2 Secure note

```text
Name
Body
Tags
Custom fields
Creation time
Modification time
```

## 8.3 API credential

```text
Name
Service
API key
API secret
Environment
Endpoint
Notes
Tags
Custom fields
Creation time
Modification time
```

The internal domain model must allow new record types without altering the core vault format.

TOTP seeds, payment cards, identities, attachments, and SSH keys are deferred.

---

# 9. Domain-Driven Design

## 9.1 Bounded contexts

### Vault Security Context

Responsible for:

* Vault creation.
* Lock state.
* Unlocking.
* Key derivation.
* Key wrapping.
* Automatic locking.
* Master-password changes.
* Panic locking.

This is the central security context.

### Secret Catalog Context

Responsible for:

* Record creation.
* Record modification.
* Record deletion.
* Search.
* Domain matching.
* Validation of record types.
* Record revision management.

### Client Access Context

Responsible for:

* Client registration.
* Chrome bridge and extension approval.
* Client capabilities.
* Session authorization.
* Revocation.
* Request validation.
* Noise handshake authorization.
* Client static-key pinning and PSK binding.

### Browser Integration Context

Responsible for translating browser activity into constrained domain operations:

* Determine the trusted browser origin.
* Find matching login records.
* Fill an approved login.
* Capture a candidate login.
* Request save or update.

The Browser Integration Context must not contain cryptographic implementation logic.

### Backup and Recovery Context

Responsible for:

* Encrypted export.
* Backup verification.
* Restore validation.
* Format migration.
* Recovery from interrupted operations.

### Security Monitoring Context

Responsible for:

* Unauthorized request events.
* Integrity failures.
* Transport handshake and AEAD failures.
* Repeated unlock failures.
* Client revocations.
* Panic-lock events.
* Local-only audit reporting.

---

## 9.2 Context map

```text
Browser Integration
        │
        ▼
Client Access ──────────────┐
        │                   │
        ▼                   ▼
Secret Catalog ───────► Vault Security
        │                   │
        └──────┬────────────┘
               ▼
       Backup and Recovery

All contexts publish events to:

       Security Monitoring
```

The Secret Catalog must not decrypt data independently. It receives a temporary cryptographic capability from the Vault Security context while the vault is unlocked.

---

## 9.3 Layering rules

### Domain layer

Contains:

* Aggregates.
* Entities.
* Value objects.
* Domain errors.
* Domain services.
* Repository interfaces.
* Domain events.

It must not import:

* `database/sql`
* `net/http`
* Chrome-specific packages
* CLI frameworks
* SQLite drivers
* Noise or transport libraries

### Application layer

Contains use cases such as:

* `CreateVault`
* `UnlockVault`
* `CreateRecord`
* `MatchLoginRecords`
* `RevealRecordSecret`
* `ChangeMasterPassword`
* `ExportBackup`

It coordinates domain objects and repository ports.

### Adapter layer

Contains:

* CLI handlers.
* Native messaging handlers.
* Local socket handlers.
* DTO conversion.
* Request validation.

### Infrastructure layer

Contains:

* SQLite repositories.
* Cryptographic implementations.
* Noise transport implementation.
* Operating-system IPC.
* File permissions.
* Runtime memory protection.
* Native messaging registration.
* System service integration.

---

# 10. Domain model

## 10.1 Vault aggregate

```go
type Vault struct {
    ID                    VaultID
    State                 VaultState
    FormatVersion         uint32
    ActiveEnvelopeVersion uint32
    LockPolicy            LockPolicy
}
```

### Vault invariants

* A vault is either locked or unlocked.
* Record decryption is impossible through the application layer while locked.
* Only one key envelope may be active.
* Master-password changes must be atomic.
* Locking invalidates all existing sessions.
* The root vault key must never appear in domain events or logs.

---

## 10.2 Secret record aggregate

```go
type Record struct {
    ID       RecordID
    Type     RecordType
    Revision uint64
    Metadata RecordMetadata
    Secret   SecretPayload
}
```

### Metadata

```go
type RecordMetadata struct {
    Name       string
    Username   string
    URLs       []LoginURL
    Tags       []string
    CreatedAt  time.Time
    UpdatedAt  time.Time
    CustomKeys []string
}
```

### Secret payload

```go
type SecretPayload struct {
    Password     SecretString
    Notes        SecretString
    APIKey       SecretString
    APISecret    SecretString
    CustomValues map[string]SecretString
}
```

Metadata and secret payload are encrypted separately.

This allows the daemon to decrypt searchable metadata when the vault is unlocked without keeping every password or API token decrypted in memory.

### Record invariants

* Record IDs are generated randomly.
* Revisions increase monotonically.
* A record cannot contain an invalid URL.
* A login must contain at least a name and one credential or URL.
* A secret value must never be converted into a loggable representation.
* Updating one record must generate new encryption nonces.

---

## 10.3 Client aggregate

```go
type Client struct {
    ID           ClientID
    Kind         ClientKind
    Status       ClientStatus
    Capabilities CapabilitySet
    StaticKey    NoiseStaticPublicKey
}
```

Client kinds:

```text
CLI
ChromeExtension
ChromeNativeHost
AdministrativeTool
```

The Chrome extension is a first-class client with its own credential and Noise static key; the native host is a separate client acting only as a relay. Each receives only the capabilities its role requires.

---

## 10.4 Session entity

Sessions exist only in memory.

```go
type Session struct {
    ID           SessionID
    ClientID     ClientID
    TransportID  TransportSessionID
    Capabilities CapabilitySet
    CreatedAt    time.Time
    LastActivity time.Time
    ExpiresAt    time.Time
    VaultEpoch   uint64
}
```

`VaultEpoch` increments every time the vault locks. A session from an earlier epoch becomes invalid immediately, even if its underlying Noise transport session is still connected.

Sessions must not survive daemon restarts.

---

## 10.5 Value objects

Required value objects include:

```text
VaultID
RecordID
ClientID
SessionID
TransportSessionID
RecordRevision
CanonicalOrigin
CanonicalHost
SecretString
LockPolicy
KDFParameters
KeyEnvelopeVersion
NoiseStaticPublicKey
HandshakePattern
RequestID
Capability
```

A `SecretString` must deliberately avoid implementing common formatting interfaces such as `fmt.Stringer`.

---

## 10.6 Domain events

```text
VaultCreated
VaultUnlocked
VaultLocked
VaultPanicLocked
MasterPasswordChanged
RecordCreated
RecordUpdated
RecordDeleted
ClientPairingRequested
ClientApproved
ClientRevoked
TransportHandshakeFailed
BackupCreated
BackupRestored
IntegrityFailureDetected
UnauthorizedRequestRejected
```

Events must never contain passwords, API keys, full notes, master passwords, encryption keys, transport session keys, or decrypted payloads.

---

# 11. Application components

The product should ship three Go executables from one Go module:

```text
vaultd         Local daemon
vault          CLI client
vault-native   Chrome Native Messaging bridge
```

They may share internal packages but must have separate entry points and restricted responsibilities.

## 11.1 `vaultd`

Responsibilities:

* Own the SQLite connection.
* Own in-memory keys.
* Own lock state.
* Execute application commands.
* Maintain the in-memory metadata index.
* Terminate Noise transport sessions and authenticate local clients.
* Perform encryption and decryption.

## 11.2 `vault`

Responsibilities:

* Parse commands.
* Read master passwords securely from the terminal.
* Communicate with `vaultd` over its own Noise session.
* Render redacted results.
* Perform administrative workflows.

## 11.3 `vault-native`

`vault-native` is a **blind relay**. It moves opaque Noise ciphertext frames between the extension and the daemon and cannot read or modify their contents without detection.

Responsibilities:

* Communicate with Chrome through native messaging.
* Verify the calling Chrome extension origin.
* Authenticate itself to `vaultd` over its own Noise session, proving which bridge instance is connecting.
* Relay the extension's end-to-end Noise frames to `vaultd` unmodified.
* Enforce message-size limits on relayed frames.
* Never open the database.
* Never persist vault secrets.
* Never hold keys capable of decrypting extension↔daemon traffic.

Chrome Native Messaging uses a registered host, launches the native application as a separate process, and communicates through length-prefixed JSON over standard input and output. Chrome also provides the calling extension origin, while the host manifest restricts allowed extension origins. ([Chrome for Developers][1])

---

# 12. Local communication architecture

## 12.1 Daemon transport

For Linux, the daemon must serve its interface over a Unix domain socket:

```text
$XDG_RUNTIME_DIR/albear/vault.sock
```

Permissions:

```text
Directory: 0700
Socket:    0600
```

The daemon must validate the peer user ID using operating-system peer credentials where supported.

Socket permissions and peer credentials are the first layer; the Noise transport layer (section 12.4) is required in addition, on every transport, in every build.

The production server must not expose a loopback TCP API by default.

An optional TCP listener may exist only in an explicitly marked development build. Because all application traffic is Noise-encrypted above the transport, even the development TCP listener never carries plaintext.

## 12.2 Chrome transport

```text
Chrome extension  ◄════════ end-to-end Noise session ════════►  vaultd
      │                                                            ▲
      │ Native Messaging (ciphertext frames)                       │
      ▼                                                            │
vault-native ──── Unix domain socket (ciphertext frames) ──────────┘
```

The extension must never communicate directly with `127.0.0.1`.

The extension's Noise session terminates inside `vaultd`, not inside `vault-native`. The bridge sees only ciphertext.

## 12.3 CLI transport

```text
vault CLI ◄──── Noise session over Unix domain socket ────► vaultd
```

The CLI holds its own client credential and Noise static key. First-run CLI pairing may be auto-approved when the daemon verifies same-UID peer credentials and an interactive terminal, avoiding pairing ceremony for the primary local user; the auto-approval is still recorded as a security event.

Master passwords must not be accepted as normal command-line arguments because arguments may appear in shell history or process listings.

Accepted input mechanisms:

* Interactive terminal prompt.
* Standard input when explicitly requested.
* Dedicated file descriptor.

## 12.4 Transport encryption

All client↔daemon application traffic is encrypted with the **Noise Protocol Framework**, regardless of the underlying transport (Unix domain socket today; named pipes or a development TCP listener later). OS-level protections remain in place; Noise is layered on top, not substituted for them.

### Cipher suite

```text
Paired clients:    Noise_XXpsk3_25519_ChaChaPoly_SHA256
Unpaired clients:  Noise_XX_25519_ChaChaPoly_SHA256 (pairing channel only)
```

ChaCha20-Poly1305 matches the vault's existing cipher family. The Noise Protocol Framework is a published specification with formal analysis and test vectors; no custom handshake or framing construction is permitted (section 4.7).

* **Unpaired clients** (first contact) use plain `XX` to establish an encrypted channel that may carry only the pairing request. The resulting session receives pairing capabilities and nothing else.
* **Paired clients** use `XXpsk3` with **PSK = the client's 32-byte credential** (section 18.3). Once a client has pinned the daemon's static key, `IKpsk2` may be used to save a round trip. Binding the credential into the handshake makes channel encryption and client authentication a single cryptographic act: a client that cannot prove the credential cannot complete the handshake at all.

### Keys

* `vaultd` holds a long-term X25519 static key, generated at first run, stored with `0600` permissions in the configuration directory. It is a transport identity key only — its compromise does not reveal vault data, because the root vault key remains wrapped by the master password. Clients pin this key at pairing (trust-on-first-use, confirmed by the pairing phrase).
* Each client — CLI, `vault-native`, and the Chrome extension — holds its own X25519 static key plus its client-credential PSK.

### Session properties

* Mutual authentication on every paired handshake.
* Forward secrecy: fresh ephemeral Diffie-Hellman keys per connection.
* Per-message AEAD using Noise transport nonces; any tampered frame fails authentication.
* Rekey after a bounded number of messages or a bounded time, whichever comes first.
* Application sessions are bound to `VaultEpoch`: locking the vault invalidates every application session immediately even if the underlying Noise transport connection survives.
* Transport session keys exist only in memory and never survive process restart.

### Framing

* Length-prefixed Noise transport messages.
* A maximum frame size is enforced at both ends; on the Chrome path frames must stay within Chrome's 1 MB native-messaging limit including relay overhead.
* The JSON request/response envelopes of section 24 travel unchanged **inside** Noise payloads. The plaintext protocol is unaffected; only the wrapping changed.
* The IPC protocol version is carried in the first encrypted handshake payload; a major mismatch aborts the connection (section 24.4). Downgrade to an unencrypted mode must be impossible — there is no plaintext operational mode in any build.

### Chrome end-to-end path

The extension's background service worker is a full Noise endpoint:

* The handshake and transport cipher run inside the service worker, using WebCrypto where available (X25519, SHA-256) plus an audited, bundled JavaScript implementation of ChaCha20-Poly1305 (for example `@noble/ciphers` with `@noble/curves`). All cryptographic code is bundled at build time; no remote code (section 13.1).
* The extension's static key and client credential are stored in `chrome.storage.local`. **Documented limitation:** malware running as the same operating-system user can read that storage. End-to-end encryption defends against a compromised or curious bridge process and passive observation of the socket or pipes — it does not defend against full same-user compromise (section 18.4).
* Noise handshake state lives only in service-worker memory. A service-worker restart requires a fresh handshake; it must not unlock the vault or resume the previous application session.

`vault-native` relays these frames blindly. It authenticates itself to `vaultd` on its own Noise session so the daemon knows which bridge is connecting, but the extension↔daemon frames pass through it as opaque ciphertext.

---

# 13. Chrome extension requirements

## 13.1 Technology

* Chrome Manifest V3.
* TypeScript with strict mode.
* Background service worker.
* Popup UI.
* Content scripts.
* Native Messaging permission.
* Minimum required host permissions.
* Bundled Noise/cryptography implementation (section 12.4); no WASM fetched at runtime.

The extension must not load remote JavaScript.

## 13.2 Extension components

### Popup

Displays:

* Native host connection state.
* End-to-end transport session state.
* Vault lock state.
* Matching records for the current tab.
* Search.
* Fill action.
* Copy action when enabled.
* Add-login action.
* Lock action.
* Security warning when the page uses HTTP.

### Background service worker

Responsibilities:

* Maintain the Native Messaging connection.
* Maintain the end-to-end Noise session with `vaultd` (section 12.4).
* Validate content-script messages.
* Determine the sender’s actual tab and frame origin.
* Request constrained operations from `vaultd` through the encrypted channel.
* Return secrets only to the specific requesting tab.
* Prevent cross-tab and cross-frame leakage.

### Content script

Responsibilities:

* Detect login forms.
* Identify username and password fields.
* Request matching records.
* Fill fields after explicit approval.
* Detect possible login submissions.
* Offer save or update.

It must not:

* Persist credentials.
* Log form values.
* Send credentials to extension storage.
* Broadcast credentials.
* Trust a website-provided domain string.
* Hold Noise keys or handshake state (transport crypto lives only in the service worker).

## 13.3 Origin matching

The extension and daemon must operate on parsed origins rather than substring matching.

Valid matching:

```text
https://github.com
https://www.github.com
```

Invalid matching:

```text
https://github.com.attacker.example
https://evilgithub.com
https://github-login.example
```

Canonical matching must account for:

* Scheme.
* Host.
* Effective port.
* Internationalized domain normalization.
* Subdomain policy.
* Top-level origin.
* Frame origin.

Cross-origin iframe filling is disabled by default.

HTTP-site filling is disabled by default and requires an explicit per-site override.

## 13.4 Reveal policy

The extension receives:

* Record name.
* Username.
* Matched host.
* Record ID.

The password is returned only after:

1. The user selects a specific record.
2. The service worker verifies the requesting tab.
3. The daemon verifies the expected origin.
4. The daemon authorizes a `reveal-for-fill` capability.
5. The content script fills that specific page.

The password must not be included in general search results.

The revealed password crosses the bridge only as Noise ciphertext; `vault-native` cannot observe it.

## 13.5 Chrome pairing

Initial workflow:

1. Extension connects to `vault-native`, which validates the exact production extension ID.
2. Extension opens an unpaired `Noise_XX` handshake to `vaultd`, relayed through `vault-native`. The daemon grants this session pairing capabilities only.
3. A pairing request is created, carrying the extension's Noise static public key.
4. Extension displays a short pairing phrase. The phrase commits to both static public keys (extension's and daemon's), so a bridge attempting to man-in-the-middle the pairing produces a mismatched phrase.
5. User approves it through:

```bash
vault clients approve
```

The CLI displays the same phrase; the user confirms they match.

6. Daemon registers the extension as a client, pinning its static key.
7. Daemon issues the client credential through the just-verified encrypted channel. The extension stores the credential and pins the daemon's static key in `chrome.storage.local`.
8. All subsequent connections use `XXpsk3` (or `IKpsk2`) with the credential as PSK.
9. The credential may be revoked through the CLI; revocation invalidates the PSK, so a revoked extension can no longer complete a handshake.

`vault-native` pairs separately as its own relay client with its own credential.

Development and production extension IDs must be separate.

Development origins must never appear in production Native Messaging manifests.

---

# 14. CLI requirements

## 14.1 Command structure

```bash
vault init
vault status
vault unlock
vault lock
vault panic-lock

vault add login
vault add note
vault add api-key

vault list
vault search <query>
vault show <record>
vault edit <record>
vault remove <record>

vault generate password
vault password change

vault clients list
vault clients approve
vault clients revoke

vault backup create
vault backup verify
vault backup restore

vault doctor
vault destroy
```

## 14.2 Redaction

The following command:

```bash
vault show github
```

must redact sensitive fields by default.

Full reveal requires:

```bash
vault show github --reveal
```

Structured output must preserve the same behavior:

```bash
vault show github --json
```

must not reveal secrets unless `--reveal` is also supplied.

## 14.3 Clipboard

Clipboard use is optional.

When enabled:

* Values are cleared after a configurable interval.
* The CLI must warn that clipboard managers may retain historical content.
* Clipboard contents must not be used for the master password.

## 14.4 Exit codes

```text
0   Success
2   Invalid arguments
3   Vault locked
4   Authentication failed
5   Record not found
6   Authorization denied
7   Integrity failure
8   Daemon unavailable
9   Conflict or revision mismatch
10  Internal failure
```

Error output must never contain sensitive request values.

---

# 15. Core workflows

## 15.1 Create vault

1. User runs `vault init`.
2. CLI requests a master password twice.
3. Daemon generates a random root vault key.
4. Daemon generates a random Argon2id salt.
5. Daemon derives a key-encryption key.
6. Daemon encrypts the root key.
7. Daemon writes the vault header and key envelope atomically.
8. Daemon creates and verifies an encrypted canary.
9. Daemon generates its X25519 transport static key if absent.
10. Temporary password buffers and derived keys are cleared best-effort.
11. Vault remains unlocked or locks based on setup preference.

## 15.2 Unlock

1. Client requests unlock over its Noise session.
2. Daemon applies unlock rate limiting.
3. Password is converted directly to a byte buffer.
4. Argon2id derives the key-encryption key.
5. Daemon attempts to decrypt the root vault key.
6. Any failure returns a generic unlock failure.
7. Derived subkeys are generated.
8. Encrypted metadata is loaded and decrypted.
9. Search and domain indexes are built in memory.
10. Password and key-encryption key buffers are cleared.
11. Vault epoch increments.
12. A new session is issued.

## 15.3 Lock

1. Stop accepting decrypt operations.
2. Invalidate all sessions.
3. Increment vault epoch.
4. Clear the in-memory metadata index.
5. Clear decrypted key buffers best-effort.
6. Close long-lived extension capabilities.
7. Publish `VaultLocked`.

Noise transport connections may remain open after a lock, but every application session bound to the previous epoch is dead; clients must re-authorize after unlock.

## 15.4 Add record

1. Validate client capability.
2. Validate domain object.
3. Generate a random record ID.
4. Set revision to `1`.
5. Serialize metadata and secret payload separately.
6. Generate two independent random nonces.
7. Encrypt both payloads.
8. Write the record in one transaction.
9. Update the in-memory metadata index after commit.
10. Clear temporary plaintext buffers.

## 15.5 Update record

1. Read expected revision from the client.
2. Reject stale revisions.
3. Increment revision.
4. Generate new nonces.
5. Encrypt new metadata and secret payload.
6. Update atomically.
7. Replace the in-memory index entry.

Optimistic concurrency prevents one client from silently overwriting another client’s update.

## 15.6 Change master password

1. Authenticate the current master password.
2. Decrypt the existing root vault key.
3. Generate a new salt.
4. Derive a new key-encryption key.
5. Re-encrypt only the root vault key.
6. Replace the active key envelope in one transaction.
7. Delete the previous envelope.
8. Invalidate all sessions.
9. Lock the vault.

Records do not need to be re-encrypted because they use keys derived from the random root vault key.

---

# 16. Cryptographic design

## 16.1 Algorithms

```text
Password KDF:         Argon2id
Record encryption:    XChaCha20-Poly1305
Key separation:       HKDF-SHA-256
Transport encryption: Noise_XXpsk3_25519_ChaChaPoly_SHA256
Key agreement:        X25519
Randomness:           crypto/rand
MAC where required:   HMAC-SHA-256
```

Argon2id is the required password-based key derivation function. RFC 9106 identifies `64 MiB`, three iterations, and four lanes as its second recommended profile for memory-constrained environments. The application should treat that as a minimum baseline and calibrate upward where the machine permits. ([IETF Datatracker][2])

Go’s XChaCha20-Poly1305 implementation uses a 256-bit key and a longer nonce intended for cases where nonces are generated randomly. ([Go Packages][3])

HKDF is used to derive separate cryptographic keys from the vault root key rather than reusing one key for multiple purposes. ([Go Packages][4])

Transport encryption uses the Noise Protocol Framework (section 12.4); in Go the `flynn/noise` implementation, pinned and audited, is the reference choice.

## 16.2 Key hierarchy

```text
Master password
      │
      ▼
Argon2id
      │
      ▼
Key Encryption Key
      │
      ▼
Decrypts wrapped Root Vault Key
      │
      ▼
Root Vault Key
      │
      ├── HKDF("metadata")  ──► Metadata Encryption Key
      ├── HKDF("secrets")   ──► Secret Encryption Key
      ├── HKDF("audit")     ──► Audit Authentication Key
      └── HKDF("backup")    ──► Backup Authentication Key
```

The transport key hierarchy is deliberately separate from the vault key hierarchy:

```text
Daemon X25519 static key ─┐
Client X25519 static key ─┤
Ephemeral X25519 keys ────┼──► Noise handshake ──► per-session transport keys
Client credential (PSK) ──┘
```

Transport keys never derive from, and can never reveal, the root vault key. A stolen transport key or credential still faces a locked, Argon2id-wrapped vault.

## 16.3 Key sizes

```text
Root vault key:           32 bytes
Key-encryption key:       32 bytes
Derived keys:             32 bytes each
Argon2 salt:              16 bytes minimum
XChaCha nonce:            24 bytes
Client credential (PSK):  32 random bytes
X25519 static keys:       32 bytes
X25519 ephemeral keys:    32 bytes
```

## 16.4 Argon2 parameters

Default initial profile:

```text
Algorithm:    Argon2id
Version:      1.3
Memory:       128–256 MiB when available
Iterations:   3 minimum
Parallelism:  min(4, available CPU threads)
Target time:  approximately 400–800 ms on the current machine
```

Hard minimum:

```text
Memory:       64 MiB
Iterations:   3
Parallelism:  1–4
```

The exact parameters are stored with the key envelope so future versions can increase the cost.

A weak master password remains vulnerable to guessing. The UI must recommend a long passphrase and must not describe the vault as “uncrackable.”

## 16.5 Associated authenticated data

Each encrypted payload must bind its identity and version through AEAD-associated data.

Metadata AAD:

```text
vault_id
record_id
record_revision
payload_kind = metadata
format_version
key_version
```

Secret AAD:

```text
vault_id
record_id
record_revision
payload_kind = secret
format_version
key_version
```

This prevents moving ciphertext between records or interpreting one payload type as another.

## 16.6 Nonce handling

Every record encryption operation uses a newly generated 24-byte nonce from `crypto/rand`.

Nonces must never be:

* Derived from timestamps.
* Derived from record IDs.
* Reused after updates.
* Manually incremented across daemon restarts.
* Accepted from clients.

Noise transport nonces are the exception: they follow the Noise specification's counter-based scheme within a single session, with mandatory rekeying before counter exhaustion. They are never persisted and never span sessions.

## 16.7 Key storage

Persistent:

```text
Root vault key:              encrypted only
Master password:             never stored
Key-encryption key:          never stored
Derived record keys:         never stored
Daemon X25519 static key:    stored 0600 in config dir (transport identity, not a vault secret)
Client X25519 static keys:   stored 0600 by each client (extension: chrome.storage.local)
Client credentials (PSKs):   stored 0600 by each client; daemon stores only a verifier hash
```

Runtime:

```text
Root vault key:          daemon memory while unlocked
Derived keys:            daemon memory while unlocked
Master password:         temporary buffer during unlock
Record secrets:          decrypted only for requested operations
Search metadata:         daemon memory while unlocked
Noise session keys:      memory only, per connection, never persisted
```

## 16.8 Memory limitations

Go is garbage collected, so complete proof of memory zeroization cannot be guaranteed.

The implementation must still:

* Avoid converting passwords to immutable strings.
* Use byte slices for secret material.
* Avoid unnecessary copies.
* Overwrite buffers before release.
* Use `runtime.KeepAlive` where needed.
* Use operating-system page locking where practical.
* Disable core dumps for the daemon where practical.
* Avoid swap exposure where operating-system support exists.
* Clear all in-memory indexes on lock.

The product must document that root or kernel-level access can inspect process memory while the vault is unlocked.

---

# 17. Database design

## 17.1 Database engine

SQLite is selected because:

* The product is single-machine and local-only.
* The daemon is the only database writer.
* Transactions provide crash-safe updates.
* Backups can be created consistently.
* It avoids operating an external database server.

Use:

```text
WAL mode
FULL synchronous mode
Foreign keys enabled
STRICT tables
Busy timeout
Trusted schema disabled
Prepared statements
```

WAL maintains a separate write-ahead log and allows database changes to be committed through that log. Because all sensitive fields are encrypted before SQLite receives them, the main database and WAL contain ciphertext. ([SQLite][5])

SQLite `STRICT` tables enforce declared column types and reject values that cannot be losslessly converted to the expected type. ([SQLite][6])

## 17.2 Storage location

Linux default:

```text
Database:
$XDG_DATA_HOME/albear/vault.db

Configuration (including daemon transport static key):
$XDG_CONFIG_HOME/albear/

Runtime socket:
$XDG_RUNTIME_DIR/albear/vault.sock
```

Fallback when XDG variables are absent:

```text
~/.local/share/albear/vault.db
~/.config/albear/
```

Permissions:

```text
Data directory:   0700
Database files:   0600
Configuration:    0600
Backup default:   0600
```

The storage path is not secret. The contents are protected cryptographically.

## 17.3 Database schema

### Schema migrations

```sql
CREATE TABLE schema_migrations (
    version       INTEGER PRIMARY KEY,
    applied_at_ms INTEGER NOT NULL,
    checksum      BLOB NOT NULL CHECK(length(checksum) = 32)
) STRICT;
```

### Vault

```sql
CREATE TABLE vault (
    singleton_id            INTEGER PRIMARY KEY
                                CHECK(singleton_id = 1),
    vault_id                BLOB NOT NULL UNIQUE
                                CHECK(length(vault_id) = 16),
    format_version          INTEGER NOT NULL,
    active_envelope_version INTEGER NOT NULL,
    created_at_ms           INTEGER NOT NULL,
    updated_at_ms           INTEGER NOT NULL
) STRICT;
```

This table contains no secret data.

### Key envelopes

```sql
CREATE TABLE key_envelopes (
    envelope_version  INTEGER PRIMARY KEY,
    vault_id          BLOB NOT NULL
                         CHECK(length(vault_id) = 16),

    kdf_algorithm     TEXT NOT NULL
                         CHECK(kdf_algorithm = 'argon2id'),
    kdf_version       INTEGER NOT NULL,
    kdf_salt          BLOB NOT NULL
                         CHECK(length(kdf_salt) >= 16),
    kdf_memory_kib    INTEGER NOT NULL
                         CHECK(kdf_memory_kib >= 65536),
    kdf_iterations    INTEGER NOT NULL
                         CHECK(kdf_iterations >= 3),
    kdf_parallelism   INTEGER NOT NULL
                         CHECK(kdf_parallelism >= 1),

    wrap_algorithm    TEXT NOT NULL
                         CHECK(wrap_algorithm = 'xchacha20poly1305'),
    wrap_nonce        BLOB NOT NULL
                         CHECK(length(wrap_nonce) = 24),
    wrapped_root_key  BLOB NOT NULL,

    canary_nonce      BLOB NOT NULL
                         CHECK(length(canary_nonce) = 24),
    encrypted_canary  BLOB NOT NULL,

    created_at_ms     INTEGER NOT NULL,

    FOREIGN KEY(vault_id)
        REFERENCES vault(vault_id)
        ON DELETE CASCADE
) STRICT;
```

### Records

```sql
CREATE TABLE records (
    record_id          BLOB PRIMARY KEY
                          CHECK(length(record_id) = 16),

    key_version        INTEGER NOT NULL,
    revision           INTEGER NOT NULL
                          CHECK(revision >= 1),

    metadata_nonce     BLOB NOT NULL
                          CHECK(length(metadata_nonce) = 24),
    metadata_ciphertext BLOB NOT NULL,

    secret_nonce       BLOB NOT NULL
                          CHECK(length(secret_nonce) = 24),
    secret_ciphertext  BLOB NOT NULL,

    payload_version    INTEGER NOT NULL,

    FOREIGN KEY(key_version)
        REFERENCES key_envelopes(envelope_version)
) STRICT, WITHOUT ROWID;
```

No title, username, URL, password, note, tag, API key, or timestamp is stored in plaintext.

User-visible timestamps are inside encrypted metadata.

### Registered clients

```sql
CREATE TABLE clients (
    client_id            BLOB PRIMARY KEY
                            CHECK(length(client_id) = 16),

    client_kind          INTEGER NOT NULL,
    status               INTEGER NOT NULL,
    capability_mask      INTEGER NOT NULL,

    credential_hash      BLOB NOT NULL
                            CHECK(length(credential_hash) = 32),

    noise_static_pubkey  BLOB NOT NULL
                            CHECK(length(noise_static_pubkey) = 32),

    label_nonce          BLOB NOT NULL
                            CHECK(length(label_nonce) = 24),
    label_ciphertext     BLOB NOT NULL,

    created_at_ms        INTEGER NOT NULL,
    last_seen_at_ms      INTEGER
) STRICT, WITHOUT ROWID;
```

The client’s raw credential is not stored in the database. Only a verifier is stored. The client's Noise static public key is pinned here at approval; a handshake from a registered client presenting a different static key must be rejected and recorded.

MAC comparison must use a constant-time comparison. Go provides dedicated constant-time comparison primitives and an HMAC comparison method intended to avoid content-dependent timing behavior. ([Go Packages][7])

### Security events

```sql
CREATE TABLE security_events (
    sequence_id         INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at_ms      INTEGER NOT NULL,
    severity            INTEGER NOT NULL,
    event_code          INTEGER NOT NULL,

    details_nonce       BLOB,
    details_ciphertext  BLOB,

    CHECK(
        (details_nonce IS NULL AND details_ciphertext IS NULL)
        OR
        (length(details_nonce) = 24 AND details_ciphertext IS NOT NULL)
    )
) STRICT;
```

Locked-state events may contain only non-sensitive numeric event codes.

## 17.4 Database rules

* Only `vaultd` may open the database.
* Every write occurs inside a transaction.
* Migrations are checksummed.
* Migration downgrade is unsupported.
* Unknown newer format versions cause read refusal.
* SQL statements are prepared.
* User-provided values are never interpolated into SQL.
* Backups use a transactionally consistent SQLite snapshot.
* Database corruption never causes fallback to plaintext recovery.

## 17.5 Search design

At unlock:

1. Daemon reads all encrypted metadata blobs.
2. Daemon decrypts metadata only.
3. Daemon builds in-memory indexes.

Indexes include:

```text
Record ID → Metadata
Canonical host → Record IDs
Normalized name tokens → Record IDs
Normalized username tokens → Record IDs
Tags → Record IDs
```

Passwords, notes, API keys, and API secrets are not included in the index.

On lock, the complete index is destroyed best-effort.

---

# 18. Authorization model

## 18.1 CLI capabilities

The CLI may receive:

```text
VaultStatus
VaultUnlock
VaultLock
RecordList
RecordRead
RecordReveal
RecordWrite
RecordDelete
BackupCreate
BackupRestore
ClientAdmin
PasswordChange
VaultDestroy
```

Administrative operations require an interactive terminal and may require reauthentication.

## 18.2 Chrome capabilities

The Chrome extension client may receive:

```text
VaultStatus
VaultUnlock
VaultLock
RecordMatch
RecordRevealForOrigin
RecordCreateLogin
RecordUpdateLogin
PasswordGenerate
```

It must never receive:

```text
BackupRestore
BackupExport
ClientAdmin
VaultDestroy
RawDatabaseAccess
UnrestrictedRecordReveal
```

The `vault-native` relay client receives only relay capabilities: it may forward frames and query daemon availability. It receives no record, vault, or administrative capabilities of its own.

## 18.3 Client credentials

Client credentials protect the daemon from unauthorized IPC clients, but they do not decrypt the vault.

Each credential also serves as the pre-shared key (PSK) in the client's Noise handshake (section 12.4). Authentication and channel encryption are therefore inseparable: without the credential, a client cannot complete a handshake, and without completing a handshake, it cannot send a single application request.

A stolen client credential must still be insufficient to access a locked vault: it grants a transport session, not vault decryption. The root vault key remains protected by the master password.

## 18.4 Same-user compromise

A malicious process running as the same operating-system user may be able to:

* Read client credential files and the extension's `chrome.storage.local`.
* Connect to local IPC and complete a handshake using stolen credentials.
* Inspect clipboard data.
* Inject into the browser.
* Inspect daemon memory while unlocked.

The product cannot fully protect against this threat without hardware-backed isolation or a separate trusted operating-system account.

Transport encryption changes what a same-user attacker must do — passive observation of the socket or of the bridge process no longer yields plaintext; the attacker must actively steal credentials or inspect process memory — but it does not eliminate the threat. The product must not claim otherwise.

The vault remains protected at rest when locked.

---

# 19. Suspicious activity and panic response

## 19.1 Response levels

### Level 1 — Reject

Used for:

* Invalid request structure.
* Unsupported command.
* Invalid record ID.
* Missing capability.
* Stale revision.

Action:

* Reject request.
* Return a generic error.
* Record a rate-limited event.

### Level 2 — Disconnect

Used for:

* Invalid client credential.
* Noise handshake failure.
* Transport AEAD authentication failure on any frame.
* Static-key mismatch for a registered client.
* Unexpected Chrome extension origin.
* Protocol framing violation.
* Oversized message.
* Repeated malformed requests.

Action:

* Close connection.
* Revoke current connection session.
* Apply backoff.

### Level 3 — Panic lock

Used for:

* Record AEAD authentication failure.
* Key-envelope integrity failure.
* Internal cryptographic invariant failure.
* Impossible vault state.
* Explicit panic-lock request.
* Optional debugger detection in high-security mode.

Action:

1. Block new operations.
2. Invalidate every session.
3. Clear decrypted metadata.
4. Clear key buffers best-effort.
5. Close connections.
6. Record a generic security event.
7. Optionally terminate the daemon.

### Level 4 — Explicit vault destruction

Permitted only through:

```bash
vault destroy
```

Required confirmation:

1. Interactive terminal.
2. Current master password.
3. Typed vault identifier.
4. Typed confirmation phrase.
5. Warning that SSD and filesystem behavior may prevent guaranteed physical erasure.
6. Optional verified backup check.

No Chrome capability may invoke this operation.

## 19.2 Anti-debugging

Debugger detection is defense-in-depth, not a primary security boundary.

High-security mode may inspect:

* Linux `TracerPid`.
* Process dumpability.
* Unexpected parent process.
* Core-dump configuration.
* Runtime integrity markers.

A detected debugger triggers a panic lock and optional exit.

It does not automatically delete the vault.

## 19.3 Unlock failures

Failed unlock attempts trigger increasing delay:

```text
Attempts 1–3: normal response
Attempts 4–5: short delay
Attempts 6–10: increasing delay
Further attempts: bounded delay and security warning
```

This slows interactive abuse but is not the main defense against an attacker who has copied the database. Argon2id and a strong master password provide offline-guessing resistance.

---

# 20. Performance requirements

Targets are measured on a typical modern laptop with 10,000 records.

| Operation                                |            Target |
| ---------------------------------------- | ----------------: |
| Locked daemon startup                    |      Under 200 ms |
| CLI status                               |       Under 50 ms |
| Unlock excluding configured KDF delay    |      Under 500 ms |
| Complete unlock including metadata index | Under 1.5 seconds |
| Search after unlock                      |   p95 under 25 ms |
| Exact host match                         |   p95 under 10 ms |
| Record reveal                            |   p95 under 30 ms |
| Record create/update transaction         |   p95 under 50 ms |
| Extension popup initial status           |      Under 150 ms |
| Lock operation                           |      Under 100 ms |

Targets include Noise handshake and per-frame encryption cost; ChaCha20-Poly1305 and X25519 are fast enough that transport encryption must not consume a meaningful share of any budget. Established sessions are reused across requests rather than re-handshaking per request.

Argon2 intentionally consumes memory and time. It is not subject to the normal low-latency target.

## 20.1 Performance strategy

* Decrypt searchable metadata once per unlock.
* Decrypt secret payloads only on demand.
* Keep one in-memory domain index.
* Reuse established Noise sessions; handshake once per connection, not per request.
* Use one serialized writer.
* Use prepared statements.
* Use WAL mode.
* Batch metadata reads during unlock.
* Update indexes incrementally after successful commits.
* Avoid full database scans after each record change.
* Do not use browser-side vault indexing.

---

# 21. Reliability requirements

* A crash during record creation must leave either the previous state or the complete new state.
* A crash during master-password change must not leave an unusable envelope.
* A partially written backup must not be considered valid.
* The daemon must validate the database schema at startup.
* Database integrity errors must never be silently ignored.
* Records with failed AEAD authentication must not be partially displayed.
* The daemon must remain locked after restart.
* Sessions must never survive restart; Noise transport sessions must never survive restart on either end.
* Extension reconnection or re-handshake must not unlock the vault.
* Loss of the daemon transport static key must not lose vault data: clients simply re-pair.

---

# 22. Backup and recovery

## 22.1 Backup format

Backups use a versioned container:

```text
Magic header
Backup format version
Vault identifier
Creation timestamp
KDF and envelope metadata
Consistent encrypted database snapshot
Container authentication data
```

The backup contains only encrypted vault data.

## 22.2 Backup requirements

* Backup creation must use a consistent database snapshot.
* Backup verification must be available without overwriting the current vault.
* Restore must validate format version and container integrity.
* Restore must never merge records in the MVP.
* Restore replaces the current vault only after confirmation.
* Existing vault data is moved to a recovery location until restore verification succeeds.
* Backup data must never pass through the extension channel (section 24.4).

## 22.3 Password relationship

By default, the backup remains protected by the vault’s current master password because it contains the wrapped root key.

A separately password-protected portable export may be added later.

## 22.4 Recovery limitation

If the user forgets the master password and has no unlocked session, the vault cannot be recovered.

The product must communicate this clearly during initialization.

---

# 23. Logging and privacy

## 23.1 Prohibited log data

Logs must never contain:

* Master passwords.
* Passwords.
* API keys.
* API secrets.
* Secure-note bodies.
* Form field values.
* Decrypted metadata.
* Encryption keys.
* Noise session keys or handshake secrets.
* Raw request bodies.
* Full clipboard contents.

## 23.2 Permitted log data

Logs may contain:

* Process startup and shutdown.
* Schema version.
* Generic operation names.
* Redacted record identifiers.
* Error categories.
* Client category.
* Transport handshake success/failure categories.
* Timing measurements.
* Generic security-event codes.

## 23.3 Telemetry

The application must have:

* No analytics.
* No crash upload.
* No remote logging.
* No update check in the daemon.
* No external font, script, or image loading in the extension.

---

# 24. API and message contracts

## 24.1 Transport frame

Every message between a client and the daemon travels inside a Noise transport frame (section 12.4):

```text
┌──────────────┬─────────────────────────────────────┐
│ length (u32) │ Noise transport message (ciphertext)│
└──────────────┴─────────────────────────────────────┘
```

The decrypted Noise payload is the JSON envelope below. `vault-native` relays these frames without the ability to decrypt them. There is no plaintext operational mode.

## 24.2 Request envelope

```json
{
  "protocolVersion": 1,
  "requestId": "0190f56d-...",
  "operation": "records.match",
  "payload": {
    "origin": "https://github.com"
  }
}
```

## 24.3 Response envelope

```json
{
  "protocolVersion": 1,
  "requestId": "0190f56d-...",
  "ok": true,
  "data": {
    "records": []
  }
}
```

## 24.4 Error response

```json
{
  "protocolVersion": 1,
  "requestId": "0190f56d-...",
  "ok": false,
  "error": {
    "code": "VAULT_LOCKED",
    "message": "The vault is locked."
  }
}
```

Internal stack traces must never cross the daemon boundary.

## 24.5 Versioning

* The IPC protocol version is exchanged inside the first encrypted Noise handshake payload, never in plaintext.
* Major protocol mismatch: refuse connection.
* Minor optional fields: ignore when safe.
* Unknown security-critical fields: refuse operation.
* Database version and IPC protocol version are independent.
* Downgrade negotiation to weaker or absent transport encryption is not a protocol feature and must be structurally impossible.

Chrome limits messages returned by a native host to 1 MB, so browser operations must remain small — including Noise framing overhead — and backup data must never pass through the extension channel. ([Chrome for Developers][1])

---

# 25. Go project structure

```text
cmd/
  vaultd/
    main.go

  vault/
    main.go

  vault-native/
    main.go

internal/
  shared/
    domain/
      ids.go
      errors.go
      clock.go

  vault/
    domain/
      vault.go
      state.go
      lock_policy.go
      events.go
      repositories.go

    application/
      create.go
      unlock.go
      lock.go
      change_password.go
      handlers.go

  records/
    domain/
      record.go
      login.go
      secure_note.go
      api_credential.go
      url.go
      repositories.go
      events.go

    application/
      create.go
      update.go
      delete.go
      search.go
      reveal.go
      match.go

  access/
    domain/
      client.go
      capability.go
      session.go
      repositories.go

    application/
      pair.go
      approve.go
      revoke.go
      authorize.go

  backup/
    domain/
      backup.go
      format.go

    application/
      create.go
      verify.go
      restore.go

  security/
    domain/
      event.go
      response.go

    application/
      panic_lock.go
      record_event.go

  infrastructure/
    crypto/
      argon2id.go
      xchacha.go
      hkdf.go
      random.go
      memory.go

    transport/
      noise/
        handshake.go
        frames.go
        rekey.go
        pinning.go

    sqlite/
      database.go
      migrations.go
      vault_repository.go
      record_repository.go
      client_repository.go
      security_repository.go

    ipc/
      socket.go
      peer_credentials_linux.go

    native/
      framing.go
      origin.go
      relay.go

    system/
      paths_linux.go
      permissions.go
      service.go
      dump_protection.go

extension/
  src/
    background/
    content/
    popup/
    messaging/
      noise/
    domain/
  manifest.json

migrations/
tests/
docs/
  threat-model.md
  cryptography.md
  transport-encryption.md
  database-format.md
  native-messaging.md
```

---

# 26. Testing requirements

## 26.1 Domain tests

Test:

* Vault state transitions.
* Record invariants.
* Revision conflicts.
* Capability enforcement.
* Origin matching.
* Session invalidation.
* Master-password-change workflow.

## 26.2 Cryptographic tests

Test:

* Published Argon2 test vectors.
* XChaCha20-Poly1305 round trips.
* HKDF key separation.
* Bit changes in ciphertext.
* Bit changes in nonce.
* Bit changes in AAD.
* Cross-record ciphertext substitution.
* Metadata/secret payload substitution.
* Wrong-password behavior.
* Random-source failures.
* Nonce-length enforcement.

## 26.3 Transport tests

Test:

* Published Noise test vectors for the chosen patterns.
* Go↔TypeScript handshake interoperability.
* Handshake with wrong PSK fails.
* Handshake with mismatched pinned static key fails and is recorded.
* Tampered transport frames fail AEAD and disconnect.
* A man-in-the-middle `vault-native` cannot read or modify extension↔daemon payloads without detection.
* Rekey occurs before nonce exhaustion and after the configured interval.
* Attempted downgrade to plaintext or a weaker pattern is rejected.
* Revoked credential cannot complete a new handshake.

## 26.4 Database tests

Test:

* Migration from every supported version.
* Rollback on failed writes.
* Crash simulation.
* WAL recovery.
* Corrupt rows.
* Invalid types.
* Duplicate record IDs.
* Revision conflicts.
* Interrupted password changes.
* Backup consistency.

## 26.5 Fuzz testing

Fuzz:

* Native Messaging framing.
* Noise handshake messages and transport frames.
* JSON protocol decoding.
* URL normalization.
* Origin matching.
* Record deserialization.
* Backup parsing.
* Database migration parsing.
* Encrypted payload decoding.

Go’s built-in fuzzing is coverage-guided and is specifically useful for finding edge cases that ordinary tests may miss. ([Go][8])

## 26.6 Extension tests

Test:

* Standard login forms.
* Dynamic forms.
* Multiple accounts.
* Subdomains.
* Malicious lookalike domains.
* Cross-origin iframes.
* Locked vault.
* Disconnected native host.
* Revoked client.
* Service-worker restart, including mandatory re-handshake.
* Page navigation during fill.
* Login forms inside shadow DOM where supported.

## 26.7 Security review

Before version `1.0`:

* Independent cryptographic design review, including the Noise transport design.
* Threat-model review.
* Dependency audit (including the Go Noise library and bundled extension crypto).
* Native Messaging permission review.
* Manual test for secret leakage in logs.
* Memory inspection testing.
* Backup and recovery drill.
* Static analysis.
* `govulncheck`.
* Extension content-security-policy review.

---

# 27. Security acceptance criteria

The release cannot ship unless all criteria pass.

1. Copying `vault.db`, `vault.db-wal`, and configuration files reveals no user secret fields.
2. Changing any ciphertext byte causes authentication failure.
3. Changing a nonce causes authentication failure.
4. Moving ciphertext between records causes authentication failure.
5. Wrong master passwords reveal no distinction between invalid password and invalid envelope.
6. No secret appears in logs during automated tests.
7. The daemon does not listen on TCP in production mode.
8. A normal website cannot invoke vault operations.
9. An unapproved Chrome extension cannot use the native host.
10. A revoked client cannot reconnect or complete a new Noise handshake.
11. Locking invalidates all sessions immediately.
12. Daemon restart leaves the vault locked.
13. Chrome cannot export, restore, or destroy the vault.
14. Record secrets are not included in search responses.
15. Password changes do not rewrite every record.
16. Repeated unauthorized requests trigger disconnect and rate limiting.
17. Debug detection never automatically deletes persistent user data.
18. The explicit destroy command cannot run non-interactively by default.
19. All client↔daemon traffic is Noise-encrypted: a passive observer of the Unix socket, the native-messaging pipes, or the relay process sees only ciphertext after the handshake.
20. A tampering or compromised `vault-native` cannot read or alter extension↔daemon payloads without AEAD detection.
21. A client without a valid credential PSK cannot complete a post-pairing handshake, and a registered client presenting an unpinned static key is rejected.

---

# 28. Product acceptance criteria

## CLI

* User can initialize and unlock the vault.
* User can create, list, search, edit, reveal, and remove records.
* Sensitive output is redacted by default.
* User can create and verify a backup.
* User can pair and revoke Chrome.
* User can panic-lock immediately.

## Chrome

* Extension detects whether the native host is installed.
* Extension shows locked and unlocked states, and its transport session state.
* Extension lists exact-domain matches.
* User can fill a selected credential.
* User can save a new login.
* User can update an existing login.
* The extension does not retain passwords after the operation.
* Cross-origin and lookalike-domain tests pass.
* The end-to-end transport tests of section 26.3 pass against a real Chrome instance.

## Performance

* 10,000-record performance targets pass.
* Search remains responsive after repeated updates.
* Extension popup opens without decrypting every secret payload.

---

# 29. Delivery milestones

## Milestone 0 — Security foundation

Deliver:

* Threat model.
* Cryptographic design, including the Noise transport design (`docs/transport-encryption.md`).
* DDD context map.
* Database format.
* IPC decision.
* Security response policy.

No user interface work begins before these documents are reviewed.

## Milestone 1 — Vault daemon

Deliver:

* SQLite migrations.
* Vault initialization.
* Argon2id unlock.
* Key hierarchy.
* Locking.
* Encrypted repository implementation.
* In-memory metadata index.
* Noise transport listener (handshake, framing, rekey, static-key pinning).
* Panic lock.

## Milestone 2 — CLI

Deliver:

* Noise client transport.
* Full record CRUD.
* Search.
* Password generation.
* Master-password change.
* Backup and restore.
* Client administration.
* Doctor command.

## Milestone 3 — Chrome bridge

Deliver:

* Native Messaging host operating as a blind relay.
* Relay-client pairing.
* Capability enforcement.
* Exact extension-origin validation.
* Protocol and relay-tampering tests.

## Milestone 4 — Chrome extension

Deliver:

* Popup.
* Service worker with end-to-end Noise client (TypeScript).
* Extension pairing with pairing-phrase key confirmation.
* Login-form detection.
* Manual fill.
* Save and update.
* Domain validation.
* Locked-state workflows.

## Milestone 5 — Hardening

Deliver:

* Fuzzing (including handshake and frame fuzzing).
* Performance benchmarks.
* Crash testing.
* Dependency review.
* Memory-hardening work.
* Security documentation.
* Independent design review.

## Milestone 6 — Version 1.0

Release requirements:

* No unresolved critical or high-severity security findings.
* Backup and restore verified on a clean machine.
* Signed artifacts.
* Checksums.
* Installation and removal documentation.
* Reproducible release process.
* Chrome extension production ID locked into the native manifest.

---

# 30. Risks and mitigations

## Go memory management

**Risk:** Garbage collection prevents guaranteed key zeroization.

**Mitigation:** Minimize copies, avoid strings, overwrite buffers, use page locking where possible, disable dumps, and clearly document the limitation.

## Same-user malware

**Risk:** Malware running as the same user may access IPC, steal client credentials, or read process memory.

**Mitigation:** Peer credential validation, Noise-authenticated channels bound to client credentials, short sessions, panic locking, least-privilege extension capabilities, and clear threat-model boundaries. Transport encryption removes passive-observation attacks but cannot stop an attacker who steals the credential files; the documentation must state this plainly.

## Extension key storage

**Risk:** The extension's Noise static key and credential live in `chrome.storage.local`, which any same-user process can read.

**Mitigation:** Document as a residual risk; revocation via the CLI invalidates a stolen credential; a stolen credential still cannot decrypt a locked vault. Hardware-backed storage is out of scope for the MVP.

## Noise library dependency

**Risk:** The transport depends on third-party Noise implementations (Go: `flynn/noise`; extension: bundled `@noble` primitives).

**Mitigation:** Pin exact versions, include them in the dependency audit and `govulncheck`, test against published Noise vectors, and cross-test Go↔TypeScript interoperability.

## Browser compromise

**Risk:** A compromised browser may observe filled credentials.

**Mitigation:** Explicit fill, exact-origin validation, no automatic fill, restricted iframe behavior, and no long-lived password cache.

## Destructive anti-tamper behavior

**Risk:** An attacker deliberately triggers self-destruction.

**Mitigation:** Suspicious behavior locks and exits but does not delete persistent data.

## Weak master password

**Risk:** An attacker copies the database and performs offline guessing.

**Mitigation:** Argon2id, strong passphrase guidance, configurable parameters, and future KDF upgrades.

## Database rollback

**Risk:** An attacker replaces the entire database with an older valid copy.

**Mitigation:** The MVP detects modified ciphertext but cannot reliably detect replacement with a complete older authenticated snapshot without an external trusted monotonic state. The product must not claim otherwise.

## Extension update compromise

**Risk:** A malicious extension release could request credentials.

**Mitigation:** Minimal permissions, code review, signed releases, no remote code, reproducible build process, and exact native-host extension ID restrictions.

---

# 31. Final product definition

The MVP is a **local-only encrypted secrets manager** where:

* `vaultd` is the single authority.
* SQLite contains encrypted opaque records.
* The Go CLI provides administration and terminal workflows.
* Chrome communicates through a restricted Native Messaging bridge that relays end-to-end encrypted frames it cannot read.
* Every client↔daemon channel is encrypted with the Noise Protocol Framework, with client credentials bound into the handshake as pre-shared keys.
* Searchable metadata is decrypted only while unlocked.
* Secret payloads are decrypted only when specifically requested.
* Suspicious activity causes immediate locking, not automatic data destruction.
* Key locations and lifecycle are documented, while key values remain unavailable without the master password.
* Domain logic remains independent from SQLite, Chrome, HTTP, and CLI frameworks.
* Mobile, cloud, and P2P synchronization remain outside the product.

[1]: https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging?utm_source=chatgpt.com "Native messaging - Chrome for Developers"
[2]: https://datatracker.ietf.org/doc/rfc9106/?utm_source=chatgpt.com "RFC 9106 - Argon2 Memory-Hard Function for Password ..."
[3]: https://pkg.go.dev/golang.org/x/crypto/chacha20poly1305?utm_source=chatgpt.com "chacha20poly1305 package - golang.org/x/crypto ..."
[4]: https://pkg.go.dev/golang.org/x/crypto/hkdf?utm_source=chatgpt.com "hkdf package - golang.org/x/crypto/hkdf"
[5]: https://sqlite.org/wal.html?utm_source=chatgpt.com "Write-Ahead Logging"
[6]: https://www.sqlite.org/stricttables.html?utm_source=chatgpt.com "STRICT Tables"
[7]: https://pkg.go.dev/crypto/hmac?utm_source=chatgpt.com "crypto/hmac"
[8]: https://go.dev/doc/security/fuzz/?utm_source=chatgpt.com "Go Fuzzing"
