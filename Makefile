SHELL := /bin/sh

BIN_DIR := bin
BINARY := $(BIN_DIR)/nexusgate

VERSION ?= 1.0.0
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
TARGET ?= termux/arm64

LDFLAGS := -s -w \
	-X nexusgate/pkg/cli.Version=$(VERSION) \
	-X nexusgate/pkg/cli.GitCommit=$(GIT_COMMIT) \
	-X nexusgate/pkg/cli.BuildDate=$(BUILD_DATE) \
	-X nexusgate/pkg/cli.Target=$(TARGET)

GCFLAGS := -trimpath

.PHONY: all build build-termux test bench clean validate verify

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build $(GCFLAGS) -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/nexusgate
	@ls -lh $(BINARY)

build-termux:
	@mkdir -p $(BIN_DIR)
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build $(GCFLAGS) -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/nexusgate
	@ls -lh $(BINARY)

test:
	go test -v ./...

bench:
	go test -bench=. -benchmem -run=^$ ./test/stress/...

validate: build
	./$(BINARY) validate -c ./nexusgate.example.yaml

clean:
	rm -rf $(BIN_DIR)
