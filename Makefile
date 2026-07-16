GO_BIN := vaultd vault vault-native

.PHONY: all build devd dev-ext dev-desktop extension test test-go test-ext cover fuzz vet sqlc vectors clean

all: build extension

build:
	go build ./cmd/...

# Development: run each component live (run devd first, it owns the socket).
devd:
	go run ./cmd/vaultd

dev-ext:
	cd extension && pnpm dev

dev-desktop:
	cd desktop && npm start

extension:
	cd extension && pnpm install && pnpm build

test: test-go test-ext

test-go:
	go test ./... -timeout 300s

test-ext:
	cd extension && ./node_modules/.bin/vitest run

cover:
	go test ./... -timeout 300s -cover

fuzz:
	go test -fuzz='^FuzzParseOrigin$$' -fuzztime=30s -run='^$$' ./internal/records/domain
	go test -fuzz='^FuzzDecodeMetadata$$' -fuzztime=30s -run='^$$' ./internal/records/application
	go test -fuzz='^FuzzDecodeSecret$$' -fuzztime=30s -run='^$$' ./internal/records/application
	go test -fuzz='^FuzzParseContainer$$' -fuzztime=30s -run='^$$' ./internal/backup/application
	go test -fuzz='^FuzzReadNativeMessage$$' -fuzztime=30s -run='^$$' ./internal/native
	go test -fuzz='^FuzzReadFrame$$' -fuzztime=30s -run='^$$' ./internal/infrastructure/transport/noise
	go test -fuzz='^FuzzServerHandshakeHello$$' -fuzztime=30s -run='^$$' ./internal/infrastructure/transport/noise

vet:
	go vet ./...
	cd extension && ./node_modules/.bin/tsc --noEmit

sqlc:
	sqlc generate

# Go is the source of truth for the wire format. The extension and desktop each
# carry their own TS Noise implementation and must test against byte-identical
# vectors, so generate once and copy — regenerating per target would let the two
# drift if the generator ever stopped being deterministic.
vectors:
	go run ./tools/noisevectors extension/src/noise/testdata/vectors.json
	cp extension/src/noise/testdata/vectors.json desktop/src/main/testdata/vectors.json

clean:
	rm -f $(GO_BIN)
	rm -rf extension/dist
