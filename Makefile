SHELL := /bin/sh

MODULE  := github.com/herder-labs/cpe-labs
BIN_DIR := bin
BIN     := $(BIN_DIR)/cpe-sim

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)

LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) \
           -X $(MODULE)/internal/version.Commit=$(COMMIT) \
           -X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: all build test test-race acceptance acceptance-update lint fmt vet tidy clean proto-gen

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/cpe-sim

test:
	go test ./...

test-race:
	go test -race ./...

# acceptance runs the wire-format acceptance suite (build-tag gated).
# Hermetic: embedded mochi-mqtt + httptest, no docker, no external services.
# Builds cpe-sim once per invocation and exec's it per scenario.
acceptance:
	go test -tags=acceptance -timeout=10m ./acceptance/...

# acceptance-update regenerates the golden fixtures. Inspect the
# resulting diff before committing. The -args separator routes -update
# to the test binary (it's a custom flag registered by the harness, not
# a built-in go test flag).
acceptance-update:
	go test -tags=acceptance -timeout=10m ./acceptance/... -args -update

lint:
	golangci-lint run

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html

proto-gen:
	@command -v protoc >/dev/null 2>&1 || { echo "protoc not installed"; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { echo "protoc-gen-go not installed (go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)"; exit 1; }
	protoc --go_out=. --go_opt=paths=source_relative \
	    internal/usp/codec/proto/usp_msg.proto \
	    internal/usp/codec/proto/usp_record.proto
