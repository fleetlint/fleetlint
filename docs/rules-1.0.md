# Rule catalog for 1.0.0

The practices behind the rules are described in [baseline.md](baseline.md) and, per stack, in [stacks.md](stacks.md).

The complete set of rules the `fleetlint:*` presets ship at 1.0. Rules are grouped by family. **Status**: `done` is in the catalog with tests. Presets: `minimal` ⊂ `recommended` ⊂ `strict`; `oss` = recommended + the `public/*` family forced on; `slop` is an add-on with the heuristic `slop/*` rules.

Severity is the default; repositories may raise it freely and lower it with a reason.

## repo — repository hygiene (all tiers)

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `repo/gitignore-present` | error | `.gitignore` exists | done |
| `repo/no-tracked-junk` | error | no OS/editor/build junk in the index (`.DS_Store`, `*.log`, `__pycache__`, …) | done |
| `repo/no-tracked-env` | error | no `.env*` with values tracked (`.env.example` allowed) | done |
| `repo/editorconfig` | warning | `.editorconfig` with `root = true` | done |
| `repo/gitattributes` | warning | `.gitattributes` with `* text=auto` | done |
| `repo/readme-present` | warning | README with an install/quickstart section | done |
| `repo/lockfile-committed` | error (tiers 1–2) | lockfile for every detected package manager is tracked | done |
| `repo/no-large-files` | warning | no tracked file over `params.max_kb` (default 2048) unless under git-lfs | done |
| `repo/codeowners` | info (tier 1) | `CODEOWNERS` in a recognised location | done |
| `repo/dev-environment` | warning (tiers 1–2) | the tools are reproducible: in a container mode the container; on the machine a Nix flake or devenv, `mise.toml` / `.tool-versions`, or a `tools` target with pinned versions (outcome rule) | done |
| `repo/toolchain-pinned` | warning (tiers 1–2) | each stack pins its toolchain: go.mod `toolchain` or full `go` version, `.python-version`, `rust-toolchain.toml`, `.nvmrc`, Gradle wrapper, `.fvmrc`, `.tool-versions`, `mise.toml` | done |

## taskrunner — the Makefile contract

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `taskrunner/targets` | error (tiers 1–2) | `fmt lint test cover audit check check-fast` exist and are not placeholders, in whichever runner is used (Makefile, justfile, Taskfile, package.json scripts; Gradle by convention) | done |
| `taskrunner/container` | warning (tiers 1–2, when `facts.container` is not `none`) | a target called outside the container runs inside it: devcontainer CLI, docker, podman, or an `accept:`ed way (outcome rule) | done |
| `taskrunner/check-composition` | warning | `check` depends on `lint` and `test` (and `cover`, `audit` at tier 1) | done |

## lint — strict static analysis per stack

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `lint/go-config` | warning | golangci-lint v2 config | done |
| `lint/go-linters-enabled` | warning | the v2 config enables `errcheck govet staticcheck errorlint gosec gocognit funlen`; one finding per missing linter | done |
| `lint/python-config` | warning | `[tool.ruff]` plus mypy/pyright strict | done |
| `lint/rust-config` | warning | `[lints]` table in Cargo.toml | done |
| `lint/node-config` | warning | eslint flat config and `strict: true` tsconfig | done |
| `lint/kotlin-config` | warning | detekt or ktlint configured | done |
| `lint/flutter-config` | warning | `analysis_options.yaml` with `very_good_analysis` or `flutter_lints` | done |
| `lint/python-rules-enabled` | warning | ruff `select` contains E, F, I, B, UP, SIM, BLE, S, C90, T20 (or `ALL`); one finding per missing family | done |
| `lint/node-strict-flags` | warning | tsconfig sets `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`, `noFallthroughCasesInSwitch`; one finding per missing flag | done |
| `lint/rust-lints-denied` | warning | `[lints.clippy]` enables `unwrap_used`, `expect_used`, `panic`, `todo`, `dbg_macro`; one finding per missing lint | done |
| `lint/flutter-strict-modes` | warning | `analyzer.language` sets `strict-casts`, `strict-inference`, `strict-raw-types`; one finding per missing mode | done |
| `lint/shell` | info | when shell scripts are tracked, shellcheck runs as a hook, in the Makefile or in CI | done |

## quality — behaviour, not configuration

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `quality/check-passes` | error (tiers 1–2, `--deep`) | `make check` exits 0 within the budget | done |
| `quality/no-slop-markers` | warning | README, docs and CHANGELOG contain none of the AI-filler phrases from the slop catalog; one finding per line | done |
| `quality/policy-enforced` | warning (tiers 1–2) | `fleetlint check` runs as a hook, in the task runner or in a CI workflow | done |
| `quality/coverage-threshold` | warning (tiers 1–2) | a coverage floor is enforced somewhere: Makefile `cover` recipe, `.codecov.yml`, `diff-cover --fail-under`, `pytest --cov-fail-under`, cargo-llvm-cov `--fail-under`, Jest `coverageThreshold` | done |

## docs — the documents that must exist

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `docs/architecture` | warning (tier 1) | `docs/ARCHITECTURE.md` or an Architecture section in the README | done |
| `docs/adr-directory` | info (tier 1) | `docs/adr/` with at least one record | done |
| `changelog/present` | warning (tiers 1–2) | `CHANGELOG.md` with an Unreleased section | done |
| `changelog/generator-configured` | info (tier 1) | `cliff.toml` or release-please/changesets | done |

## hooks — prek / pre-commit

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `hooks/config-present` | error (tiers 1–2) | `.pre-commit-config.yaml` present | done |
| `hooks/shared-hygiene` | warning | the shared hygiene hooks (trailing whitespace, EOF, merge conflicts, large files, private keys) | done |
| `hooks/secret-scan` | error (tiers 1–2) | gitleaks hook | done |
| `hooks/conventional-commits` | warning | commit-msg hook enforcing Conventional Commits | done |
| `hooks/pre-push-check` | warning (tiers 1–2) | a pre-push stage runs `make check-fast` or `check` | done |

## deps — dependency hygiene

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `deps/update-automation` | warning (tiers 1–2) | Renovate (`renovate.json*`, `.renovaterc*`, `.github/renovate.json5`) or Dependabot config | done |
| `deps/audit-target` | — | folded into `taskrunner/targets` (`audit`) | done |
| `deps/vulnerability-scan` | warning (tiers 1–2) | the task runner or CI runs a vulnerability scanner (govulncheck, pip-audit, cargo deny/audit, npm/pnpm/yarn audit, osv-scanner, trivy, grype) | done |
| `deps/license-check` | info (tier 1) | the task runner or CI checks dependency licenses (go-licenses, pip-licenses, cargo deny, license-checker, licensee, osv-scanner) | done |
| `deps/install-scripts-disabled` | info (node, tiers 1–2) | install scripts are off or allowlisted (`.npmrc`, `.yarnrc.yml`, pnpm `onlyBuiltDependencies`, bun `trustedDependencies`) | done |

## containers — repositories with a Dockerfile

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `containers/dockerignore` | warning | a `.dockerignore` exists | done |
| `containers/non-root-user` | warning | every Dockerfile sets a non-root `USER` or builds on a `:nonroot` base; one finding per file | done |
| `containers/base-image-pinned` | info (tier 1) | every `FROM` carries `@sha256:`; `scratch`, build arguments and earlier stages exempt | done |

## ci — the check workflow

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `ci/check-workflow` | error (tiers 1–2) | a workflow on push/PR runs the task runner's `check` | done |
| `ci/actions-pinned` | warning (tier 1) | every `uses:` is pinned to a commit SHA | done |
| `ci/least-privilege` | warning (tiers 1–2) | top-level or per-job `permissions:` present and not `write-all` | done |
| `ci/job-timeouts` | info | every job sets `timeout-minutes`; one finding per job | done |
| `ci/concurrency-cancel` | info | PR workflows declare a `concurrency` group with `cancel-in-progress` | done |
| `ci/no-pull-request-target` | error (public) | no workflow uses `pull_request_target` with a checkout of the PR head | done |

## release — producing artifacts (tier 1, `when: release.exists`)

Outcome rules: satisfiers list the known producers; a repository adds its own with `accept:`.

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `release/sbom` | error | SPDX/CycloneDX SBOM generated in the release pipeline | done |
| `release/signing` | error | artifacts signed in the release pipeline: GoReleaser `signs:` with cosign, `sigstore/cosign-installer` + `cosign sign-blob`, `sigstore/gh-action-sigstore-python`, `npm publish --provenance` | done |
| `release/provenance` | warning | build provenance attested: `actions/attest-build-provenance`, `slsa-framework/slsa-github-generator`, GoReleaser `sboms`+`signs` with keyless, Gitea equivalent via `cosign attest` | done |
| `release/tag-triggered` | error | a workflow triggered by `push.tags` exists when releases are produced | done |
| `release/semver-tags` | warning | existing tags match `v?MAJOR.MINOR.PATCH[-pre]`; one finding per offending tag | done |
| `release/checksums` | warning | a checksums file is produced (GoReleaser default, `sha256sum`, `shasum -a 256`) | done |
| `release/changelog-in-release` | info | release notes come from git-cliff/GoReleaser changelog, not hand-written | done |

## public — open-source repositories (`when: repo.public`)

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `public/license` | error | `LICENSE` with a recognised SPDX license | done |
| `public/security-policy` | warning (tier 1) | `SECURITY.md` | done |
| `public/no-agent-files` | error | no `CLAUDE.md`, `AGENTS.md`, `.claude/`, `.cursor*`, `.aider*`, `.windsurf*`, `.github/copilot-instructions.md` tracked | done |
| `public/no-ai-attribution` | error | last `params.commits` (default 200) commits carry no AI author, co-author trailer or generated-with footer | done |
| `public/contributing` | info (tier 1) | `CONTRIBUTING.md` | done |
| `public/code-of-conduct` | info (tier 1) | `CODE_OF_CONDUCT.md` | done |
| `public/manifest-license` | info (tier 1) | the package manifest declares the license (package.json, pyproject.toml, Cargo.toml) | done |
| `public/issue-templates` | info (tier 1) | an issue or pull-request template in `.github/` or `.gitea/` | done |

## slop — traces of careless or generated code

Catalog and rationale: [slop.md](slop.md). The first four are in `fleetlint:recommended`; the others are heuristics in the add-on preset `fleetlint:slop`, all `info` except `slop/insecure-patterns` (warning).

| Rule | Severity | Checks | Status |
|---|---|---|---|
| `slop/focused-tests` | error | no `.only`, `fit`, `fdescribe`, `solo: true` | done |
| `slop/conflict-markers` | error | no merge conflict markers in source | done |
| `slop/scratch-scripts` | warning | no `fix_*`, `debug-*`, `tmp_*` scripts at the root | done |
| `slop/report-documents` | warning | no `*_SUMMARY.md`, `*REPORT*.md`, `NEXT_STEPS.md` and the like | done |
| `slop/narration-comments`, `slop/placeholder-comments`, `slop/banner-comments`, `slop/todo-without-issue`, `slop/commented-out-code` | info | comment hygiene | done |
| `slop/emoji-in-code`, `slop/typographic-chars`, `slop/emoji-headings` | info | no emoji or typographic characters in code and headings | done |
| `slop/debug-output`, `slop/arbitrary-delays`, `slop/swallowed-errors`, `slop/escape-hatches`, `slop/vague-errors`, `slop/placeholder-values` | info | code smells outside tests | done |
| `slop/insecure-patterns` | warning | no disabled TLS verification, `eval(`, `chmod 777`, wildcard CORS | done |
| `slop/variant-names`, `slop/variant-files`, `slop/dumping-ground-files` | info | no `V2`/`New`/`Old` variants, no `utils` dumping grounds | done |
| `slop/skipped-tests`, `slop/trivial-asserts` | info | test hygiene | done |
| `slop/vague-commit-messages`, `slop/non-conventional-commits`, `slop/huge-commits` | info | history of the last `params.commits` commits | done |

## Totals

| | Count |
|---|---|
| done | 94 |
| **1.0 catalog** | **94** |

## Not rules: the templates

Three entries of earlier drafts (`release/template-shape`, `release/gitea-parity`, `ci/same-shape-both-forges`) asserted that a repository's workflows have the shape of fleetlint's templates. They are not rules: the release and CI rules above check outcomes, so a repository with its own pipeline passes, and a shape rule would fail it for being different. What they stood for is checked on fleetlint's side instead: a test (`internal/engine/templates_test.go`) requires that a repository set up from each stack's templates passes the hook, CI and release rules, and the templates still have to run once on a real GitHub runner (ROADMAP).

Not rules, but part of 1.0: organization layer (locked rules, severity floors, `--require`, signed git/OCI catalog sources) and team layer (sources file, layered resolution, per-layer attribution). See ROADMAP.
