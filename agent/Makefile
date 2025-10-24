GO ?= go
GOLANGCI ?= golangci-lint
CARGO ?= cargo

.PHONY: all
all: test

.PHONY: test
test:
	$(GO) test ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: lint
lint:
	$(GOLANGCI) run ./...

.PHONY: probe-build
probe-build:
	cd rust/probe && $(CARGO) build
