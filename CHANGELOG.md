# Changelog

All notable changes to this project are documented here.
Format: Keep a Changelog. Versioning: Semantic Versioning.

## [Unreleased]

### Added

- `--forge` and `--token` on `init`, `fix` and `fleet` (or `FLEETLINT_FORGE` and `FLEETLINT_TOKEN`, which keep the token out of the process list). `--forge` takes the `--from` forms without the owner: `github`, `github:<host>`, `gitea:<host>`, `gitlab:<host>`, an https URL with a path prefix (`gitlab:https://git.example/scm`), or a bare https URL whose kind the remote decides. Naming a forge trusts its host and sets its kind and API root; the token goes to the named forge (and the `--from` hosts) only, and also authenticates the push to an https remote, as an Authorization header scoped to the host.
- CI servers are known without naming them: in Gitea and Forgejo Actions the job's server is a Gitea forge with the API of `GITHUB_API_URL` and the job's `GITHUB_TOKEN`; in GitLab CI the job's server is a GitLab forge with the API of `CI_API_V4_URL` (merge requests need `GITLAB_TOKEN` or `--token`: the job token cannot open them).
- `--pr` names the remote's own host, so a pull request on Codeberg or a self-hosted forge needs a token but no `GITEA_HOST` or `GITLAB_HOST`; `fleet --pr` names the hosts of its `--from` sources the same way.


- **Ownership from the repository's files.** `fleetlint facts` and the JSON name the owner (`owner`, `owner_from`): Backstage `catalog-info.yaml` `spec.owner` (`group:default/payments` is `payments`), else the default owner of CODEOWNERS (the first owner of the last `*` rule, in `.github/`, the root, `docs/`, `.gitea/` or `.gitlab/`). In fleet mode a repository without a `team:` of its own in the fleet file gets that owner as its team, before a group's or the defaults' team; `team_from` in `fleet.json` says which. Read from files only, no forge call.
- **Exception policy.** A catalog's `exception_policy: {require_until, max_days, require_approver}` binds the exceptions it and every later catalog declare, the repository's and its scopes' included; policies add up (a later one can tighten, never loosen), and an exception that breaks one is a configuration error naming the catalogs that set it. `max_days` counts from the day of the run.
- `approved_by` on an exception names who agreed to it. It is shown in the table, the fleet report's exceptions and SARIF (`properties.approved_by`); fleetlint records it and does not verify it.

- `fleetlint fix --apply --pr` proposes the fixes as a pull request instead of writing the working tree: they are made on the default branch in a temporary worktree, committed on `fleetlint/fixes` (as whoever runs fleetlint, with the repository's git hooks off), force-pushed, and the open pull request from that branch is updated, or one is opened, with the changes and the report as its description. A protected default branch and uncommitted work are both fine. The forge's API is used when `GITHUB_TOKEN`, `GITEA_TOKEN` or `GITLAB_TOKEN` is set, else gh, tea or glab.
- `fleetlint fleet --fix --apply --pr` does the same per repository, cloned ones included, and notes each pull request in the report.
- GitLab in the forge client: `fleet --from gitlab:<host>/<group>` lists a group's projects with its subgroups' (a user's when no group has the name), the visibility of a GitLab remote is looked up (nested namespaces too), and `init --pr` and `fix --pr` open merge requests. Self-hosted hosts are named in `GITLAB_HOST` before fleetlint contacts them or sends `GITLAB_TOKEN`; gitlab.com is known.

- `fleetlint fleet --jobs N` (default 4) checks that many repositories at once. A panic while checking one repository is reported as that repository's error and the run goes on.
- `fleetlint fleet --keep-history N` keeps only the latest N runs in `history/`. History files are written as compact JSON, and a `--fix` run without `--apply` adds none.
- The fix plan names the catalog a change's template came from when it is not the run's catalog source (`(template from catalog oci://...)`), since such a template becomes a hook or a workflow.
- SARIF `fixes`: a failing result that `fleetlint fix --apply` can repair carries the change it would make, one replacement per file (`deletedRegion` in UTF-16 code units, the run's `columnKind`, and the inserted text), on the rule's first finding. Created files, appends and edits are included; a rule whose fix untracks a file or changes one file twice has none.

### Changed

- The baseline counts fingerprints: a finding recorded twice covers two findings with that fingerprint, so a new copy of a grandfathered finding fails instead of hiding behind the old one, and `baseline` keeps one entry per copy. Existing baseline files keep working (each entry counts once).
- A scope whose tier no configuration or fleet file sets takes the repository's tier when that is more serious (`inferred from the repository (release pipeline)`): a workspace member of a released repository was inferred as tier 3 because the workflows live at the root.
- Visibility the forge reported through its API has the source `forge` in `fleetlint facts` and the JSON, instead of `detected`.

### Fixed

- An exception whose `match: re:...` does not compile is a configuration error; it used to load and never match.
- `fleetlint init` replaces control characters in the workspace member names it writes into a comment of `.fleetlint.yaml`, so a directory name with a newline cannot add a key.

## [0.8.1] - 2026-10-21

The release of 0.8.0 did not run: its verify job failed twice on a flaky test, so 0.8.0 is a source tag without binaries or packages. 0.8.1 is 0.8.0 with that test fixed; nothing in fleetlint itself changed.

### Fixed

- Tests turn off git's automatic gc and maintenance in their repositories' own config. The code under test runs git with its own environment, so a push into a test's bare remote started a background gc that kept writing after the test ended and broke the cleanup on CI runners.

## [0.8.0] - 2026-10-21

Engine level 7: a catalog that names artifact instances or sets `locate:` needs this version, and one written for level 7 may not use `combine:` or `artifact_workflows()`. Catalogs for levels 1 to 6 still run, with both.

### Added
- `fleetlint fleet --deep-clones`: with `--deep`, also run the rules that execute repository code in cloned repositories (url entries, `--from`).
- `--allow-dirty` on `fleetlint fix --apply` and `fleetlint fleet --fix --apply`: write into a working tree that has uncommitted changes.

- **Published artifacts are instances with names.** An artifact definition's `instances:` expression names what tells one artifact of a kind from another (an image reference, a chart path, a registry reference), evaluated per publishing workflow with `item` bound to it. An implementation keyed by artifact and written in CEL runs once per published artifact with `artifact` bound (`{kind, name, workflows}`); a Go or command implementation runs once. Its own `when` is asked per artifact. A finding carries the artifact's name in its own field (`artifact`), which reports show in front of the message (`ghcr.io/x/b: the image is not signed`); the evidence names the artifacts that pass. `facts.artifacts` takes `{kind, name, workflows}` entries next to bare kinds, so a pinned artifact can name the workflows the rules read; entries of the same kind and name are merged, and a pinned workflow that does not exist is refused. `fleetlint facts`, the check JSON (format 5) and `fleet.json` (format 6) carry the names.
- CEL `find(text, regex)` and `findall(text, regex)`: the first capture group of the first or of every match.
- **`locate` on a rule**, a CEL expression yielding the file a finding that names no path is about (`'"README.md"'`, `taskrunner.file`, `artifact.workflows[0]`). Its result is the finding's `anchor`, checked at run time to stay inside the repository (a locate that fails or escapes anchors nothing and says so in the evidence). Reports point there: the table and the agent output name the file in front of the finding, and SARIF uses it as the location instead of the scope's configuration file. The anchor is presentation only: the finding's path, fingerprint, exceptions and fixes stay as they were. A catalog test keeps the list of rules without it from growing (`internal/catalog/testdata/rules_without_locate.txt`).
- SARIF: one run per scope, keyed `fleetlint/<repository>/` for the root and `fleetlint/<repository>/<scope>/` for a scope; code scanning takes an id up to its last `/` as the category, so every scope keeps its own alert set. Uploading after the upgrade moves a repository's alerts to the new categories once. An exception's expiry is on its suppression (`properties.until`, and in the justification).
- `scripts/install.sh` verifies the build provenance with `gh attestation verify` when cosign is absent and gh is present and authenticated (`gh auth status` succeeds, or `GH_TOKEN`/`GITHUB_TOKEN` is set); with neither it says that only the checksum is checked.
- A `catalogs:` entry in the sources file may be `{ref, signers, min_version}` instead of the bare reference: `signers` verify that `oci://` catalog alone, in place of the top-level signers; `min_version` is a semantic-version floor on the catalog's `metadata.version`, so a tag moved back to an older release fails the run naming the alias, the loaded version and the floor. `fleetlint schema sources` describes both forms.

### Changed

- A release finding about a named artifact has a baseline fingerprint and SARIF fingerprint of its own (the name is part of it), so a baseline written by 0.7 does not cover it; `fleetlint baseline --reset` grandfathers it again. Every other fingerprint is unchanged: `locate` sets where a report points, not the finding's path.
- The deprecated `action/` directory is gone; the action is `fleetlint/action`.
- `scripts/install.sh` speaks https with TLS 1.2 or later only, accepts only `vX.Y.Z`, `X.Y.Z` or `latest` as the version, and takes the checksum line whose file name is exactly the archive's (`checksums.txt` also lists the archive's `.spdx.json`); the GitLab CI job fetches it with `--tlsv1.2` too.
- `combine:` and `artifact_workflows()` are gone from the rule format at engine level 7: a catalog written for level 7 is refused with either, inline or from its library (`artifact_workflows(kind)` read every artifact of the kind inside an implementation meant to check one; `artifact.workflows` is the one being checked). A requirement one stack's tooling meets for the whole repository is a rule at the root with the union of the satisfiers; one that differs per published artifact is a capability keyed by artifact. Catalog 0.5.0 does both: `changelog/generator-configured` is one rule, `release/reproducible-build` is keyed by artifact.

### Security
- `fleet --deep` no longer executes code from cloned repositories: url entries and discovered repositories are checked without their deep rules unless `--deep-clones` is given, and the report notes it per repository (`notes` in `fleet.json`, a Notes column in the report).
- `fix --apply` refuses a working tree with uncommitted changes to tracked files (exit 2) unless `--allow-dirty` is given (untracked files, such as the configuration `init` just wrote, are in no danger and do not count); `fleet --fix --apply` leaves such a repository unfixed, still checks it and records the refusal in its notes. A repository without git is never dirty.
- Clone cache directories follow the URL's host and path (`<cache>/<host>/<owner>/<repo>-<hash>`), each segment sanitised on its own and the last one suffixed with 8 hex characters of the SHA-256 of the unsanitised host and path, so two URLs can no longer share a clone (`a/b_c` and `a_b/c` did; `we%20ird` and `we_20ird`, or `Org/Repo` and `org/repo` on a case-insensitive filesystem, would have). Existing clones at the old flat path are not reused; the next run clones afresh, and the old directories can be deleted.
- Repositories discovered with `--from` are cloned only from `https://`, `ssh://` or `git@` URLs on the forge's own host (for GitHub Enterprise, also the host of `GITHUB_API_URL`); a clone URL elsewhere, a local path among them, fails the run naming the repository. Discovered names were already sanitised like fleet-file names; a test now pins that.

- The sigstore TUF root fetch runs under the run's context with a two-minute limit instead of unbounded.
- An OCI signature referrer yields at most 8 bundle layers; more is an error, as more than 32 referrers already was.
- A git catalog directory with its templates/ is refused above 16 MiB in total, not only per file.
- Plain http to a registry is decided by the exact host `localhost`, `127.0.0.1` or `::1` (with or without a port), no longer by the `localhost` prefix that `localhost.evil.tld` matched.
- The uncommitted-changes guard of `fix --apply` and `fleet --fix --apply` fails closed: when `git status` fails (a corrupt or unreadable index), nothing is written and the message names the git error (`--allow-dirty` writes anyway).
- `fleet` normalises every repository the same way, positional paths and `--discover` finds included: a directory name with `#`, `?` or a space becomes a safe name, the same in `actions/` and `sarif/`, and names that collide after sanitising are refused.

## [0.7.0] - 2026-10-21

Engine level 6: a catalog whose `refs` are `{url, kind, section}` entries needs this version. Catalogs for levels 1 to 5 still run.

### Added

- Catalog 0.5.0: named artifact instances for images, charts and OCI artifacts, `locate` on 189 rules, `release/reproducible-build` keyed by artifact, engine level 7.
- Catalog 0.4.0: every rule names its source with a typed ref.
- **Typed sources.** A rule's `refs` entry is a URL or `{url, kind, section}`, where `kind` says what the source is, strongest first: `spec` (a language or format specification, an official tool), `guidance` (official guidance such as Go Code Review Comments or PEP 8), `style-guide`, `standard` (SSDF, SLSA, Scorecard, ASVS, SARIF), `check` (a linter's check, `section` its id) or `convention` (this catalog's own, no outside source). The rule pages get a Sources section, `fleetlint explain` lists them, the check JSON (format 4) and `fleet.json` (format 5) carry them as objects. A catalog test keeps the list of rules that cite only fleetlint.org from growing (`internal/catalog/testdata/rules_without_source.txt`).
- SARIF output completed for code scanning: every result has a location (a finding without a path is anchored to its scope's `.fleetlint.yaml`, else `README.md`), `partialFingerprints` (`fleetlint/v1`) so alerts keep their identity across runs, `helpUri` to the rule's page on fleetlint.org, the family as a tag and, for the security families (`release`, `ci`, `deps`, `containers`, `hooks`, `repo`), a `security` tag with `security-severity`; the rule's `refs` in its properties; `automationDetails.id` `fleetlint/<repository>`; excepted and baselined findings as suppressed results with their reason; rule errors as invocation notifications (`executionSuccessful` false) instead of results. The output is validated against the OASIS SARIF 2.1.0 schema in the tests.
- `fleetlint fleet --out <dir>` writes `sarif/<repository>.sarif` per checked repository, keyed `fleetlint/<fleet name>`.

## [0.6.2] - 2026-10-21

### Added

- Catalog 0.3.1: the artifact kind `oci-artifact` (a policy bundle, a catalog, a WASM module pushed with `oras push`, `flux push artifact`, `cosign upload blob` or `regctl artifact put`, in the step or in a script it runs) with signing, SBOM and provenance implementations; the `image` kind sees GoReleaser `dockers_v2:` and `kos:`; `release/checksums` accepts a registry-bound release.
- `TestOCIIntegration` loads a signed catalog version from ghcr.io through the public sigstore root with SCT checking (opt-in with `FLEETLINT_OCI_TEST_REF`; weekly in CI against the catalog version in go.mod).

### Fixed

- A binary installed with `go install` reported its version as `dev`; it now reports the module version from the build info.
- The not-applicable evidence of an artifact capability names the artifact kind ("the artifact kind npm") instead of guessing an article.

## [0.6.1] - 2026-10-21

### Security

- Binaries are built with Go 1.27.3: GO-2026-6809 (crypto/x509 trusted compromised intermediate certificates) sat on the path that verifies catalog signatures in 0.6.0. google.golang.org/grpc 1.83.2 closes a vulnerability the binary does not call.

## [0.6.0] - 2026-10-21

Engine level 5: catalog 0.3.0 or newer. Catalogs for levels 1 to 4 still run.

### Added

- **Artifacts are catalog data.** `artifacts/<id>.yaml` in a catalog says what a release publishes (files on a forge release, a container image, an npm package, ...) and how it is detected: a CEL expression returning the workflows that publish it, evaluated once per repository. The result is the fact `artifacts` (`fleetlint facts`, the JSON, `repo.artifacts` and `artifact_workflows(kind)` in CEL); `facts: {artifacts: [...]}` pins it when detection misses a publish step (a reusable workflow, say), and an empty list says nothing is published.
- **Capabilities keyed by artifact.** A capability with `by: artifact` has one implementation per artifact kind (`rules/<family>/<name>/<artifact>.yaml`) and is checked for every kind the release publishes; each must pass, so "binaries signed, npm package not" is a finding on the npm implementation. A kind without an implementation is reported as a gap, a repository that publishes nothing detectable gets a not-applicable note on the capability instead of a silent pass, and `fleetlint docs` writes an artifact matrix. There is no `any` implementation for such a capability.
- **`fleetlint hierarchy`** prints the catalogs of the run in the order they applied: how each was referenced, its label, name and version, the digest or artifact that pins it, what it builds on and what it contributed (`--format table|json|markdown`). The same list is in the check JSON (`catalogs`, format 3), the table and agent output, the Markdown comment, the SARIF driver properties, and per repository in `fleet.json` (format 4) with a Catalogs section in the fleet report.
- **Fleet files include fleet files** (`include: [teams/platform/fleet.yaml]`): the included file's defaults and groups settle its own repositories first, the including file's defaults fill the rest. Groups nest: `--discover` records the full directory path (`example/tooling`), settings apply from the group up through its parents to the defaults, and the group summary rolls every level up.

### Changed

- **Catalogs apply in written order.** The fixed layers (preset, org, team, repo) are gone: catalogs apply in the order `includes` and `extends` list them, the sources file's baseline first, nothing reordered. Policy only tightens, so an earlier catalog binds the later ones; a team catalog that includes the organisation's always comes after it. The repository's own file still comes last. `metadata.layer` is now `metadata.label`, a free name (`org`, `team/mobile`, `security-team`) for attribution in reports and for `--require`; the old key is read with a deprecation. Sources-file names are free too (`compliance/pci`). Every `layer` field in outputs is `label`. Two versions of a catalog with the same name in one run are a load error.
- `release/signing`, `release/sbom`, `release/provenance` and `release/trusted-publishing` of the fleetlint catalog are keyed by artifact; the stack-keyed ids are aliases.

## [0.5.1] - 2026-10-15

### Fixed

- `fleetlint docs` shows each capability's `combine` (a column of the capability matrix, a sentence on its page) and says that the stack-neutral implementation also runs where no stack is detected.

## [0.5.0] - 2026-10-15

### Added

- **`combine:` on a capability** says how its implementations add up in a scope: `all` (the default) needs every implementation that runs to pass, `any` needs one. With `any`, the implementations that failed pass once one passes, with the evidence `satisfied by <id> (combine: any)`; errors stay errors. For what belongs to the repository rather than to each stack, such as a release: a Go CLI published as an npm package with provenance passes `release/provenance` for its Go side too. Implementations inherit it and may not set it; the catalog schema has it.

### Changed

- **Engine level 4.** Catalogs that use `combine:` state level 4 and need this fleetlint; an older one refuses them with one message instead of failing on an unknown key. Catalogs for levels 1 to 3 still run.
- The stack-neutral implementation (`any.yaml`) also runs where no stack is detected, so every repository is checked by something: a repository that only releases a container image gets `release/signing`. A capability for which that means nothing says so with a `when` on its `any.yaml`.

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
