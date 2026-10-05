# fleetlint

Policy for a fleet of repositories. fleetlint checks how a repository is set up (hooks, task runner, lint configuration, CI, release pipeline) against a catalog of rules, reports how far it is from compliant, and tells you or your coding agent exactly how to close each gap.

It does not lint code. It checks that the things which lint, test, build and release the code are present, wired together and configured the way you decided they should be, in every repository, the same way.

## Install

```sh
go install github.com/fleetlint/fleetlint/cmd/fleetlint@latest
```

Or download a binary for Linux, macOS or Windows from the [releases](https://github.com/fleetlint/fleetlint/releases). Each release carries `checksums.txt`, a signature bundle for it and one SBOM per archive; the release notes show the `cosign verify-blob` command.

## Quickstart

```sh
cd your-repo
fleetlint                 # zero config: discovers stack, forge, tier; applies fleetlint:recommended
fleetlint init            # writes a two-line .fleetlint.yaml with the discovered facts as comments
fleetlint explain taskrunner/targets
fleetlint                 # findings marked * are the ones `fix` can repair
fleetlint fix             # dry run: files it would create, patterns it would add, files it would untrack
fleetlint fix --apply     # write them; existing files are never overwritten
fleetlint check -f agent  # task list for a coding agent, one commit per rule
fleetlint facts           # what was discovered, and where each value came from
fleetlint fleet a/ b/ c/  # many repositories: one HTML/Markdown/JSON report plus per-repo task lists
fleetlint fleet --from github:your-org   # or every repository of an owner
fleetlint baseline        # grandfather today's findings; from now on only new ones fail
```

Exit codes: 0 clean, 1 findings at or above `--fail-on` (default `error`), 2 configuration error, 3 internal error.

The presets and the templates live in their own repository, [fleetlint/catalog](https://github.com/fleetlint/catalog); every binary embeds one version of it, and `fleetlint --version` and every report say which. What the presets check and why is in `docs/baseline.md`; the tools per stack are in `docs/stacks.md`; `docs/slop.md` is the catalog behind the `slop/*` rules and the add-on preset `fleetlint:slop`.

Organizations: a baseline catalog can mark rules `locked`, set a `min_severity` floor or forbid `exceptions`; CI and fleet runs pass `--require <catalog>` so a repository cannot drop the baseline unnoticed. A sources file gives catalogs the names `org` and `team/<name>`, and reports say which layer defined or weakened each rule. See `docs/writing-rules.md` and `docs/fleet.md`. Hooks, CI and editors: `docs/integrations.md`.

## Configuration

`.fleetlint.yaml` at the repository root (`fleetlint.yaml` is also accepted). Everything is optional; the whole policy is in this one file. Monorepos need nothing extra: workspace members from `go.work`, `pnpm-workspace.yaml`, `package.json` workspaces, Cargo `[workspace]`, `melos.yaml` and Gradle `settings.gradle` are discovered and evaluated as scopes, and so is a top-level directory with its own tracked manifest (a `frontend/` with a `package.json` next to a Go module); `scopes:` overrides the list, `scopes: []` disables it.

```yaml
version: 1
extends:
  - fleetlint:recommended                              # built-in preset
  - https://example.com/catalog.yaml#sha256-<digest>    # your own catalog, pinned
  - git+https://git.example.com/org/standards.git//catalog.yaml@<commit>   # or from a git repository
facts:
  tier: 1            # 1 production/public, 2 personal tool, 3 experiment
  visibility: public
  taskrunner: just   # make, just, task, npm-scripts, gradle; default: the first one found
  container: podman  # none, devcontainer, docker, podman; default: devcontainer if one exists, else none
scopes:              # polyglot repositories: each path is evaluated as its own repo
  - path: web/
    facts: { stacks: [node] }
rules:
  hooks/pre-push-check: { enabled: false, reason: "CI runs the full check" }
  taskrunner/targets: { params: { required: [fmt, lint, test, check] } }
  repo/arch-doc:     # inline rule
    kind: expr
    expr: file("docs/ARCHITECTURE.md")
    message: "architecture doc missing"
    fix: { human: "write docs/ARCHITECTURE.md" }
exceptions:
  - rule: ci/actions-pinned
    match: ".github/workflows/android.yml"
    reason: "flutter-action has no stable SHA"
    until: 2027-03-31
```

Disabling a rule or lowering its severity requires a `reason`; both appear in the report. Exceptions cover individual findings, keep the rule running, and expire. Rule predicates are [CEL](https://cel.dev) over discovered facts and file accessors; see `docs/writing-rules.md`.

## Development

| Target | Purpose |
|---|---|
| `make check-fast` | before every commit: format, lint on new code, short tests, self-check |
| `make check` | definition of green: lint, race tests, coverage, audit, build, self-check |
| `make lint` / `make test` / `make cover` / `make audit` / `make build` | the individual gates |
| `make tools` | installs the pinned golangci-lint, govulncheck and gocover-cobertura |
| `make release` | checks, bumps the version from conventional commits, tags |

Requires Go 1.26 or newer (the build uses the toolchain named in `go.mod` and downloads it if needed) and, for `audit`, gitleaks; `make tools` installs the rest. Hooks: `prek install`. `make <target> CONTAINER=1` runs any target inside the dev container (`.devcontainer/`, needs Docker and the devcontainer CLI); the default is this machine. See `CONTRIBUTING.md`.

## Status

0.1.0 is the first release. The configuration, catalog and JSON formats may still change before 1.0; `CHANGELOG.md` lists every change with a compatibility note. What is next and what is still unverified is in `docs/ROADMAP.md`.

## License

MIT, see `LICENSE`.

Files that fleetlint writes into your repository (`.fleetlint.yaml`, hook configuration, workflows, Makefile, linter configuration, dev container and the other templates) belong to that repository. Use, change and distribute them under whatever terms you like; no attribution or license notice is required for them.
