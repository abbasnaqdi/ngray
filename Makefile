VERSION ?= 0.1.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%d)
PKG     := github.com/abbasnaqdi/ng-ray/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build install test vet fmt clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o ngray ./cmd/ngray

install: build
	install -m 0755 ngray /usr/local/bin/ngray

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f ngray
