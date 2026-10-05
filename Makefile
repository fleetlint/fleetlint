# Task-runner contract (https://fleetlint.org/baseline/#task-runner-contract). `make check` is the definition of green.
SHELL := bash
.SHELLFLAGS := -euo pipefail -c
.ONESHELL:
.DEFAULT_GOAL := check-fast

MAIN_BRANCH ?= main
COVER_MIN   ?= 80
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X github.com/fleetlint/fleetlint/internal/cli.Version=$(VERSION)
PKGS        := ./...
LICENSES    ?= MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0
GO_LICENSES := go run github.com/google/go-licenses/v2@v2.0.1
GOLANGCI_VERSION    ?= v2.14.0
GOVULNCHECK_VERSION ?= v1.8.0
GOCOVER_VERSION     ?= v1.5.0
# Tools are built with this module's toolchain: golangci-lint refuses code that targets a newer Go than it was built with.
TOOLCHAIN   := $(shell go env GOVERSION)
BIN         := bin/fleetlint

# CONTAINER=1 runs every target inside the dev container (needs Docker and the devcontainer CLI);
# the default, CONTAINER=0, runs it on this machine. Inside the container targets always run directly.
CONTAINER ?= 0
TARGETS := help tools fmt lint test cover audit bench build dist check-fast check release
.PHONY: $(TARGETS)

ifeq ($(CONTAINER)$(IN_CONTAINER),1)
$(TARGETS):
	@devcontainer up --workspace-folder . >/dev/null
	devcontainer exec --workspace-folder . make $@ $(filter-out CONTAINER=%,$(MAKEOVERRIDES)) CONTAINER=0
else

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*##' '{printf "  %-12s %s\n", $$1, $$2}'

tools: ## install the pinned Go tools the gates use (CI runs this; gitleaks, syft and diff-cover come from their own installers)
	GOTOOLCHAIN=$(TOOLCHAIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	GOTOOLCHAIN=$(TOOLCHAIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	GOTOOLCHAIN=$(TOOLCHAIN) go install github.com/boumenot/gocover-cobertura@$(GOCOVER_VERSION)

fmt: ## format in place
	golangci-lint fmt

lint: ## format check + linters + vet, warnings as errors
	golangci-lint fmt --diff
	golangci-lint run
	go vet $(PKGS)

test: ## all tests with the race detector, shuffled
	go test -race -shuffle=on $(PKGS)

cover: ## coverage on changed lines >= COVER_MIN (diff-cover) plus total for information
	mkdir -p coverage
	go test -race -coverprofile=coverage/cover.out -coverpkg=$(PKGS) $(PKGS)
	go tool cover -func=coverage/cover.out | tail -1
	@if command -v gocover-cobertura >/dev/null && command -v diff-cover >/dev/null; then \
	  gocover-cobertura < coverage/cover.out > coverage/cobertura.xml; \
	  diff-cover coverage/cobertura.xml --compare-branch=origin/$(MAIN_BRANCH) --fail-under=$(COVER_MIN); \
	else echo "diff-cover/gocover-cobertura not installed: changed-line gate skipped"; fi

audit: ## vulnerabilities, licenses, tidy module graph, secrets
	govulncheck $(PKGS)
	$(GO_LICENSES) check $(PKGS) --allowed_licenses=$(LICENSES)
	go mod tidy -diff
	gitleaks git --no-banner --redact .

bench: ## benchmarks (not part of check)
	go test -run='^$$' -bench=. -benchmem $(PKGS)

build: ## build the binary
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/fleetlint

dist: ## release artifacts into dist/ (used by the release workflow / goreleaser)
	goreleaser release --snapshot --clean

check-fast: ## before every commit (< 30 s)
	golangci-lint fmt --diff
	golangci-lint run --new-from-rev=origin/$(MAIN_BRANCH) || golangci-lint run
	go test -short $(PKGS)
	$(BIN) check --fail-on error || true

check: lint test cover audit build ## definition of green (< 10 min)
	$(BIN) check --fail-on error

release: ## refuse dirty/non-main -> check -> tag; pushing the tag triggers the release workflow
	@test -z "$$(git status --porcelain)" || { echo "dirty tree"; exit 1; }
	@test "$$(git branch --show-current)" = "$(MAIN_BRANCH)" || { echo "not on $(MAIN_BRANCH)"; exit 1; }
	$(MAKE) check
	@v=$$(git-cliff --bumped-version); echo "next version: $$v"; \
	git-cliff --tag "$$v" -o CHANGELOG.md && git add CHANGELOG.md && git commit -q -m "chore(release): $$v" && git tag -s "$$v" -m "$$v" && echo "tagged $$v — push with: git push --follow-tags"
endif
