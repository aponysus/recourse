GOCACHE ?= $(CURDIR)/.cache/go-build

.PHONY: docs-reference docs-build docs
# Generate reference docs from source

docs-reference:
	@mkdir -p $(GOCACHE)
	GOCACHE=$(GOCACHE) go run scripts/gen_reference.go

# Build docs with strict mode (matches CI)
docs-build:
	mkdocs build --strict

# Generate reference docs and build the site
docs: docs-reference docs-build
