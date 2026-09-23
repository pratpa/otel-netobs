# Root Makefile. Each component is its own Go module; these targets fan out
# across every module found under extension/ and receiver/.

GOCMD ?= go
MODULES := $(shell find extension receiver -name go.mod -exec dirname {} \; 2>/dev/null | sort)

.DEFAULT_GOAL := test

.PHONY: modules
modules:
	@echo "$(MODULES)" | tr ' ' '\n'

.PHONY: build
build:
	@set -e; for m in $(MODULES); do \
		echo "==> build $$m"; \
		(cd $$m && $(GOCMD) build ./...); \
	done

.PHONY: test
test:
	@set -e; for m in $(MODULES); do \
		echo "==> test $$m"; \
		(cd $$m && $(GOCMD) test -race -count=1 ./...); \
	done

.PHONY: cover
cover:
	@set -e; for m in $(MODULES); do \
		echo "==> cover $$m"; \
		(cd $$m && $(GOCMD) test -coverprofile=coverage.txt -covermode=atomic ./... && \
			$(GOCMD) tool cover -func=coverage.txt | tail -1); \
	done

.PHONY: vet
vet:
	@set -e; for m in $(MODULES); do \
		echo "==> vet $$m"; \
		(cd $$m && $(GOCMD) vet ./...); \
	done

.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found: https://golangci-lint.run/welcome/install/"; exit 1; }
	@set -e; for m in $(MODULES); do \
		echo "==> lint $$m"; \
		(cd $$m && golangci-lint run ./...); \
	done

.PHONY: tidy
tidy:
	@set -e; for m in $(MODULES); do \
		echo "==> tidy $$m"; \
		(cd $$m && $(GOCMD) mod tidy); \
	done

.PHONY: generate
generate:
	@set -e; for m in $(MODULES); do \
		echo "==> generate $$m"; \
		(cd $$m && $(GOCMD) generate ./...); \
	done

.PHONY: fmt
fmt:
	@set -e; for m in $(MODULES); do \
		(cd $$m && $(GOCMD) fmt ./...); \
	done

.PHONY: workspace
workspace:
	@rm -f go.work
	$(GOCMD) work init
	@for m in $(MODULES); do $(GOCMD) work use ./$$m; done
	@echo "go.work written for: $(MODULES)"
