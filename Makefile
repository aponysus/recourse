GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod

ROOT_EXAMPLES := ./examples/http_client ./examples/background_worker ./examples/migration_backoff
NESTED_MODULES := integrations/grpc integrations/otel examples/otel examples/prometheus
GO_MODULES := . $(NESTED_MODULES)
STATICCHECK ?= go run honnef.co/go/tools/cmd/staticcheck@latest
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: test vet staticcheck govulncheck security-check examples-check modules-check ci-check docs-reference docs-reference-check docs-build docs-claims docs

test:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./... -coverprofile=coverage.out -covermode=atomic

vet:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...

staticcheck:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	@set -e; \
	for dir in $(GO_MODULES); do \
		echo "==> staticcheck $$dir"; \
		(cd $$dir && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(STATICCHECK) ./...); \
	done

govulncheck:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	@set -e; \
	for dir in $(GO_MODULES); do \
		echo "==> govulncheck $$dir"; \
		(cd $$dir && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(GOVULNCHECK) ./...); \
	done

security-check: govulncheck

examples-check:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build $(ROOT_EXAMPLES)

modules-check:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	@set -e; \
	for dir in $(NESTED_MODULES); do \
		echo "==> checking $$dir"; \
		(cd $$dir && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...); \
	done

ci-check: test vet examples-check modules-check docs-reference-check docs-claims docs-build

# Generate reference docs from source

docs-reference:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run scripts/gen_reference.go

docs-reference-check:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	@tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp -R docs/reference "$$tmp/reference"; \
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run scripts/gen_reference.go; \
	diff -ru "$$tmp/reference" docs/reference

# Build docs with strict mode (matches CI)
docs-build:
	mkdocs build --strict

# Verify docs Claim-ID markers against the claims ledger
docs-claims:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./scripts/claims_check

# Generate reference docs and build the site
docs: docs-reference docs-build
