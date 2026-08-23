# Raspberry Mining Monitor

BINARY  := rmm
PKG     := ./cmd/rmm
# The semantic version is hand-maintained in cmd/rmm/main.go and is the single
# source of truth. Bump it there on every change (patch for fixes, minor for
# features). The build reads it back rather than deriving a git description, so
# the number shown in the UI matches the code. GITREV is embedded separately for
# traceability but is not shown in the UI.
VERSION := $(shell sed -nE 's/^var version = "([^"]+)".*/\1/p' cmd/rmm/main.go)
GITREV  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.gitRev=$(GITREV)

.PHONY: all build demo test race vet fmt lint pi clean

all: fmt vet test build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

# Run the fully simulated dashboard. Needs no miners and no internet.
demo:
	go run $(PKG) --demo --addr 127.0.0.1:8080

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w cmd internal web

# Cross-compile for the Raspberry Pi 4 (64-bit Raspberry Pi OS).
pi:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-arm64 $(PKG)

clean:
	rm -f $(BINARY) $(BINARY)-linux-arm64
