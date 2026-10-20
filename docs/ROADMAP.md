# Roadmap

This file tracks what is next and what is blocked. User documentation is at [fleetlint.org](https://fleetlint.org/), from the fleetlint/docs repository.

## Verified on a real runner

v0.1.0 (2026-10-06) ran `check.yml` and `release.yml` on GitHub-hosted runners: six archives, `checksums.txt`, a keyless cosign bundle and one SPDX SBOM per archive, plus a build-provenance attestation that `gh attestation verify` accepts. `templates/check/` and `templates/go/release.yml` in the catalog repository match what ran. Still unverified: the other stacks' `release.yml` templates (same shape, different toolchain step), Pages deployment of `deploy/demo/pages.yml`, and every Gitea branch (key-based cosign, the Gitea release API) since no Gitea mirror is planned.

## Next, in order

| Version | Contents | Status |
|---|---|---|
| 0.1 | engine, presets, check/init/facts/explain, table/json/agent | done |
| 0.2 | workspace discovery, rule scopes, task-runner contract, `fix` with templates, `.fleetlint.yaml` | done |
| 0.4 (pulled forward) | JSON schema, SARIF and Markdown output, generated rule docs with drift check, prek hook + GitHub Action definitions, `--deep` (runs `make check`), CLI tests, outcome rules with YAML satisfiers (`release/sbom` as reference), `foreach` | done |
| 0.3 | release, signing, changelog and CI families as outcome rules that detect producers | done; presets `strict` and `oss` are open |
| 0.5 | fleet mode: matrix report, history, per-repo agent tasks; `init --pr`; forge discovery with `--from` | done (`--from` is tested against a local server; against the real GitHub API only as far as listing a public owner and starting the clones, and not yet against Gitea or a private organization) |
| 0.6 | `command` rules, baseline ratchet, nested configs | done |
| 0.6.1 | 20 further rules towards the 1.0 catalog (`docs/rules-1.0.md`): repo, lint, quality, docs, deps, ci, release (producer detection), public families; accessors `jobs`, `triggers`, `tags`, `commits`, `grep`, `filesize`, `globmatch`; CEL string/list extensions | done |
| 0.5.1 | public demo at demo.fleetlint.org: `fleetlint fleet` over the four `fleetlint/*` repositories published to Pages nightly (`deploy/demo/`); the report directory already contains `index.html` and HTML action lists | files ready, Pages deployment open |
| 0.7 | organization layer: locked rules, severity floors, `exceptions: false`, `--require` | done |
| 0.7.1 | git catalog sources (`git+<url>//<path>@<tag or commit>`) | done |
| 0.7.2 | OCI catalog sources with signature verification, private templates shipped with catalogs | needs registry access to test |
| 0.8 | team layer: sources file with aliases, layered resolution, per-layer attribution in reports | done (`signers` in the sources file waits for OCI sources, 0.7.2; the fleet report has a team summary with expiring exceptions and a CSV export) |
| 1.0 | formats frozen, compatibility promise | |

## Also outstanding

- Once the catalog repository has a tag and `publish.yml` has signed `ghcr.io/fleetlint/catalog:<tag>`: an opt-in integration test of the `oci://` source against that real artifact (public sigstore root, SCT check, the JSON bundle path) — [fleetlint#1](https://github.com/fleetlint/fleetlint/issues/1).
- The per-stack release templates (`templates/*/release.yml` in the catalog repository) other than Go have not run on a runner; the Rust one assumes a `.goreleaser.yaml` with the Rust builder. Folding the four `make dist` variants into one template with a per-stack toolchain step is open.
- Baseline practices without a rule yet: detekt rule strictness for Kotlin, duplication and mutation gates, API-compatibility checks for libraries, signed tags, documented forks.

- Statement coverage is ~44%; CLI and reporters are the gap.

- Sources for every rule. Of 235 rules, 230 cite only fleetlint.org (pages generated from the rules themselves), 4 cite nothing, 1 cites an outside source (reproducible-builds.org). Every rule should name where its practice comes from, strongest source first: normative (language spec, official tool: `gofmt`, `go vet`); official guidance (Go Code Review Comments, Effective Go, PEP 8, Rust API Guidelines); public style guides with clear status (Google Go Style Guide, Uber Go Style Guide); standards for process and supply chain (NIST SSDF SP 800-218, SLSA, OpenSSF Scorecard, OWASP ASVS, CIS benchmarks, OASIS SARIF); a linter's check id (staticcheck `SA1019`, ruff, clippy). Slogans and blog posts are not a source on their own ("accept interfaces, return structs" is folklore; Code Review Comments is the source). A rule that is a convention says so. Open: `refs` as typed entries (source, kind, section) instead of bare URLs, shown on the rule pages and in reports, and a catalog test that fails a rule without an outside source.

- SARIF beyond the subset written since 0.4 (OASIS SARIF 2.1.0, `internal/report/sarif.go`). Findings without a path get no location, and GitHub code scanning drops locationless results, so most "file is missing" findings never show up there: anchor them to the expected path or to `.fleetlint.yaml`. Missing: `helpUri` to the rule's page on fleetlint.org; `partialFingerprints`, so alerts keep their identity across runs; `security-severity` and family tags on security rules; `automationDetails.id` per scope, for monorepos that upload several runs; excepted findings as `suppressions` with their reason and expiry instead of being left out; rule errors as `invocations[].toolExecutionNotifications` instead of results; the rule's sources (see above) in its properties; optional `fixes` from the fix templates. Validate the output against the official OASIS schema in the tests. Fleet mode writes no SARIF yet.

## To think about: a more flexible hierarchy

The layers are fixed at four (preset, org, team, repo) since 2026-10-07. Real organisations may need more or different levels: business unit, department, product line, platform group, several org-wide catalogs owned by different teams (security, platform, compliance), or a team nested under a team. Open questions before changing it:

- Arbitrary named layers with a declared order (in the sources file), or a tree where a catalog names its parent?
- How a repository belongs to several branches at once (a team catalog plus a compliance catalog that is not "above" it).
- Whether policy should bind by layer rank or by explicit precedence between catalogs.
- What stays fixed for reproducibility: one catalog version per run and a deterministic order must survive any redesign.

Nothing in code or docs promises the four layers beyond `metadata.layer: org|team` and the `org` / `team/<name>` alias names; a change there is a format change to settle before 1.0.

Fleet nesting belongs to the same decision. A fleet is flat today: `defaults:`, one level of `groups:` (from the fleet file or the top directory under `--discover`), and entries. Open questions:

- Fleet files that include other fleet files (a team's fleet inside the organisation's), and which one's `defaults:` wins.
- Nested groups: `--discover` takes only the top directory (`incyde/cra-tooling-neu/vuln-ui` is in group `incyde`); settings and report summaries per level, rolled up.
- Whether a fleet group is the same thing as a catalog layer (a group selects the team catalog) or stays a reporting and settings unit only.

## Backlog: what large organisations will ask for

Gaps noted on 2026-10-06 against what exists today. The forge client lists repositories and looks up visibility only; the fleet history is a directory of dated JSON files; six stacks are detected; `init --pr` opens a pull request but nothing runs on a schedule.

Running at scale
- A scheduled central runner: nightly `fleet` over an owner, durable results, pull requests for fixable findings, retries. API-only evaluation for cheap facts so not every repository is cloned.
- `fix --apply --pr` and `fleet --fix --pr`: when main is protected, commit the fixes on a branch and open a pull request on GitHub, GitLab, Gitea or Forgejo through the forge API (`init --pr` does this for the config only, and only on GitHub and Gitea; GitLab is not in the forge client at all). Detect protection instead of failing on push, reuse an open fleetlint branch, and write the findings into the PR body.
- Trend output per team and rule (time series or a stable export for a warehouse), built on the dated history files.
- Ownership from `CODEOWNERS`, forge teams or a service catalog (Backstage) instead of the hand-kept fleet file; the `repo/codeowners` rule only checks presence.
- Exception governance: who may grant one, approver recorded, escalation when they expire, policy layers that forbid exceptions per rule (locks cover the rule side, not the human side).

Security and supply chain
- A `forge/*` family for repository settings read through the API: branch protection, required reviews, signed commits, secret scanning and vulnerability alerts enabled, deploy keys, admin list. This is the Scorecard overlap the comparison page exposes.
- Verification beyond presence: stated SLSA level and attestations checked, lockfile integrity, fork pull-request workflows without write tokens, allowed-actions lists, dependency review on pull requests (`ci/pull-request-target` exists; the rest does not).
- Signed catalogs with provenance verified before a catalog may write files (0.7.2 covers OCI; git sources have no signature check).
- Key-based verification as an alternative to keyless OIDC: verify git catalog commits and tags (SSH or GPG signatures against allowed signers) and OCI catalog images (cosign public keys) with signing keys listed in the sources file, for registries and forges without a sigstore OIDC issuer and for air-gapped setups.

Integration
- Pull-request annotations or review comments, incremental against the base branch (the action only runs `check`).
- Scorecard-compatible JSON, Backstage tech-insights facts, Slack or e-mail digest of the fleet summary (SARIF and Markdown exist).
- A read-only policy endpoint answering "does repository X pass policy Y" from the latest fleet run, for merge or creation gates.

Repository shapes
- Build graphs: Bazel, Nx, Turborepo (`taskrunner` knows make, just, task, npm scripts, gradle).
- Stacks: Java/Maven, .NET, C/C++, Ruby, PHP; hygiene rules for Terraform, Helm, Kustomize, protobuf and OpenAPI.

Governance of fleetlint
- Deprecation policy, schema version in every output, conformance tests for third-party catalogs (1.0 row above).
- Kubernetes-project conventions: `OWNERS`, `SECURITY_CONTACTS`, DCO or CLA check, published support matrix.

Adoption (since 0.2.0: Homebrew tap, Scoop bucket, Nix flake in `fleetlint/nur`, `.deb`/`.rpm`/`.apk`/Arch packages, the signed image `ghcr.io/fleetlint/fleetlint`, `fleetlint/action` (unreleased), `scripts/install.sh` and a GitLab CI job in `ci/gitlab/`. Still open below: AUR, Winget, the aqua registry entry, the first action release and its Marketplace listing, `install.sh` served from fleetlint.org, a GitLab CI/CD catalog component)
- `install.sh` served from fleetlint.org (`curl -sSfL https://fleetlint.org/install.sh | sh`): picks OS and arch, downloads the release archive, verifies `checksums.txt` against the cosign bundle, installs to `~/.local/bin` or a given prefix, pinnable with a version argument.
- Package managers from GoReleaser: Homebrew tap (`brew install fleetlint/tap/fleetlint`), Scoop and Winget for Windows, `.deb`/`.rpm`/`.apk` via nfpm, AUR, a Nix flake, and a `mise`/`aqua` registry entry so `mise use fleetlint` works.
- A published GitHub Action (`fleetlint/action` on the Marketplace) and a GitLab CI component that install a pinned release, run `check`, upload SARIF to code scanning and annotate pull requests; a Gitea/Forgejo variant of the same.
- A pre-commit / prek hook definition in the fleetlint repository so `repo: https://github.com/fleetlint/fleetlint` works in `.pre-commit-config.yaml`.
- Container image (`ghcr.io/fleetlint/fleetlint`, distroless, signed, SBOM attached) for CI systems that run everything in containers.
- Renovate and Dependabot know the install paths (Homebrew formula, action tag, image tag, `install.sh` version pin) so adopters' pins stay current.
- `fleetlint init` as the five-minute path: `curl … | sh && fleetlint init --pr` should be the whole quick start on the home page, with a copy button.

Fleet app without a server
- A GitHub App ("fleetlint for GitHub") whose only runtime is a scheduled workflow in one repository, the way self-hosted Renovate runs from `renovatebot/github-action`: the workflow mints installation tokens from the App's private key (`actions/create-github-app-token`), lists the installations and their repositories, runs `fleet` with API-only facts where possible, opens or updates fix pull requests and publishes the matrix to Pages. Installing the App is the whole onboarding: no `.fleetlint.yaml` edit, no secret in the target repository, no server. What a server would add and this cannot: reacting to webhooks (push, PR opened) instead of a schedule; a push-to-main re-check can be approximated by the composite action in the target repository. The same shape works on Gitea/Forgejo with a bot account and a cron workflow; GitLab needs a group access token and a scheduled pipeline.

## Low findings from the 0.3 security and architecture review (for later decisions)

The review on 2026-10-12 found these; 0.3.0 fixed everything above low. Each line is a decision to make, not a commitment.

Security
- Catalog fetching: the sigstore TUF fetch ignores the run's timeout; OCI referrers and bundle layers are unbounded in count; a git catalog fetch is bounded by time only, not by size on disk.
- `localhost*` gets plain HTTP and Docker credentials for OCI (`localhost.evil.tld` matches): compare the host exactly.
- Signers apply to every `oci://` reference, not per sources alias; nothing protects against an old signed tag being served again (no minimum version).
- Fix templates from remote catalogs overlay the built-ins and become hooks and workflows: show each template's catalog in the fix plan.
- Fleet clones: the forge may hand out a local path or another host as clone URL (the token no longer follows it); cache directories of two repositories can collide (`a/b_c` and `a_b/c`); discovered repository names are not cleaned like fleet-file names (`#` or `?` break the action-page links).
- `fleet --deep` with `--from` runs `make check` in every cloned repository: refuse it for clones unless allowed per entry? `fleet --fix --apply` writes into checkouts with uncommitted changes: refuse without `--allow-dirty`?
- `init` writes workspace member names unescaped into a YAML comment.
- Exception `re:` patterns are not validated when the config loads; an invalid one never matches. Baseline fingerprints are a set, so a new duplicate of a baselined finding stays hidden.
- `scripts/install.sh` and the action: without cosign only the checksum from the same release is checked (`gh attestation verify` would check provenance); add `curl --proto '=https' --tlsv1.2`, validate the version argument, match the checksum line with `grep -F`.
- CI and release: the docs workflow writes `GITHUB_TOKEN` into the global git config for later steps; the catalog's publish workflow has no environment or tag protection and interpolates a step output into `run:`; the deprecated `action/` interpolates its inputs into shell (delete it); repository settings `sha_pinning_required` and an allowed-actions list; rulesets limiting the package repositories' deploy keys to fast-forward pushes; the Homebrew cask removes the quarantine flag (notarize instead?); the image sets `safe.directory=*` (use `/repo`).

Architecture
- Fleet history stores a full `fleet.json` per run, dry runs included: compact history files or `--keep N`.
- Fleet runs one repository at a time and a panic in one rule ends the run: a worker pool with per-repository recovery (the CLI no longer holds state that blocks it).
- A scope re-infers release and CI on its own, so a member of a tier-1 repository with root-only workflows is inferred as tier 3; forge-reported visibility is labelled `detected`.
- `Repo` keeps the context it was opened with; test seams `requireSCT` and `loadBundle` are package variables.
- GitLab is missing from the forge client (`--from gitlab:`, visibility, `init --pr`).
- Catalog fix: what the top-level `catalog` field of the check JSON and of `fleet.json` means differs (the run's pinned version vs the built-in one); per-repository pins are now in `fleet.json`.

## Release checks per artifact

`release/signing`, `release/sbom` and `release/provenance` are capabilities with one implementation per stack and `combine: any` since 2026-10-15: the stack that publishes decides, and one recognised producer anywhere passes the repository. That cannot tell "binaries signed, npm package not" apart from "everything signed". The precise model checks each artifact a tag-triggered workflow publishes, recognised by its publish step, against what fits that artifact. One item per artifact:

- **Files on a forge release** (binaries, archives, an APK or IPA uploaded as assets; `gh release create|upload`, `tea releases create`, the Gitea/GitLab release API, action-gh-release, GoReleaser): `checksums.txt` or `SHA256SUMS` signed (cosign keyless on GitHub/GitLab, key-based on Gitea, gpg), one SPDX or CycloneDX SBOM per artifact uploaded next to it, provenance per artifact (attest action, `cosign attest-blob`, SLSA generator).
- **Container images** (`docker push`, build-push-action, `podman push`, ko, Jib): `cosign sign <image>@<digest>`, SBOM and provenance attached to the digest (BuildKit `sbom: true` / `provenance`, `cosign attest`), never by tag.
- **npm packages** (`npm|pnpm|yarn|bun publish`): provenance through trusted publishing; the registry's provenance is the signature. SBOM: `npm sbom` only when the package bundles dependencies.
- **PyPI distributions** (pypi-publish, twine, uv, poetry, flit, hatch, pdm): PEP 740 attestations through trusted publishing; wheels that vendor native libraries need an SBOM.
- **crates.io crates** (`cargo publish`): trusted publishing; crates.io has no signatures yet, so a crate is checked for trusted publishing only, and its binaries as forge release files (cargo-auditable embeds the dependency list).
- **RubyGems** (`gem push`, release-gem): Sigstore attestations through trusted publishing; signed gems (`signing_key`/`cert_chain`) as the alternative.
- **Maven Central artifacts** (JARs, AARs, POMs; Gradle `publish*`, `mvn deploy`, the vanniktech plugin): PGP signatures (required by Central), the CycloneDX BOM published as an extra artifact with the `cyclonedx` classifier, provenance with `cosign attest-blob` until Central accepts attestations. No trusted publishing yet.
- **Android apps** (APK/AAB as release files, `fastlane supply`, gradle-play-publisher): the APK signature verified with `apksigner verify` (`jarsigner -verify` for an AAB) against the pinned release certificate digest, an SBOM of the release runtime classpath (lockfile or CycloneDX Gradle), provenance with `cosign attest-blob`. Play App Signing re-signs: record which key a check expects.
- **Apple apps and binaries** (IPA, `.app`/`.pkg`/`.dmg`; `xcodebuild -exportArchive`, fastlane, `notarytool`): code signing with a Developer ID or distribution identity, notarization and stapling for macOS downloads, provenance with `cosign attest-blob`.
- **NuGet packages** (`dotnet nuget push`): `dotnet nuget sign` or nuget.org's repository signature, trusted publishing (nuget.org OIDC), an SBOM (CycloneDX .NET or Microsoft sbom-tool) inside or next to the package.
- **pub.dev packages** (`dart|flutter pub publish`): automated publishing with GitHub OIDC; pub.dev has no package signatures.
- **Helm charts** (`helm push` to an OCI registry, chart-releaser): `cosign sign` on the chart digest or `helm package --sign` (`.prov`), checked like a container image when pushed to OCI.
- **Go modules**: nothing is uploaded; the checksum database (sum.golang.org) is the integrity record, so only binaries built from the module are artifacts (forge release files).
- **Package-manager manifests that point at release files** (Homebrew tap, Scoop bucket, Nix flake, AUR, winget): they reference the release files' checksums; checked through the signed checksums file of the forge release, not separately.

Open questions: whether each artifact becomes its own rule (`release/artifact-signed` with `foreach` over publish steps) or a capability keyed by artifact kind instead of stack; how a repository declares an artifact the publish-step detection misses; and how this interacts with `combine: any`, which the per-artifact rules would replace for these three capabilities.
