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
	@command -v go >/dev/null 2>&1 || { \
		echo "error: Go is not installed."; \
		echo "  curl -fsSL https://go.dev/dl/go1.24.0.$(shell uname -s | tr A-Z a-z)-$(shell uname -m | sed s/x86_64/amd64/).tar.gz | sudo tar -C /usr/local -xz"; \
		echo "  export PATH=\$$PATH:/usr/local/go/bin"; \
		exit 1; }
	# GOTOOLCHAIN=local: build with the installed Go only. go.mod needs ≥1.24;
	# a missing newer toolchain must fail fast, never hang on a download.
	CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= go build -mod=vendor -ldflags "$(LDFLAGS)" -o ngray ./cmd/ngray

install: build
	$(SUDO) install -m 0755 ngray $(PREFIX)/bin/ngray
	# Refresh the SNI candidate pool alongside the binary — servers installed
	# with an older, smaller pool otherwise keep testing stale SNIs forever.
	if [ -f snis/candidates.sni ]; then \
		$(SUDO) install -d -m 0750 /etc/ngray/snis; \
		$(SUDO) install -m 0640 snis/candidates.sni /etc/ngray/snis/candidates.sni; \
	fi

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
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

dist: check
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		out=dist/ngray-$$os-$$arch$$ext; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "$(LDFLAGS)" -o $$out ./cmd/ngray; \
	done
	@cp snis/candidates.sni dist/
	@cd dist && sha256sum ngray-* candidates.sni > SHA256SUMS
	@echo "dist ready:" && ls -1 dist

release: dist
	@test -z "$$(git status --porcelain)" || { echo "tree dirty; commit first"; exit 1; }
	@command -v gh >/dev/null 2>&1 || { echo "install gh: https://cli.github.com"; exit 1; }
	git tag -a v$(VERSION)-$(COMMIT) -m "ng-ray v$(VERSION)-$(COMMIT)"
	git push origin v$(VERSION)-$(COMMIT)
	gh release create v$(VERSION)-$(COMMIT) dist/* --title "ngray v$(VERSION)-$(COMMIT)" --generate-notes --latest

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
