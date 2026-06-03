# Version is read from version.go — single source of truth.
# Bump the string there; everything else follows automatically.
VERSION := $(shell grep -oE '"[0-9]+\.[0-9]+\.[0-9][^"]*"' internal/version/version.go | tr -d '"')
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%d)
PKG     := github.com/abbasnaqdi/ng-ray/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build install test vet fmt lint check e2e dist release package clean

PREFIX ?= /usr/local
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

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || go vet ./...

check: fmt vet test

e2e:
	bash scripts/e2e.sh

# Cross-compile release binaries (used for GitHub releases).
PLATFORMS := linux/amd64 linux/arm64

dist: check
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; out=dist/ngray-$$os-$$arch; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "$(LDFLAGS)" -o $$out ./cmd/ngray; \
	done
	@cd dist && sha256sum ngray-* > SHA256SUMS
	@echo "dist ready:" && ls -1 dist

release: dist
	@test -z "$$(git status --porcelain)" || { echo "tree dirty; commit first"; exit 1; }
	@command -v gh >/dev/null 2>&1 || { echo "install gh: https://cli.github.com"; exit 1; }
	git tag -a v$(VERSION) -m "ng-ray v$(VERSION)"
	git push origin v$(VERSION)
	gh release create v$(VERSION) dist/* --title "ng-ray v$(VERSION)" --generate-notes

# Build a ready-to-run local package in pkg/release/.
# config.example.yml ships as a reference (NOT as config.yml): with no config.yml
# present, `ngray server setup` auto-populates the full SNI set from the server's
# own vantage, and that generated config.yml then survives future deploys.
# Usage: make package   (version is read automatically from version.go)
package: build
	@rm -rf pkg/release && mkdir -p pkg/release/snis
	@cp ngray              pkg/release/ngray
	@cp config.example.yml pkg/release/config.example.yml
	@cp snis/candidates.sni pkg/release/snis/candidates.sni
	@echo "✓ pkg/release/  (ngray v$(VERSION), $(DATE))"
	@ls -lh pkg/release/ngray

clean:
	rm -f ngray
	rm -rf dist pkg
