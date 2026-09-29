GO_BIN := vaultd vault vault-native

.PHONY: all build devd dev-ext dev-desktop extension crx test test-go test-ext cover fuzz lint vet sqlc vectors clean

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

# The extension build is dev unless ALBEAR_ENV / ALBEAR_VERSION say otherwise,
# e.g. ALBEAR_VERSION=v1.4.2 make extension (see extension/build/target.ts).
extension:
	cd extension && pnpm install && pnpm build

# Sign the built extension into a CRX3 package for `vault install`. The key
# defaults to the committed dev key; a prod build passes its key, e.g.
#   ALBEAR_VERSION=v1.4.2 ALBEAR_CRX_KEY=/path/prod.pem make crx
ALBEAR_CRX_KEY ?= extension/keys/dev.pem
CRX_OUT ?= extension/albear.crx

crx: extension
	go run ./tools/crxpack -key $(ALBEAR_CRX_KEY) -out $(CRX_OUT) extension/dist

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

lint:
	golangci-lint run ./...

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
	rm -rf extension/dist extension/albear.crx
