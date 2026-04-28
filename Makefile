GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod

ROOT_EXAMPLES := ./examples/http_client ./examples/background_worker
NESTED_MODULES := integrations/grpc integrations/otel examples/otel examples/prometheus

.PHONY: test vet examples-check modules-check ci-check docs-reference docs-build docs-claims docs

test:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./... -coverprofile=coverage.out -covermode=atomic

vet:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...

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

ci-check: test vet examples-check modules-check docs-reference docs-claims
	git diff --exit-code docs/reference

# Generate reference docs from source

docs-reference:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run scripts/gen_reference.go

# Build docs with strict mode (matches CI)
docs-build:
	mkdocs build --strict

# Verify docs Claim-ID markers against the claims ledger
docs-claims:
	@mkdir -p $(GOCACHE) $(GOMODCACHE)
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./scripts/claims_check

# Generate reference docs and build the site
docs: docs-reference docs-build
