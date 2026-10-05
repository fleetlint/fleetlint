# Task-runner contract: fmt, lint, test, cover, audit, build, check, check-fast. Replace each TODO with the stack's commands.
# Targets are the interface the git hooks and CI rely on; keep their names.
SHELL := bash
.SHELLFLAGS := -euo pipefail -c
.ONESHELL:
.DEFAULT_GOAL := check-fast
MAIN_BRANCH ?= main
COVER_MIN   ?= 80

.PHONY: help tools fmt lint test cover audit bench build dist check-fast check release

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*##' '{printf "  %-12s %s\n", $$1, $$2}'

tools: ## install the pinned tools the other targets use
	@echo "TODO: install each tool at a fixed version (go install pkg@v1.2.3, uv tool install name==1.2.3)"

fmt: ## format in place
	@echo "TODO: formatter"

lint: ## format check + linter + type check, warnings as errors
	@echo "TODO: linter"

test: ## all tests (race detector / sanitizers where available)
	@echo "TODO: tests"

cover: ## tests with coverage; fail if changed lines < COVER_MIN
	@echo "TODO: coverage, then: diff-cover coverage.xml --compare-branch=origin/$(MAIN_BRANCH) --fail-under=$(COVER_MIN)"

audit: ## vulnerabilities + licenses + secrets + unused deps
	gitleaks git --no-banner .
	@echo "TODO: osv-scanner / license check / unused deps"

bench: ## benchmarks vs stored baseline (not part of check)
	@echo "TODO: benchmarks"

build: ## production build
	@echo "TODO: build"

dist: ## release artifacts into dist/ (the release workflow signs and publishes them)
	@echo "TODO: build the release artifacts into dist/"

check-fast: ## before every commit (< 30 s)
	$(MAKE) lint
	@echo "TODO: unit tests on changed packages"

check: lint test cover audit build ## definition of green (< 10 min)
	fleetlint check --fail-on error

release: ## refuse dirty/non-main -> check -> version -> changelog -> signed tag
	@test -z "$$(git status --porcelain)" || { echo "dirty tree"; exit 1; }
	@test "$$(git branch --show-current)" = "$(MAIN_BRANCH)" || { echo "not on $(MAIN_BRANCH)"; exit 1; }
	$(MAKE) check
	@echo "TODO: bump version; git-cliff --tag vX.Y.Z -o CHANGELOG.md; git commit -m 'chore(release): vX.Y.Z'; git tag -s vX.Y.Z"
