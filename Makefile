GO_BIN := vaultd vault vault-native

.PHONY: all build extension test test-go test-ext fuzz vet sqlc vectors clean

all: build extension

build:
	go build ./cmd/...

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

vectors:
	go run ./tools/noisevectors extension/src/noise/testdata/vectors.json

clean:
	rm -f $(GO_BIN)
	rm -rf extension/dist
