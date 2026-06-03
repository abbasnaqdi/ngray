VERSION ?= 0.2.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%d)
PKG     := github.com/abbasnaqdi/ng-ray/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build install test vet fmt lint check e2e clean

PREFIX ?= /usr/local
# Elevate with sudo only when the target dir isn't writable.
SUDO := $(shell [ -w $(PREFIX)/bin ] 2>/dev/null && echo "" || echo sudo)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o ngray ./cmd/ngray

install: build
	$(SUDO) install -m 0755 ngray $(PREFIX)/bin/ngray

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

# Requires golangci-lint (https://golangci-lint.run). `go vet` is the fallback.
lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || go vet ./...

# Build + vet + test gate run before every commit.
check: fmt vet test

# Real install inside a throwaway Ubuntu container (needs Docker).
e2e:
	bash scripts/e2e.sh

clean:
	rm -f ngray
