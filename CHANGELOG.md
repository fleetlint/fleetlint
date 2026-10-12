# Changelog

All notable changes to this project are documented here.
Format: Keep a Changelog. Versioning: Semantic Versioning.

## [Unreleased]

### Added

- `scripts/install.sh` installs a release for Linux or macOS, checked against `checksums.txt` and, with `cosign` installed, against the signature: `curl -fsSL https://raw.githubusercontent.com/fleetlint/fleetlint/main/scripts/install.sh | sh`.
- `ci/gitlab/fleetlint.yml`, a GitLab CI job that reports findings in the merge request's Code Quality widget.
- `fleetlint version` prints what `--version` prints; before, `version` ran a check.
- `fleetlint catalog version` shows the catalog version the repository runs with (built in or pinned) and each catalog it loads, with layer and digest.

### Changed

- The composite action in `action/` is deprecated in favour of [fleetlint/action](https://github.com/fleetlint/action), which installs a verified release instead of building with Go and annotates findings.

### Fixed

- `--from` reads every page of a forge's repository list: paging follows GitHub's `Link` header and Gitea's `X-Total-Count`, so a server that caps the page size below 50 no longer cuts the list after the first page. A rate limit is reported as such, with its reset time.
- Merging into a JSON file no longer rewrites `&&`, `<` and `>` in it as `\u0026`, `\u003c` and `\u003e`.
- Several fixes to one file all land: each edit applies to the file as the previous one left it, instead of the content seen when planning.
- A directory that is no git repository but holds repositories is no longer checked as one repository; fleetlint points to `fleetlint fleet --discover` instead. Running `fleetlint` in a directory whose `go.work` lists separate checkouts failed with `unknown field "extends"`.
- A workspace member with its own `.git` (a submodule or a separate checkout) is a repository, not a scope of the enclosing one.

### Security

- Local catalogs and sources files named in `.fleetlint.yaml` must be inside the repository. An absolute path, `..` or a symlink to elsewhere is refused, so a repository's configuration can no longer read other files (such as `/proc/self/environ`) into error messages and reports. Catalogs shared between repositories come from https, git or OCI.
- Reads of repository files go through `os.Root`: a symlink that leads outside the repository reads as absent, and only regular files are read, so a link to `/dev/zero` or a FIFO no longer exhausts memory or blocks.
- `fix --apply` writes through the same boundary: it never follows a symlink out of the repository, never writes inside `.git` (in any letter case), and creates new files exclusively. A catalog's fix actions are checked when they load: targets must be paths inside the repository outside `.git`, and `append` only extends TOML and YAML files and Gemfiles.

- `GITEA_TOKEN` goes only to Gitea hosts you name, in `GITEA_HOST` (comma-separated) or as the host of a `--from` source. Before, any remote host that was not github.com was taken for Gitea and sent the token. A host you did not name is not contacted at all; its repositories' visibility stays unknown, with a note how to name it.
- Clones of repositories listed by `--from` send the forge token only when the clone URL points at the forge itself.
- `--deep` runs `make check` with an environment without credentials (tokens, cloud keys, CI secrets), like command rules: the variables a shell and the common toolchains need, minus any whose name looks like a secret.
- On timeout the whole process group of a command rule, `--deep` or git is killed and waiting for its output is bounded, so a background process left behind can no longer hang a run.
- git runs without variables that redirect it to another repository (`GIT_DIR`, `GIT_INDEX_FILE` and the like, which git hooks export) and without fsmonitor.
- A fleet file `url` with credentials (`https://user:token@…`) is refused instead of being written into `fleet.json`, the HTML, CSV and history.

## [0.2.0] - 2026-10-11

### Added

- **Fleet triage.** The fleet report opens with the top levers: the rules that fail most repositories, errors first, with how many `fleetlint fix --apply` repairs and how much of the errors and of all failures fixing them closes. Repositories are listed most urgent first.
- **Fixable findings in fleet mode.** The fleet run marks what `fix --apply` can repair: a column per repository and per rule, and `*` in the matrix.
- Action lists (`check -f agent` and the fleet's `actions/`) start with `fleetlint fix --apply` when part of the work is mechanical, before the manual steps.
- **Repository context.** Each repository shows the branch and commit that were checked, how many uncommitted changes the working tree had, and the date of the last commit, marked stale after a year.
- **Facts to pin.** The tier and visibility of every repository where they were guessed or are unknown, with the line to add to `.fleetlint.yaml`.
- An inferred tier says so: the tier source is `inferred` with the deciding fact (`inferred from release pipeline`) instead of `default`, which remains for the plain tier 3.
- The fleet file takes `group:` per entry; the report has a group summary next to the team summary.
- **Discovery.** `fleet --discover <dir>` finds git repositories below a directory (four levels, not inside other repositories), names colliding ones by their path, groups them by the directory they sit in, and lists project directories that are in no repository of their own. `--exclude <glob>` skips paths.
- The HTML report has the same sections as the Markdown one (it lacked the rule table), renders inline code, colors compliance by threshold, sorts tables by column and filters the matrix.
- The HTML report and the action pages share one stylesheet with light and dark colors at readable contrast; links in dark mode were the browser's default blue on black, and the action pages had no dark mode.
- **Fixing across the fleet.** `fleet --fix` lists the mechanical fixes in every local repository, `--apply` writes them (nothing is committed) and the report shows the state after them; `--rule` limits both to one rule.
- **Since the last run.** The fleet report compares with the latest earlier run in `history/`: rules failing at error severity before and after, and per repository the rules that newly fail and those that were fixed.
- **Fleet-wide facts.** The fleet file sets `team`, `tier` and `visibility` for every repository under `defaults:` and per group under `groups:`, discovered ones included, and entries take `tier:`; what a repository pins in `.fleetlint.yaml` still wins. Fleet runs ask GitHub or Gitea whether a local repository is public when nothing says so, as `init` does, with one note per forge that would not answer. A fleet file entry for a repository that `--discover` also finds adjusts that repository instead of clashing with it.

- **Installation.** Homebrew (`brew install fleetlint/tap/fleetlint`), Scoop, a Nix flake (`github:fleetlint/nur`), `.deb`, `.rpm`, `.apk` and Arch packages on every release, and a signed multi-arch container image `ghcr.io/fleetlint/fleetlint` that includes git. Packages carry an SBOM each, like the archives.

### Changed

- **BREAKING** `fleet.json` stores each rule definition once under `definitions` and gives `version: 2`; a result's `rule` is the rule id, and `definition` names the stored definition when the repository adjusted it. For fifty repositories the file shrinks from about 10 MB to about 1.3 MB. Compatibility: readers of `results[].rule.id` read `results[].rule` and look the definition up.
- Rules that could not be evaluated no longer count against compliance; before, they lowered it like a failure.
- Rule errors have their own column and section and no longer count as failures in the rule table.
- The rule table and matrix leave out rules that apply to no repository (listed in one line) and tell apart a rule a repository does not select (`·`) from one that does not apply (`-`).
- `fleet.csv` gains the columns `group`, `tier_source`, `fixable`, `rule_errors`, `branch`, `commit`, `dirty`, `last_commit` and `stale`, after the existing ones.

### Fixed

- `json()` reads comments and trailing commas (JWCC, through `github.com/tailscale/hujson`), so a commented `tsconfig.json` no longer makes `lint/node-strict-flags` a rule error.
- The built-in catalog reports a tracked `.env` file or agent file with its path, no longer takes a Go function called `fit` or an encrypted `.env` file for a finding, and its agent instructions lose a literal `\n`.

### Security

- Built with Go 1.27.2 and `golang.org/x/net` v0.60.0, which fix HTTP/2 vulnerabilities (GO-2026-6610, GO-2026-6611, GO-2026-6612, GO-2026-6613, GO-2026-6617).

## [0.1.0] - 2026-10-05

First release. The configuration, catalog and JSON output formats may still change before 1.0; changes will be listed here with a compatibility note.

### Added

- **Checks.** `fleetlint check` evaluates a repository against a catalog of rules about its setup: repository hygiene, the task-runner contract, git hooks, linter configuration per stack, CI workflows, the release pipeline, dependencies, containers, documentation and what a public repository needs. Zero configuration: stacks (Go, Python, Rust, Node, Kotlin, Flutter), forge, tier and layout are discovered. Exit codes 0 (clean), 1 (findings), 2 (configuration error), 3 (internal error).
- **Catalog.** 94 rules in three presets, maintained in their own repository ([fleetlint/catalog](https://github.com/fleetlint/catalog)) and embedded in the binary; `fleetlint --version` and every report (table, JSON `tool.catalog`, Markdown, agent, SARIF, fleet) name the catalog version that was built in: `fleetlint:minimal`, `fleetlint:recommended` and the add-on `fleetlint:slop` with heuristics for the traces careless or generated code leaves behind. The practices behind them, and a generated page per rule, are at [fleetlint.org](https://fleetlint.org/) (source: fleetlint/docs).
- **Rules as data.** Every rule in the presets except the one that runs the repository's check is data in the catalog. Rules are YAML with [CEL](https://cel.dev) predicates over discovered facts and file accessors ([reference](https://fleetlint.org/cel-reference/)). Kinds: `expr`, `foreach` (one finding per element), `outcome` (several tools can satisfy a requirement; a repository adds its own with `accept:`), and `command` (repository-local only, with `--deep`).
- **Configuration.** `.fleetlint.yaml`: `extends`, pinned `facts`, `scopes`, rule overrides, inline rules, and exceptions with a reason and an optional deadline. Disabling a rule or lowering its severity needs a reason, and both show up in every report. `fleetlint init` writes the file; `fleetlint schema` prints its JSON Schema.
- **Monorepos and nested projects.** Workspace members (go.work, pnpm, package.json workspaces, Cargo, melos, Gradle) and top-level directories with their own manifest are checked as scopes, each with its own stack, with optional nested `.fleetlint.yaml` files.
- **Fixes.** `fleetlint fix` plans the mechanical part (create a file from a template, untrack files, add ignore patterns, set a value in a JSON or TOML manifest) and `--apply` writes it; existing files are never overwritten and existing keys never changed. Hook configuration and check workflow are assembled for the root stack and every nested project, and they run fleetlint itself (`quality/policy-enforced` requires that something does). `repo/dev-environment` asks for a reproducible tool setup.
- **Tool alternatives.** The task runner (`facts.taskrunner`: make, just, task, package.json scripts, Gradle) and where its targets run (`facts.container`: none, devcontainer, docker, podman) are facts: detected, and pinned in `.fleetlint.yaml` when wanted. Rules read every runner through one model, and `fix` writes the runner file, hooks and check workflow for the chosen combination; in a container mode a target called outside the container runs inside it (`CONTAINER=0` stays on the machine). Rules that depend on a tool (`ci/check-workflow`, `lint/go-config`, `deps/vulnerability-scan`, `deps/license-check`, `taskrunner/container`, `repo/dev-environment`) are outcome rules: they list the known alternatives and take more through `accept:`.
- **Baseline.** `fleetlint baseline` grandfathers today's findings so only new ones fail; the baseline only shrinks.
- **Output.** Table, JSON, Markdown (pull-request comment), SARIF (code scanning) and an `agent` format: an ordered task list with a precise instruction per rule for a coding agent. `fleetlint explain <rule>` prints one rule in full.
- **Fleet mode.** `fleetlint fleet` checks many repositories (paths, a fleet file, or `--from github:<owner>` / `--from gitea:<host>/<owner>`) and writes a static report: repositories with compliance, the repository by rule matrix, a team summary, policy notes (disabled rules, lowered severities, exceptions and their deadlines), per-repository action lists, `fleet.json`, `fleet.csv` and a history entry.
- **Organizations and teams.** Catalogs load from local paths, https with a digest, or a git repository at a tag or commit. A catalog builds on others with `includes` and adjusts their rules with `overrides:`: lock a rule, set a severity floor, forbid exceptions, change severity or parameters, or disable it with a reason, without restating the rule. `--require` makes CI and fleet runs fail when a repository drops the baseline. A sources file maps `org` and `team/<name>` to catalogs, resolution order is fixed (presets, organization, teams, repository), and reports say which layer defined or weakened each rule.
- **Catalog versions.** `catalog: {version: …}` in `.fleetlint.yaml` uses another version of the built-in catalog (rules and templates) for that repository. It is fetched once with git and cached, so later runs are offline. Catalogs state an engine level; one the binary cannot run is refused with a single message.
- **Dependency updates.** `fix` writes a `renovate.json` that covers dependencies, pinned actions, hook revisions and tool versions annotated with `# renovate:` comments; fleetlint's own tool pins carry those comments.
- **Integrations.** A prek / pre-commit hook, a composite action for GitHub and Gitea Actions, and editor completion through the JSON Schema ([integrations](https://fleetlint.org/integrations/)).

### Known limits

- The workflow templates `fleetlint fix` writes, and this project's own release workflow, were validated locally but have not run on a real runner before this release (`docs/ROADMAP.md`).
- Gitea support (templates, release signing with a key, repository discovery) is untested against a Gitea instance.
- OCI catalog sources with signature verification and the presets `strict` and `oss` are not built yet.
