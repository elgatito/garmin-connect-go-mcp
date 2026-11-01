GO ?= go
VERSION ?= dev
BINARY := garmin-mcp$(shell $(GO) env GOEXE)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Release targets; README download links name these archives.
PLATFORMS := windows/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: help build run test test-race fmt format-check vet lint cross-build check dist clean

help:
	@printf '%s\n' \
		'build        Build garmin-mcp for this machine (VERSION=v1.2.3 to stamp a version)' \
		'run          Run the program (ARGS="auth" to pass arguments)' \
		'test         Run all tests' \
		'test-race    Run all tests with the race detector' \
		'fmt          Format Go source files' \
		'lint         Check formatting and run go vet' \
		'cross-build  Check that every release platform compiles' \
		'check        Run the CI checks (lint, test-race, cross-build)' \
		'dist         Build release archives and checksums.txt into dist/' \
		'clean        Remove the built executable and dist/'

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/garmin-mcp

run:
	@$(GO) run ./cmd/garmin-mcp $(ARGS)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

fmt:
	$(GO) fmt ./...

format-check:
	@unformatted="$$(gofmt -l cmd internal)"; \
		if [ -n "$$unformatted" ]; then \
			printf 'Go files need formatting (run make fmt):\n%s\n' "$$unformatted"; exit 1; \
		fi

vet:
	$(GO) vet ./...

lint: format-check vet

cross-build:
	@for t in $(PLATFORMS); do \
		CGO_ENABLED=0 GOOS=$${t%/*} GOARCH=$${t#*/} $(GO) build -o /dev/null ./cmd/garmin-mcp || exit 1; \
	done

check: lint test-race cross-build

dist:
	@set -eu; rm -rf dist; \
	for t in $(PLATFORMS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
		mkdir -p dist/stage; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "dist/stage/garmin-mcp$$ext" ./cmd/garmin-mcp; \
		cp README.md LICENSE dist/stage/; \
		name="garmin-mcp_$${os}_$${arch}"; \
		if [ "$$os" = windows ]; then zip -qj "dist/$$name.zip" dist/stage/*; \
		else tar -C dist/stage -czf "dist/$$name.tar.gz" .; fi; \
		rm -rf dist/stage; \
	done; \
	cd dist && sha256sum *.tar.gz *.zip > checksums.txt

clean:
	rm -rf $(BINARY) dist
