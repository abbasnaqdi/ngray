VERSION ?= 0.3.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%d)
PKG     := github.com/abbasnaqdi/ng-ray/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build install test vet fmt lint check e2e dist release package clean

# Platforms shipped in a release. linux/amd64 is what `ngray update --self` pulls.
PLATFORMS := linux/amd64 linux/arm64

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

# Cross-compile every release platform into dist/ with a checksums file.
# Asset names (ngray-<os>-<arch>) match the self-updater's lookup regex.
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

# Tag v$(VERSION) and publish a GitHub release with the dist/ assets.
# Requires gh (https://cli.github.com) and a clean tree on the release commit.
release: dist
	@test -z "$$(git status --porcelain)" || { echo "tree dirty; commit first"; exit 1; }
	@command -v gh >/dev/null 2>&1 || { echo "install gh: https://cli.github.com"; exit 1; }
	git tag -a v$(VERSION) -m "ng-ray v$(VERSION)"
	git push origin v$(VERSION)
	gh release create v$(VERSION) dist/* \
		--title "ng-ray v$(VERSION)" --generate-notes

# Build a ready-to-run package for the current CPU in pkg/ngray-<version>/.
# Contains: the binary + config.example.yml + snis/candidates.sni.
# Copy the folder to any Linux server and run: cp config.example.yml config.yml && sudo ./ngray
package: build
	$(eval PKGDIR := pkg/ngray-$(VERSION))
	@rm -rf pkg && mkdir -p $(PKGDIR)/snis
	@cp ngray           $(PKGDIR)/ngray
	@cp config.example.yml $(PKGDIR)/config.example.yml
	@cp snis/candidates.sni $(PKGDIR)/snis/candidates.sni
	@echo "package ready: $(PKGDIR)/"
	@ls -lh $(PKGDIR)/

clean:
	rm -f ngray
	rm -rf dist pkg
