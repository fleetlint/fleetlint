# Changelog

All notable changes to this project are documented here.
Format: Keep a Changelog. Versioning: Semantic Versioning.

## [Unreleased]

## [0.1.0] - 2026-10-05

First release. The configuration, catalog and JSON output formats may still change before 1.0; changes will be listed here with a compatibility note.

### Added

- **Checks.** `fleetlint check` evaluates a repository against a catalog of rules about its setup: repository hygiene, the task-runner contract, git hooks, linter configuration per stack, CI workflows, the release pipeline, dependencies, containers, documentation and what a public repository needs. Zero configuration: stacks (Go, Python, Rust, Node, Kotlin, Flutter), forge, tier and layout are discovered. Exit codes 0 (clean), 1 (findings), 2 (configuration error), 3 (internal error).
- **Catalog.** 94 rules in three presets, maintained in their own repository ([fleetlint/catalog](https://github.com/fleetlint/catalog)) and embedded in the binary: `fleetlint:minimal`, `fleetlint:recommended` and the add-on `fleetlint:slop` with heuristics for the traces careless or generated code leaves behind. The practices behind them are in `docs/baseline.md`, `docs/stacks.md` and `docs/slop.md`; every rule has a generated page under `docs/rules/`.
- **Rules as data.** Every rule in the presets except the one that runs the repository's check is data in the catalog. Rules are YAML with [CEL](https://cel.dev) predicates over discovered facts and file accessors (`docs/cel-reference.md`). Kinds: `expr`, `foreach` (one finding per element), `outcome` (several tools can satisfy a requirement; a repository adds its own with `accept:`), and `command` (repository-local only, with `--deep`).
- **Configuration.** `.fleetlint.yaml`: `extends`, pinned `facts`, `scopes`, rule overrides, inline rules, and exceptions with a reason and an optional deadline. Disabling a rule or lowering its severity needs a reason, and both show up in every report. `fleetlint init` writes the file; `fleetlint schema` prints its JSON Schema.
- **Monorepos and nested projects.** Workspace members (go.work, pnpm, package.json workspaces, Cargo, melos, Gradle) and top-level directories with their own manifest are checked as scopes, each with its own stack, with optional nested `.fleetlint.yaml` files.
- **Fixes.** `fleetlint fix` plans the mechanical part (create a file from a template, untrack files, add ignore patterns, set a value in a JSON or TOML manifest) and `--apply` writes it; existing files are never overwritten and existing keys never changed. Hook configuration and check workflow are assembled for the root stack and every nested project, and they run fleetlint itself (`quality/policy-enforced` requires that something does). `repo/dev-environment` asks for a reproducible tool setup.
- **Tool alternatives.** The task runner (`facts.taskrunner`: make, just, task, package.json scripts, Gradle) and where its targets run (`facts.container`: none, devcontainer, docker, podman) are facts: detected, and pinned in `.fleetlint.yaml` when wanted. Rules read every runner through one model, and `fix` writes the runner file, hooks and check workflow for the chosen combination; in a container mode a target called outside the container runs inside it (`CONTAINER=0` stays on the machine). Rules that depend on a tool (`ci/check-workflow`, `lint/go-config`, `deps/vulnerability-scan`, `deps/license-check`, `taskrunner/container`, `repo/dev-environment`) are outcome rules: they list the known alternatives and take more through `accept:`.
- **Baseline.** `fleetlint baseline` grandfathers today's findings so only new ones fail; the baseline only shrinks.
- **Output.** Table, JSON, Markdown (pull-request comment), SARIF (code scanning) and an `agent` format: an ordered task list with a precise instruction per rule for a coding agent. `fleetlint explain <rule>` prints one rule in full.
- **Fleet mode.** `fleetlint fleet` checks many repositories (paths, a fleet file, or `--from github:<owner>` / `--from gitea:<host>/<owner>`) and writes a static report: repositories with compliance, the repository by rule matrix, a team summary, policy notes (disabled rules, lowered severities, exceptions and their deadlines), per-repository action lists, `fleet.json`, `fleet.csv` and a history entry.
- **Organizations and teams.** Catalogs load from local paths, https with a digest, or a git repository at a tag or commit. A catalog can lock rules, set severity floors and forbid exceptions; `--require` makes CI and fleet runs fail when a repository drops the baseline. A sources file maps `org` and `team/<name>` to catalogs, resolution order is fixed (presets, organization, teams, repository), and reports say which layer defined or weakened each rule.
- **Integrations.** A prek / pre-commit hook, a composite action for GitHub and Gitea Actions, and editor completion through the JSON Schema (`docs/integrations.md`).

### Known limits

- The workflow templates `fleetlint fix` writes, and this project's own release workflow, were validated locally but have not run on a real runner before this release (`docs/ROADMAP.md`).
- Gitea support (templates, release signing with a key, repository discovery) is untested against a Gitea instance.
- OCI catalog sources with signature verification and the presets `strict` and `oss` are not built yet.
