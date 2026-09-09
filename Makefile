# Slopscale Makefile
# Modern Makefile following best practices

# Version calculation
VERSION ?= $(shell git describe --always --tags --dirty)

# Build configuration
GOOS ?= $(shell uname | tr '[:upper:]' '[:lower:]')

# SQLite is mattn/go-sqlite3, compiled from C by cgo with the flags in
# sqlite.cflags (hardening and performance defines). Every go invocation
# below inherits them; a build without them logs a warning at startup.
export CGO_ENABLED := 1
export CGO_CFLAGS := $(shell grep -v '^\#' sqlite.cflags | tr '\n' ' ')
ifeq ($(filter $(GOOS), openbsd netbsd solaris plan9), )
	PIE_FLAGS = -buildmode=pie
endif

# Tool availability check with nix warning
define check_tool
	@command -v $(1) >/dev/null 2>&1 || { \
		echo "Warning: $(1) not found. Run 'nix develop' to ensure all dependencies are available."; \
		exit 1; \
	}
endef

# Source file collections using shell find for better performance
GO_SOURCES := $(shell find . -name '*.go' -not -path './gen/*' -not -path './vendor/*')
MARKUP_SOURCES := $(shell find . \( -name '*.md' -o -name '*.yaml' -o -name '*.yml' -o -name '*.ts' -o -name '*.js' -o -name '*.html' -o -name '*.css' -o -name '*.scss' -o -name '*.sass' \) -not -path './gen/*' -not -path './vendor/*' -not -path './node_modules/*' -not -path './web/*' -not -path './docs/*' -not -path './.claude/*')
# oxfmt is pinned in web/bun.lock and formats the repo's markup and config
# files as well as the console (see .oxfmtrc.json for the root scope).
OXFMT := web/node_modules/.bin/oxfmt
WEB_SOURCES := $(shell find web -type f -not -path 'web/node_modules/*' -not -path 'web/codegen/node_modules/*' -not -path 'web/dist/*')

# Default target
.PHONY: all
all: lint test build

# Dependency checking
.PHONY: check-deps
check-deps:
	$(call check_tool,go)
	$(call check_tool,golangci-lint)

.PHONY: check-web-deps
check-web-deps:
	$(call check_tool,bun)

# Build targets
.PHONY: build
build: check-deps $(GO_SOURCES) go.mod go.sum
	@echo "Building slopscale..."
	go build $(PIE_FLAGS) -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o slopscale ./cmd/slopscale

# Admin console. The React app in web/ is built by bun and embedded into
# the binary by web/embed.go, so `make web` must run before `make build`
# for the console to ship; without it the server serves a placeholder at
# /admin/. Runtime dependencies are pinned by web/bun.lock.
.PHONY: web-deps
web-deps: check-web-deps web/package.json web/bun.lock
	@echo "Installing console dependencies..."
	cd web && bun install --frozen-lockfile

.PHONY: web
web: web-deps $(WEB_SOURCES)
	@echo "Building admin console..."
	cd web && bun run build

# Regenerate the console's API types from the served OpenAPI document. The
# spec is committed under gen/openapi so the TypeScript output is
# reproducible without a Go toolchain.
.PHONY: web-generate
web-generate: web-deps
	@echo "Generating console API types..."
	go run ./cmd/gen-openapi -out gen/openapi/v1.yaml
	cd web && bun run generate && bun run fmt

.PHONY: lint-web
lint-web: web-deps
	@echo "Checking admin console..."
	cd web && bun run typecheck && bun run lint && bun run fmt:check

.PHONY: fmt-web
fmt-web: web-deps
	@echo "Formatting admin console..."
	cd web && bun run fmt && bun run lint:fix

.PHONY: test-web
test-web: web-deps
	@echo "Running console tests..."
	cd web && bun run test

.PHONY: docs-deps
docs-deps:
	cd docs && bun install --frozen-lockfile --silent

.PHONY: docs
docs: docs-deps
	@echo "Building documentation..."
	cd docs && bun run build && bun run validate

.PHONY: docs-dev
docs-dev: docs-deps
	cd docs && bun run dev

# End-to-end: builds the console, starts a real server through cmd/dev
# with its mock identity provider, and signs in through a browser.
.PHONY: test-e2e
test-e2e: web-deps
	@echo "Running console end-to-end tests..."
	cd web && bun run e2e

# Test targets
.PHONY: test
test: check-deps $(GO_SOURCES) go.mod go.sum
	@echo "Running Go tests..."
	go test -race -short -timeout 30m ./...


# Formatting targets
.PHONY: fmt
fmt: fmt-go fmt-markup fmt-web

.PHONY: fmt-go
fmt-go: check-deps $(GO_SOURCES)
	@echo "Formatting Go code..."
	go fix ./...
	golangci-lint fmt
	golangci-lint run --fix

.PHONY: fmt-markup
fmt-markup: web-deps $(MARKUP_SOURCES)
	@echo "Formatting markup and config files..."
	$(OXFMT) --config .oxfmtrc.json .

# Linting targets. `lint` is the full gate: every golangci-lint linter and
# formatter (see .golangci.yaml), go vet, a tidy/verified module graph, and a
# vulnerability scan of the dependency tree.
.PHONY: lint
lint: lint-go lint-mod lint-vuln lint-web lint-markup

.PHONY: lint-markup
lint-markup: web-deps $(MARKUP_SOURCES)
	@echo "Checking markup and config formatting..."
	$(OXFMT) --config .oxfmtrc.json --check .

.PHONY: lint-go
lint-go: check-deps $(GO_SOURCES) go.mod go.sum
	@echo "Linting Go code..."
	golangci-lint run --timeout 10m
	go vet -tags integration ./...

.PHONY: lint-mod
lint-mod: go.mod go.sum
	@echo "Checking module graph..."
	go mod tidy -diff
	go mod verify

.PHONY: lint-vuln
lint-vuln: go.mod go.sum
	@echo "Scanning dependencies for known vulnerabilities..."
	go tool govulncheck ./...

# Code generation
.PHONY: generate
generate: check-deps
	@echo "Generating code..."
	go generate ./...
	$(MAKE) client
	$(MAKE) web-generate

# Emit the OpenAPI spec on demand. The server serves it live at /openapi.yaml;
# this is for external consumers or inspection and is not committed.
.PHONY: openapi
openapi:
	@echo "Emitting OpenAPI spec from code..."
	go run ./cmd/gen-openapi

# Generate the strongly-typed Go HTTP clients (v1 and v2) from the served
# OpenAPI 3.1 documents using oapi-codegen. Pinned so the committed clients
# are reproducible.
.PHONY: client
client:
	@echo "Generating API clients..."
	@tmp=$$(mktemp -t slopscale-openapi-3.1.XXXXXX.yaml); \
	go run ./cmd/gen-openapi -out "$$tmp" && \
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
		-generate types,client -package clientv1 -o gen/client/v1/client.gen.go "$$tmp" && \
	go run ./cmd/gen-openapi -api v2 -out "$$tmp" && \
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
		-generate types,client -package clientv2 -o gen/client/v2/client.gen.go "$$tmp"; \
	status=$$?; rm -f "$$tmp"; exit $$status

# Clean targets
.PHONY: clean
clean:
	rm -rf slopscale gen/client web/dist/assets web/dist/index.html

# Development workflow
.PHONY: dev
dev: fmt lint test build

# Start a local slopscale dev server (use mts to add nodes)
.PHONY: dev-server
dev-server:
	go run ./cmd/dev

# Help target
.PHONY: help
help:
	@echo "Slopscale Development Makefile"
	@echo ""
	@echo "Main targets:"
	@echo "  all          - Run lint, test, and build (default)"
	@echo "  build        - Build slopscale binary"
	@echo "  test         - Run Go tests"
	@echo "  fmt          - Format all code (Go, docs, markup)"
	@echo "  lint         - Lint all code (golangci-lint, vet, mod tidy, govulncheck)"
	@echo "  generate     - Generate code (go generate + client + web-generate)"
	@echo "  dev          - Full development workflow (fmt + lint + test + build)"
	@echo "  clean        - Clean build artifacts"
	@echo ""
	@echo "Specific targets:"
	@echo "  fmt-go       - Format Go code only"
	@echo "  fmt-markup   - Format markup and config files only (oxfmt)"
	@echo "  lint-go      - Lint Go code only"
	@echo "  web          - Build the admin console into web/dist (embedded by build)"
	@echo "  web-generate - Regenerate the console's API types from the OpenAPI spec"
	@echo "  lint-web     - Typecheck, lint and format-check the admin console"
	@echo "  docs         - Build the documentation site and validate its links"
	@echo "  docs-dev     - Serve the documentation with hot reload"
	@echo "  test-web     - Run the admin console's browser tests"
	@echo "  test-e2e     - Sign in to a real server through the browser (mock identity provider)"
	@echo ""
	@echo "Dependencies:"
	@echo "  check-deps   - Verify required tools are available"
	@echo ""
	@echo "Note: If not running in a nix shell, ensure dependencies are available:"
	@echo "  nix develop"
