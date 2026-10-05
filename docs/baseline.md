# The baseline

What the `fleetlint:*` presets stand for. Each section states the practice, then which rules check it. fleetlint checks how a repository is set up; the parts marked *review* are about the code itself and need a linter, a test or a reader. The per-stack tools are in [stacks.md](stacks.md).

Tool names, flags and rule names reflect the tools as of 2026. Verify them against the installed version before writing configuration.

## Tiers

Not every repository deserves everything. `facts.tier` selects how much applies; most rules name the tiers they apply to.

| Tier | For | Applies |
|---|---|---|
| 1 | production or public | everything |
| 2 | tools you rely on | hygiene, task runner, hooks, lint, tests on critical paths, security, dependencies |
| 3 | experiments | secrets and junk kept out of git, a `.gitignore`, a README line saying it is an experiment |

Promote a repository the day something starts depending on it.

## Hygiene

- No OS, editor or build junk, scratch scripts or generated report documents in git.
- No `.env` file with values in git; `.env.example` documents the keys. A secret that was committed is rotated, not just removed.
- `.gitignore` exists and covers the stack's build output.
- `.editorconfig` and `.gitattributes` (`* text=auto eol=lf`, binaries marked `binary`) keep formatting and line endings the same on every machine.
- No large binaries in git outside git-lfs.
- A dependency lockfile is committed for every package manager in use.
- The toolchain version is pinned in the repository, so every machine and CI build with the same compiler or runtime.

Checked by: `repo/no-tracked-junk`, `repo/no-tracked-env`, `repo/gitignore-present`, `repo/editorconfig`, `repo/gitattributes`, `repo/no-large-files`, `repo/lockfile-committed`, `repo/toolchain-pinned`.

## Slop

The traces careless or generated code leaves behind: narration comments, placeholders, variant copies, swallowed errors, suppressions, skipped and trivial tests, report documents, vague commits. The full catalog, with what each one costs and how to clean it up, is in [slop.md](slop.md).

Checked by: `slop/focused-tests`, `slop/conflict-markers`, `slop/scratch-scripts`, `slop/report-documents` in `fleetlint:recommended`; the heuristic `slop/*` rules in the add-on preset `fleetlint:slop`; `quality/no-slop-markers`.

## Task-runner contract

One entry point, the same on every machine and in CI: a Makefile, a justfile or a Taskfile at the root, or the stack's native runner where that is the convention (Gradle, `npm run`).

| Target | Does |
|---|---|
| `fmt` | format in place |
| `lint` | formatter check, linter, type check; warnings are errors |
| `test` | all tests, with the race detector or sanitizers where the stack has them |
| `cover` | tests with coverage; fails when changed lines fall below the threshold |
| `audit` | vulnerability scan, license check, secret scan, unused dependencies |
| `build` | the artifact |
| `check` | `lint`, `test`, `cover`, `audit`, then `build`: the definition of green |
| `check-fast` | format check, lint and unit tests on what changed, for use before every commit |
| `bench`, `release` | not part of `check`: benchmarks are noisy and a release is a deliberate act |

- Budgets: `check-fast` under 30 seconds, `check` under 10 minutes. A slow gate gets bypassed; cache or parallelize before adding to it.
- Ratchet, do not big-bang: existing violations go into a baseline that may only shrink, so new code is clean from the first day. `fleetlint baseline` does this for fleetlint's own findings; each linter has its equivalent.
- Size and complexity limits are enforced by the linter where the stack has the rules: files up to 400 lines (800 hard), functions up to 50 (80 hard), cognitive complexity up to 15. Tests and generated code are exempt from length limits.

The contract is the set of targets, not the tool. Two things are a choice, and both are facts fleetlint detects and `.fleetlint.yaml` can pin:

- **The task runner** (`facts.taskrunner`): `make`, `just`, `task`, package.json scripts or Gradle. The first one found is used; pin it when a repository has several, or to say which one `fix` should write. Rules read targets, dependencies and commands through the same model for every kind.
- **Where the targets run** (`facts.container`):
  - `none`: on the machine. The tools then have to be reproducible from the repository: a `tools` target that installs pinned versions, a version-manager file (`mise.toml`, `.tool-versions`), or a Nix flake or devenv.
  - `devcontainer`: in the repository's dev container. This is the default once a `devcontainer.json` exists.
  - `docker` or `podman`: in an image run with that engine, without a dev container definition.

In a container mode, a target called outside the container runs inside it, and the check workflow uses the same environment, so every result comes from one place. `CONTAINER=0` on the command line stays on the machine; `facts.container: none` does so for the whole repository.

`fleetlint fix` writes for the combination chosen: a Makefile, justfile or Taskfile, with the container switch when there is a container; hooks and a check workflow that call that runner; and for `devcontainer` the container itself. It never introduces a container or changes the runner on its own.

The rules about tools accept alternatives the same way: each lists the known ways of meeting it (other scanners, another linter setup, `docker compose exec` instead of the devcontainer CLI), and a repository adds its own with `accept:`. A generated file belongs to the repository; editing it is expected, and tests in this project keep reasonable edits passing.

Checked by: `repo/dev-environment`, `taskrunner/container`, `taskrunner/targets`, `taskrunner/check-composition`, `quality/check-passes` (with `--deep`), `quality/coverage-threshold`, `quality/policy-enforced`.

## Git hooks

Hooks run the project's own pinned tools through the task runner (`language: system`), so a hook result always matches `check`. [prek](https://github.com/j178/prek) reads the standard `.pre-commit-config.yaml` and runs the hygiene hooks as offline built-ins.

- `pre-commit`: fast checks on staged files (format, lint, secrets, hygiene), under about 10 seconds.
- `commit-msg`: Conventional Commits.
- `pre-push`: the full `check`.
- Tools that analyze the whole project (type checkers, `go vet`, clippy) do not take file names.
- Formatters fix files in place and fail the commit once; re-stage and commit again.
- Never exclude files to get a hook passing, and never bypass hooks; findings that cannot be fixed now go into the linter's baseline.
- Polyglot repositories scope each stack's hooks with `files:`; `fleetlint fix` writes that for a nested project (hooks run inside its directory, on its files only). Monorepos with independent projects keep one configuration per project.

Checked by: `hooks/config-present`, `hooks/shared-hygiene`, `hooks/secret-scan`, `hooks/conventional-commits`, `hooks/pre-push-check`, `lint/shell`.

## Static analysis

Each stack's strict linter and type-checker settings are in [stacks.md](stacks.md). Suppressions carry a reason on the same line or the line above; a suppression without one is removed by fixing the code.

Checked by: `lint/go-config`, `lint/go-linters-enabled`, `lint/python-config`, `lint/python-rules-enabled`, `lint/rust-config`, `lint/rust-lints-denied`, `lint/node-config`, `lint/node-strict-flags`, `lint/kotlin-config`, `lint/flutter-config`, `lint/flutter-strict-modes`, `lint/shell`.

## Tests

*Review*, apart from the coverage gate.

- Critical paths have characterization tests: the happy path and the failures that actually occur (unreachable, timeout, expired credentials, malformed input, interrupted operations, concurrent access).
- Pure logic has unit tests; parsers and anything reading untrusted input have property or fuzz tests.
- Integration tests use the project's real code and fake only the outer boundary (network, filesystem, database, clock). Prefer in-memory fakes or test containers to mocks.
- Deterministic: clock, randomness and environment are injected; no real network; no sleeps; random order and parallel.
- One behaviour per test, named after it. Assert outcomes, not implementation details.
- A bug fix ships with a test that failed before the fix.
- No tests without assertions, no skipped or focused tests committed, no expected values changed to match buggy output, no automatic retries to hide a flaky test.
- Coverage is a floor, not a goal: at least 80% on changed lines, 90% on core logic. Mutation testing on core modules shows whether the tests would notice a bug.

Checked by: `quality/coverage-threshold`.

## Error handling

*Review.*

- No empty catches, no ignored error returns. Catch-alls only at true top-level boundaries, where they log and report.
- Catch specific types. A handler recovers, translates to a domain error, or propagates with context and the cause.
- Expected failures are modelled: typed errors, error enums or result types, never null, false or -1.
- Log once, where the error is handled, with structured context and no secrets.
- Error messages say what failed and with which input.
- No panics for recoverable conditions.

## Design

*Review.*

- Dependencies point inward: entry points, then application, then domain; adapters implement interfaces the inner layers own. The stack's import-rule tool enforces it.
- No global mutable state and no singletons reached from inside other code; dependencies are injected and wired in one place.
- Types carry meaning: illegal states are unrepresentable, IDs and units have their own types.
- Wire and storage models are separate from domain models and mapped by validating parsers at the boundary.
- No dead code, commented-out blocks, near-duplicate implementations with suffixes such as `V2` or `New`, pass-through layers, or `utils` dumping grounds.

## Robustness

*Review*, apart from containers.

- Every IO call has a timeout; cancellation propagates; nothing asynchronous runs without an owner.
- Retries are bounded, back off with jitter and apply only to idempotent operations.
- Resources are released through the language's scoped-cleanup construct.
- Configuration is read from the environment or files, validated once at startup, and fails fast.
- Persistent formats are versioned and migrations are tested.
- Time is stored in UTC, money in decimal or integer minor units, strings are handled Unicode-aware, pagination and limits are bounded.
- Services have health and readiness endpoints, request timeouts and body limits, and one documented error format.
- Containers: the base image is pinned by digest, the build is multi-stage, the process does not run as root, a `.dockerignore` exists, no secrets end up in layers.

Checked by: `containers/dockerignore`, `containers/non-root-user`, `containers/base-image-pinned`.

## Security

- Secrets live in the platform's secure storage or a secret manager, never in source or plain configuration. A secret scanner runs before every commit and over the history.
- Secrets are wrapped in a type that redacts itself when printed, logged or serialized.
- No global "trust all certificates"; custom CAs are scoped to a host.
- All external input is validated and bounded at the boundary.
- Queries are parameterized, no shell strings are built, paths are joined safely.
- CI workflows declare the least permissions they need, pin third-party actions to commit SHAs, and do not run untrusted pull-request code with secrets.

Checked by: `repo/no-tracked-env`, `hooks/secret-scan`, `ci/least-privilege`, `ci/actions-pinned`, `ci/no-pull-request-target`. The rest is *review*.

## Dependencies

- Lockfiles are committed and verified on install (`npm ci`, `uv sync --locked`, `cargo --locked`, Gradle dependency verification).
- Install scripts are disabled where the package manager allows it.
- The `audit` target scans dependencies for known vulnerabilities and checks licenses against an allowlist compatible with the project's own license; exceptions are documented with a reason.
- Updates are automated, so pinned does not become stale.
- A new dependency has to beat about 100 lines of own code; check publisher, age and that the name is the package you meant.
- A vendored or forked dependency is documented: upstream, base version, what changed and why.

Checked by: `repo/lockfile-committed`, `deps/update-automation`, `deps/vulnerability-scan`, `deps/license-check`, `deps/install-scripts-disabled`.

## Logging

*Review.*

- One structured logger with levels; raw print calls are banned by the linter. User-facing CLI output goes through its own output layer.
- Events carry key/value context. Debug output is off in release builds.
- No personal data in logs or crash reports; reporting is opt-in.

## Documentation

Write what a reader cannot get from the code in 30 seconds, in plain factual prose: no marketing adjectives, no claims the code does not back, no filler sections. Every command in the documentation works.

- `README.md`: what it is, requirements, quickstart, the task-runner targets, configuration.
- `docs/ARCHITECTURE.md` (two pages at most): a diagram of the components, where each concern lives, the critical paths and the invariants.
- `docs/adr/NNNN-title.md`: one short record per significant decision.
- `CHANGELOG.md` in Keep a Changelog format.
- Comments say why, never what.

Checked by: `repo/readme-present`, `docs/architecture`, `docs/adr-directory`, `changelog/present`, `quality/no-slop-markers`.

## Continuous integration

CI runs the same `check` the hooks run, on pushes to the main branch and on pull requests, so it is the record that main was green and not a different subset of checks.

Checked by: `ci/check-workflow`, `ci/least-privilege`, `ci/actions-pinned`, `ci/job-timeouts`, `ci/concurrency-cancel`, `ci/no-pull-request-target`.

## Releases

- Semantic Versioning; one source of truth for the version.
- The changelog is generated from Conventional Commits (git-cliff) and edited for clarity; breaking changes carry a migration note.
- A `release` target refuses a dirty tree or a branch other than main, runs `check`, computes the version, writes the changelog and creates a signed tag. Pushing is a separate step.
- A tag starts the release pipeline, which builds the artifacts, writes checksums, generates an SBOM per artifact, signs them and attests the build provenance.
- Builds are reproducible: pinned toolchain, locked dependencies, version and commit embedded in the artifact.
- Libraries run the stack's API-compatibility check against the previous release.
- Every release has a way back.

Checked by: `changelog/generator-configured`, `release/tag-triggered`, `release/semver-tags`, `release/checksums`, `release/sbom`, `release/signing`, `release/provenance`, `release/changelog-in-release`. A repository with its own pipeline passes when a known producer is present, or names its producer with `accept:`; see [writing-rules.md](writing-rules.md).

## Public repositories

- `LICENSE` with a recognised license, also declared in the package manifest.
- `SECURITY.md` says how to report a vulnerability privately.
- `CONTRIBUTING.md` covers setup, the `check` command and the commit convention; a code of conduct names a contact.
- Issue and pull-request templates where the forge supports them.
- No agent instruction files are tracked, and no commit carries an AI author, co-author trailer or "generated with" footer.

Checked by: `public/license`, `public/manifest-license`, `public/security-policy`, `public/contributing`, `public/code-of-conduct`, `public/issue-templates`, `public/no-agent-files`, `public/no-ai-attribution`.

## User interfaces

*Review.* Applies to applications with a user interface.

- Accessibility: labels and roles on every interactive element, WCAG AA contrast, touch targets of at least 44 by 44 pt, usable with a screen reader and at 200% text size, nothing conveyed by colour alone. Automated checks run in tests.
- No hard-coded user-facing strings; a check fails on missing or unused translation keys.
- Screenshot tests for key screens in light and dark mode and at a large text size; baselines are reviewed like code.
- Every screen has loading, empty, error and offline states.

## Performance

*Review.*

- Measure before optimizing; hot paths have benchmarks with baselines stored in the repository.
- Budgets (bundle size, cold start, memory, p95 latency) are numbers in the repository with a check that fails when they are exceeded.
- No N+1 queries, unbounded lists in memory or synchronous IO on UI threads.
