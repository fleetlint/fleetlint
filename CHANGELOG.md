# Changelog

All notable changes to this project are documented here.
Format: Keep a Changelog. Versioning: Semantic Versioning.

## [Unreleased]

## [0.4.1] - 2026-10-14

### Added

- The HTML fleet report links each rule id to its documentation (its page on fleetlint.org, or the rule's first reference for an organisation's own rule) and shows the rule's title, severity and requirement on hover.

## [0.4.0] - 2026-10-14

### Added

- **Capabilities with one implementation per stack.** A rule of `kind: capability` states a requirement (`lint/configured`: a linter is configured); its implementations at `rules/<family>/<name>/<stack>.yaml` check it with the stack's tools and inherit every field they do not set. `any.yaml` is the stack-neutral implementation: it checks stacks without their own, and its satisfiers count for every stack. `use:`, overrides, exceptions and policy on the capability reach all its implementations; `lint/configured/go` adjusts one. A detected stack without an implementation is reported as not applicable with `no implementation for <stack>`, instead of passing unnoticed. Rule ids may have a third segment for that. `fleetlint docs` writes a capability matrix (`rules/capabilities.md`).
- **Stacks are catalog data.** The catalog's `stacks/<id>.yaml` defines how a stack is detected (file and glob markers, content checks, `unless_stack`) and what `fleetlint fix` needs for it (dev container feature or image, the image targets run in, Renovate lockfile maintenance). A new stack is a catalog change. Stacks named in `.fleetlint.yaml` or on rules must be defined by the catalog. Catalogs without `stacks/` keep the detection of 0.3.
- **A catalog version from a registry.** `catalog: {oci: ghcr.io/fleetlint/catalog:v0.1.0}` runs the catalog version published as an OCI artifact (presets, library, stacks, templates, renamed ids), as `catalog: {version: v0.1.0}` does from git; the two are the same version for catalogs that build on it. A tag of the fleetlint catalog is verified against the signature of its publish workflow, any other tag against the sources file's signers; a `@sha256:` digest pins the content. Extending such an artifact explains how to pin it instead. The sources file and `metadata.catalog` accept `oci` too.
- **GitLab CI and Forgejo Actions.** `workflows()` returns `.gitlab-ci.yml` (and `.gitlab-ci.yaml`) with the local files it includes, and `.forgejo/workflows/*`. `jobs()`, `steps()`, `triggers()` and `tagged_workflows()` read GitLab pipelines into the shape they give Actions workflows: a job's scripts are one step, `timeout:` becomes `timeout_minutes`, merge request pipelines read as `pull_request`, rules on `$CI_COMMIT_TAG` make a release pipeline. New: `ci_kind(path)` (`github`, `gitea`, `forgejo`, `gitlab`) and `includes(path)` for GitLab `include:` entries. `repo.ci` reports `forgejo-actions`.

### Changed

- **Engine level 3.** Catalogs written for level 3 need this fleetlint; catalogs for levels 1 and 2 still run.
- The presets take their version from the catalog release they ship in (the tag, or the commit), so `selected_by` and reports read `fleetlint-recommended@v0.1.0`; a preset that states a version keeps it. Other catalogs still state theirs.
- `codegrep` reads source under `packages/` (JavaScript monorepos were skipped entirely) and Ruby, PHP, C#, F#, C, C++, Vue, Svelte and Scala files. Test files also include `e2e/`, `cypress/`, `*.cy.*`, `*_spec.rb`, `tests.py`, `conftest.py` and `*Tests.cs`.
- A Gradle build without the Kotlin plugin is a `java` stack, no longer `kotlin`; the Java dev container installs Gradle.
- The Hugo image for `container: docker|podman` is pinned to a release instead of `latest`.
- `fleetlint fix` turns the toolchain caches off in the release workflow it writes (a cache a pull request could fill must not feed signed artifacts), and the renovate.json it writes waits three days before proposing a new release (`minimumReleaseAge`).
- Disabling a capability is listed once under its id, not once per implementation.
- **Renamed rule ids keep working.** An old id (`lint/go-config` from 0.3) in `overrides:`, `exceptions:` or `use:` reaches the rules that replace it, and the run prints a deprecation warning naming them. The catalog lists the renames in `aliases.yaml`.
- In a workspace member or nested project, `workflows()` and `tagged_workflows()` return the repository's CI files as `../` paths: CI lives at the root, and per-stack rules in a member see it.
- Makefile recipes are read with simple variables expanded (`$(GO_LICENSES) check` reads as the command it runs), so `taskrunner.recipes` shows the tools a target really invokes.
- `unless_stack` in a stack definition takes a list, and stacks are decided in that order; an unknown stack there fails the load.
- Capabilities and implementations live in a catalog's `rules/` library only. A rules list (a catalog's or the repository's) refuses a capability or an `<capability>/<stack>` id: an inline implementation could otherwise satisfy a locked capability.
- An implementation inherits the capability's `params` as defaults. A catalog library that redefines a capability rebuilds the shared implementations on its definition, so its severity, policy and gate reach them. A partial in an implementation states its severity.
- Overriding an implementation of a capability that is switched off is not an error; `accept:` on a capability without outcome implementations is.

### Fixed

- Signed OCI artifacts on registries without the referrers API (ghcr.io) are verified: cosign lists their bundles under a `sha256-<digest>` tag with the empty config type, which was skipped as "no sigstore bundle". At most 32 referrers are read, and an artifact's files are fetched 16 at a time: a catalog version of 320 files took over the two-minute limit from ghcr.io one by one.
- A `foreach` item the expression builds itself (a map literal, `map()`, `flatten()`) locates its finding with path, line and label like an item an accessor returns; the finding showed the dumped map before.
- `jobs()` reports `runs_on` when `runs-on` is a list or a `{group, labels}` map. `commits()` has `root`, true for a commit without parents.
- `fleetlint init` detects stacks with the catalog's definitions.
- A GitLab `include: local:` without a wildcard names one file, not every file of that name in the tree; a `timeout:` in days or as bare seconds is read.
- A capability's own `when` also applies to the "no implementation" note.
- A workspace glob that reaches into a test or fixture directory (`__tests__`, `fixtures`, `__mocks__`, `testdata`) does not make a member there, and Go sources next to a go.mod do not make each directory a nested project.
- JSON, YAML and TOML files that start with a byte-order mark are read; a `package.json` saved by a Windows editor made rules error before.
- `repo.hooks` recognises lefthook under all its file names (`lefthook.yml`, `.yaml`, `.toml`, `.json`, `.jsonc`, with or without a leading dot).

## [0.3.0] - 2026-10-13

### Changed

- **Stacks and task runner are set per directory, under `scopes:`.** `path: .` is the repository root: `scopes: [{path: ., stacks: [go], taskrunner: make}, {path: web, stacks: [node]}]`. `facts:` holds what applies to the whole repository: tier, visibility, forge, container. A root listed alone adds no scope, and workspace members are still discovered. Before, `facts.stacks` and `facts.taskrunner` applied to every scope too, so a member had to restate its own stack. The 0.2 forms are still read for this release and print a deprecation warning (in `fleet`, per repository): `facts.stacks` and `facts.taskrunner` now pin the root only, and a scope's `facts: {stacks: ...}` is read as its `stacks:`.

- **Output formats.** Every field of the check JSON and of `fleet.json` is snake_case. `summary`, `scopes`, `disabled`, `weakened` and `catalogs` used Go field names (`Pass`, `NotApplicable`, `Layer`) before. The summary's count of rules that could not run is `rule_errors`, apart from `errors`, the error-severity failures. The check JSON is now `version: 2` and `fleet.json` is `version: 3`. Both shapes are pinned by golden tests, so a change needs a new version. The fleet report still compares with history from earlier versions; it picks the format by `version` and passes over files it cannot read instead of failing the run.
- CEL expressions are type-checked when the configuration loads, for every rule, whether it applies to the repository or not: a typo is a configuration error (exit 2) naming the rule and the field (`when`, `expr`, `foreach`, a satisfier or partial). It was a rule error before, and only where the rule applied. Each expression is checked once per process instead of once per scope and repository.
- An expression evaluation stops at a cost limit and when the run is cancelled, so a catalog's comprehension over every file cannot run for hours. Regular expressions in `matches`, `grep`, `codegrep` and `scan` are compiled once instead of once per item.
- `exists()`, `glob()` and stack detection match against one file index per repository: with git, the tracked files plus the untracked ones git does not ignore, so a gitignored local file no longer makes a rule pass on a developer's machine and fail in CI. The tree was walked on every call before, and included ignored files. Vendored and build directories stay out, as before.
- A rule id must be `family/name` in lowercase letters, digits and dashes, as the schema always said; any `/` was accepted before. A repository's own rules may not set `locked`, `min_severity` or `exceptions` (there is no layer below them to bind); they were accepted without effect. The `.fleetlint.yaml` schema gained `refs` and `since` on rules and `exceptions` on scopes, which the loader already read.
- A rule's `accept:` satisfiers are their own field (`accept` in the JSON output) instead of an entry in `params`.
- `fleet.json` names, per repository, the catalog version it pinned (`catalog`), next to the built-in one the report names at the top.

### Added

- `fleetlint schema catalog` and `fleetlint schema sources` print JSON Schemas for catalog files and sources files, next to the one for `.fleetlint.yaml`. Tests hold all three to the types the loader decodes into, in both directions.

- `scripts/install.sh` installs a release for Linux or macOS, checked against `checksums.txt` and, with `cosign` installed, against the signature: `curl -fsSL https://raw.githubusercontent.com/fleetlint/fleetlint/main/scripts/install.sh | sh`.
- `ci/gitlab/fleetlint.yml`, a GitLab CI job that reports findings in the merge request's Code Quality widget.
- `fleetlint version` prints what `--version` prints; before, `version` ran a check.
- `fleetlint catalog version` shows the catalog version the repository runs with (built in or pinned) and each catalog it loads, with layer and digest.

### Changed

- The composite action in `action/` is deprecated in favour of [fleetlint/action](https://github.com/fleetlint/action), which installs a verified release instead of building with Go and annotates findings.

### Fixed

- A rule that cannot read its input is a rule error, never a pass: `text()`, `lines()`, `scan()` and `yaml()`/`toml()`/`json()` fail for a file that exists but cannot be read (too large, not a regular file, a symlink out of the repository) instead of returning an empty value, and `commits()` fails when `git log` does. A command rule that exits 1 without printing a finding is a rule error (most often a crash), and command rules need a git repository whose tracked files can be listed.
- A pinned catalog version now brings its rule library (`rules/`) along with presets and templates, so presets that select rules from the library work with a pin.
- Every catalog file of an OCI artifact takes effect. All of them carried the artifact's digest before, and only the first was loaded.
- `--from` reads every page of a forge's repository list: paging follows GitHub's `Link` header and Gitea's `X-Total-Count`, so a server that caps the page size below 50 no longer cuts the list after the first page. A rate limit is reported as such, with its reset time.
- `init --pr` says whether the push of `fleetlint/onboarding` worked when it cannot open the pull request: either the branch is pushed and only the pull request is missing (no `gh` or `tea`, or they failed), or the push failed and the message names the command to push it. Before, both cases read the same.
- A `.csproj`, `.sln` or `.gemspec` below `testdata/`, `examples/` and the like, or more than two directories down, no longer marks the repository as .NET or Ruby.
- Within a scope, a workspace member with its own `.git` is recognised as well (the check looked relative to the repository root).
- The built-in rule library is parsed once per process; in fleet mode it was parsed again for every repository and kept in memory each time.
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
- The release workflow verifies in a separate job without write permissions: the PyPI and Go tools of `make check` no longer run next to the signing identity and the deploy keys. The release job restores no build cache and pins GoReleaser to an exact version; GoReleaser runs `go mod verify` instead of `go mod tidy`. The verification command in the release notes names the exact signing identity of the tag.
- The GitLab job installs the release it belongs to, with the install script of that tag, and requires the cosign signature (`FLEETLINT_VERIFY=cosign`, which `scripts/install.sh` now understands). Before, it ran the install script from `main` and installed `latest` without a signature check. `make release` sets the version in the job.
- YAML from a repository or a catalog whose anchors and aliases expand to more than 200,000 nodes is refused before decoding. A few hundred bytes of nested aliases took minutes of CPU or tens of gigabytes of memory before.
- Text from the repository and its catalogs is inert in every output: the table drops terminal escapes and other control characters and keeps each finding on one line, so it cannot clear the screen, write the clipboard or start a GitHub Actions workflow command. Markdown (job summaries, pull-request bodies, `fleet.md`) escapes emphasis, links, images, HTML tags and @mentions.
- Policy cannot be faked from inside the repository. A catalog file in the repository belongs to the repository layer, whatever its `metadata.layer` says or a sources file calls it; only presets and catalogs fetched by digest, commit or signature can be org or team. `--require <name>` and `--require <alias>` count only such catalogs, so a local file named `acme-baseline` no longer satisfies `--require acme-baseline`; `--require <ref>#sha256-<digest>` is unchanged.
- A rule redefined by a later catalog keeps `exceptions: false` from the earlier one, which stays named as the one that set it; a redefinition that allows exceptions again is refused.
- `catalog.repo` in a sources file is validated like the one in `.fleetlint.yaml` (https or ssh git URL), and the loader checks it again before fetching, so a sources file can no longer point the run's catalog at a local path or another git transport.
- A pinned catalog version is read from git's objects instead of a checkout, so no filter or hook from the user's git configuration runs on fetched content. Each cache entry records a digest of its files and is fetched again when they no longer match, so a modified or poisoned cache (for example one restored in CI) is not used. Versions `a/b` and `a_b` no longer share a cache entry.
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
