GOCMD ?= go

.DEFAULT_GOAL := test

.PHONY: build
build:
	$(GOCMD) build ./...

.PHONY: test
test:
	$(GOCMD) test -race -count=1 ./...

.PHONY: cover
cover:
	$(GOCMD) test -coverprofile=coverage.txt -covermode=atomic ./...
	$(GOCMD) tool cover -func=coverage.txt | tail -1

.PHONY: vet
vet:
	$(GOCMD) vet ./...

.PHONY: tidy
tidy:
	$(GOCMD) mod tidy

.PHONY: generate
generate:
	$(GOCMD) generate ./...

.PHONY: fmt
fmt:
	$(GOCMD) fmt ./...
