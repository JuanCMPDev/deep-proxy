BINARY   := bin/deep-proxy
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
MODULE   := github.com/JuanCMPDev/deep-proxy
LDFLAGS  := -s -w \
            -X $(MODULE)/internal/cli.version=$(VERSION) \
            -X $(MODULE)/internal/cli.commit=$(COMMIT) \
            -X $(MODULE)/internal/cli.date=$(DATE)

ifeq ($(OS),Windows_NT)
  BINARY := bin/deep-proxy.exe
endif

.PHONY: build test lint vet vuln snapshot clean

## build: compile the binary with version metadata
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/deep-proxy

## test: run all tests
test:
	go test -count=1 ./...

## lint: run golangci-lint (requires golangci-lint in PATH)
lint:
	golangci-lint run --timeout=3m

## vet: run go vet
vet:
	go vet ./...

## vuln: check for known vulnerabilities (requires govulncheck)
vuln:
	govulncheck ./...

## snapshot: build a local release snapshot without publishing (requires goreleaser)
snapshot:
	goreleaser release --snapshot --clean

## clean: remove build artifacts
clean:
	rm -rf bin/ dist/

## help: print this help
help:
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/## /  /'
