.PHONY: build check-all check-architecture check-layout check-tools check-dupl format lint test test-all tools

export PATH := $(shell go env GOPATH)/bin:$(PATH)

GO_ARCH_LINT_VERSION ?= v1.15.0
GOLANGCI_LINT_VERSION ?= v2.8.0
DUPL_VERSION ?= v0.0.0-20260401084720-c99c5cf5c202
DUPL_THRESHOLD ?= 60
DUPL_PATHS ?= internal

build:
	go build -trimpath -buildvcs=false ./...

format:
	@test -z "$$(gofmt -l .)" || { gofmt -d .; exit 1; }

test:
	go test ./...

lint:
	golangci-lint run

check-tools:
	@./scripts/check-tools.sh

tools: check-tools
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	go install github.com/fe3dback/go-arch-lint@$(GO_ARCH_LINT_VERSION); \
	go install github.com/golangci/dupl@$(DUPL_VERSION)

check-layout:
	go run ./tools/layoutguard ./...

check-architecture:
	@command -v go-arch-lint >/dev/null 2>&1 || { echo "go-arch-lint is missing; run 'make tools'"; exit 1; }; \
	for layer in domain application infra delivery; do \
	  find internal -type d -name "$$layer" -print -quit | grep -q . || { echo "architecture check skipped: no complete layered feature exists yet"; exit 0; }; \
	done; \
	go-arch-lint check --project-path .

check-dupl:
	@command -v dupl >/dev/null 2>&1 || { echo "dupl is missing; run 'make tools'"; exit 1; }; \
	output=$$(dupl -t $(DUPL_THRESHOLD) $(DUPL_PATHS) 2>&1); \
	printf '%s\n' "$$output"; \
	printf '%s' "$$output" | grep -q 'Found total 0 clone groups'

test-all:
	go test ./...
	go test -race ./...
	go vet ./...
	$(MAKE) lint
	$(MAKE) check-layout
	$(MAKE) check-architecture
	$(MAKE) check-dupl

check-all:
	$(MAKE) format
	$(MAKE) test-all
	$(MAKE) build
