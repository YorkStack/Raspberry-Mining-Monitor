# Raspberry Mining Monitor

BINARY  := rmm
PKG     := ./cmd/rmm
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

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
