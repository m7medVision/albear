# albear — البير

A local-only encrypted secrets manager: a Go daemon (`vaultd`), a CLI
(`vault`), and a Chrome extension connected through a blind native-messaging
relay (`vault-native`). Every client↔daemon channel is end-to-end encrypted
with the Noise Protocol Framework — including the browser path, where the
relay only ever sees ciphertext.

No cloud. No telemetry. No network listeners. See `docs/PRD.md` for the full
product definition and `docs/{threat-model,cryptography,database-format,native-messaging}.md`
for the security design.

## Build

```sh
go build ./cmd/...            # vaultd, vault, vault-native
cd extension && pnpm install && pnpm build   # crxjs → extension/dist
```

## Run

```sh
./vaultd &                    # serves $XDG_RUNTIME_DIR/albear/vault.sock
./vault init                  # create the vault (no recovery without backup!)
./vault unlock
./vault add login --name GitHub --username you --url https://github.com --generate
./vault list
./vault show github --reveal
./vault backup create ~/albear.abk
```

## Chrome extension

1. Build: `go build ./cmd/...` and `cd extension && pnpm build`.
2. Install the native host for Chrome: `./vault install chrome`.
3. Open `chrome://extensions`, enable Developer mode, choose *Load unpacked*,
   and select the printed `extension/dist` path.
4. Open the popup → *Pair with vaultd* → run `vault clients approve` in a
   terminal → confirm the phrases match on both sides.
5. Unlock from the popup; matching logins for the current site appear; *Fill*
   requires an explicit click and an exact origin match, verified daemon-side.

## Tests

```sh
go test ./...                              # all Go suites (unit + socket integration)
cd extension && pnpm test                  # TS: Noise Go-interop vectors, transport, forms
go run ./tools/noisevectors extension/src/noise/testdata/vectors.json  # regenerate interop vectors
```

Fuzz targets (run with `go test -fuzz=<Name> -fuzztime=30s -run='^$' <pkg>`):
`FuzzParseOrigin`, `FuzzDecodeMetadata`, `FuzzDecodeSecret`,
`FuzzParseContainer`, `FuzzReadNativeMessage`, `FuzzReadFrame`,
`FuzzServerHandshakeHello`.

## Architecture invariants

- Only `vaultd` opens the database; plaintext never touches disk.
- CQRS with sqlc: `sql/commands.sql` (writes) and `sql/queries.sql` (reads)
  are the only SQL in the project — simple single-statement queries.
- Domain packages import no SQL, HTTP, Chrome, or CLI machinery.
- Sessions are memory-only, epoch-bound, and die on lock or restart.
- Suspicious activity locks the vault; nothing automatic ever deletes it.
