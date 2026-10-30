GO ?= go
BINARY := garmin-mcp$(shell $(GO) env GOEXE)

.PHONY: help build run test test-race fmt format-check vet lint cross-build check clean

help:
	@printf '%s\n' \
		'build        Build the garmin-mcp executable' \
		'run          Run the program (use ARGS="auth" for arguments)' \
		'test         Run all tests' \
		'test-race    Run all tests with the race detector' \
		'fmt          Format Go source files' \
		'lint         Check formatting and run go vet' \
		'vet          Run go vet' \
		'cross-build  Verify all six release targets' \
		'check        Run the CI checks' \
		'clean        Remove the built executable'

build:
	$(GO) build -o $(BINARY) ./cmd/garmin-mcp

run:
	@$(GO) run ./cmd/garmin-mcp $(ARGS)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

fmt:
	$(GO) fmt ./...

format-check:
	@set -e; unformatted="$$(gofmt -l cmd internal)"; \
		if [ -n "$$unformatted" ]; then \
			printf 'Go files need formatting:\n%s\n' "$$unformatted"; exit 1; \
		fi

vet:
	$(GO) vet ./...

lint: format-check vet

cross-build:
	@for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		CGO_ENABLED=0 GOOS=$${target%/*} GOARCH=$${target#*/} $(GO) build -o /dev/null ./cmd/garmin-mcp || exit 1; \
	done

check: lint test-race cross-build

clean:
	rm -f $(BINARY)
