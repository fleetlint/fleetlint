# Writing rules

## Where rules live

- **Presets** ship in the binary and are referenced as `fleetlint:<name>`. Their source is the catalog repository, [fleetlint/catalog](https://github.com/fleetlint/catalog) (`presets/*.yaml`), of which every binary embeds one version; another version of a preset can be referenced like any catalog in a git repository, `git+https://github.com/fleetlint/catalog.git//presets/recommended.yaml@<commit>`, and then takes its fix templates from the binary.
- **Your catalog** is a YAML file with the same format, hosted anywhere over https and referenced with its digest: `https://…/catalog.yaml#sha256-<hex>`. Get the digest with `fleetlint catalog digest catalog.yaml`. Fetches are https only, follow redirects only within the same host, time out after 20 seconds and refuse bodies over 2 MiB; the digest, not the transport, is what makes the content trusted. Every rule and partial must state its `severity`.
- **A catalog in a git repository** is referenced as `git+<url>//<path>@<revision>`, for example `git+https://git.example.com/platform/standards.git//teams/mobile/catalog.yaml@v3.2.0#sha256-<hex>`. The URL is https or ssh and is fetched with the credentials git already has (credential helper, ssh agent), so private repositories work. The revision is a tag or a full commit SHA, never a branch. A commit SHA needs no digest; a tag can be moved, so it must carry `#sha256-<hex>` and a moved tag fails the digest check. Files over 2 MiB are refused and the fetch times out after two minutes. Like https catalogs, git catalogs are untrusted: they cannot declare `command` rules, and may include only presets, https catalogs and other git catalogs.
- **Inline rules** go under `rules:` in `.fleetlint.yaml` with a `kind`.

## Catalog format

```yaml
apiVersion: fleetlint.org/v1
kind: Catalog
metadata:
  name: my-standards
  version: 1.2.0
  includes: [fleetlint:recommended]   # optional; loaded first
rules:
  - id: family/name            # namespaced, stable; documentation and overrides key on it
    title: One line
    kind: expr                 # expr | go | command
    severity: warning          # error | warning | info
    tiers: [1, 2]              # omit = all
    stacks: [go]               # omit = any
    scope: root                # root | each (default) | members — where in a monorepo it runs
    when: repo.tier <= 2       # applicability; false = not applicable, not a failure
    expr: file("docs/ARCHITECTURE.md")
    message: "architecture doc missing"
    requirement: What must be true, in one or two sentences.
    rationale: Why it matters.
    fix:
      human: What a person does.
      agent: What a coding agent does, precisely, and nothing else.
      actions:                 # optional mechanical steps for `fleetlint fix`
        - template: "{stack}/pre-commit-config.yaml"   # from the catalog's templates; {stack} = detected stack
          to: .pre-commit-config.yaml                   # never overwrites
        - untrack: "*"                                  # git rm --cached the rule's findings
          gitignore: ["*.log"]                          # append missing patterns
        - set: {file: package.json, key: license, value: "{license}"}   # add a value to an existing JSON or TOML file
    params: { key: value }     # available to the expression as params.key
    refs: ["docs/baseline.md#documentation"]
```

`set` adds one string to a file that exists and never changes a key that is already there. JSON takes a top-level key, TOML takes `table.key` in a table written with a `[table]` header. The edit is kept only if the file afterwards parses to exactly the old content plus the key; files it cannot edit that way (JSON with comments, inline tables) are left to a person. `{license}` is the SPDX identifier of the repository's LICENSE file when its text is one recognised license; without it the action does nothing. GNU licenses yield no identifier, because the text does not say whether `-only` or `-or-later` applies.

`{taskrunner}` is the runner file for the repository's task runner (`Makefile`, `justfile` or `Taskfile.yml`), written with the container switch when `repo.container` is not `none`. `{stack}/devcontainer.json` is generated for all stacks, and only when `repo.container` is `devcontainer`; `{stack}/check.yml` and the hooks call the repository's runner and follow the container setting. A `{stack}/pre-commit-config.yaml` or `{stack}/check.yml` template is assembled: the shared part (hygiene, secret scan, commit messages, shell, the pre-push check), then the hooks or toolchain of the root stack, of any second stack in the same directory, and of each nested project. Like every template it is only written when the file does not exist; an existing file is never merged into.

Every rule needs `title`, `requirement` and `fix.human`; presets additionally require `rationale` and `fix.agent` (enforced by tests).

## CEL predicates

Variables: `repo` (`name`, `stacks`, `forge`, `visibility`, `public`, `tier`, `layout`, `ci`, `hooks`, `has_git`, `scope`), `release` (`exists`, `mechanisms`, `tag_patterns`, `latest_tag`), `taskrunner` (`kind`, `targets`), `params`.

Accessors:

| Function | Returns |
|---|---|
| `exists(glob)` | any file matches (`**` supported) |
| `file(path)` | exact path exists (`has` is reserved by CEL) |
| `glob(pattern)` | matching paths, sorted |
| `tracked()` | git-tracked paths in the scope |
| `text(path)` / `lines(path)` | contents, `""`/`[]` when absent |
| `yaml(path)` / `toml(path)` / `json(path)` | parsed document, `null` when absent; a parse error is a rule error |
| `makefile(path)` | `{targets: [...], deps: {target: [...]}}` |
| `scan(path, regex)` | 1-based line numbers that match |
| `matchesAny(list, regex)` | any element matches |

Use `has(doc.field)` to test for a key before reading it; reading a missing key is an evaluation error, which the engine reports as a rule error rather than a pass.

Examples:

```yaml
expr: '"check" in makefile("Makefile").targets'
expr: 'yaml(".pre-commit-config.yaml").repos.exists(r, r.repo.contains("gitleaks"))'
expr: '!tracked().exists(f, f.matches("\\.env$"))'
expr: 'glob(".github/workflows/*.yml").all(w, has(yaml(w).permissions))'
```

## Go rules

Implement `rules.Checker` in `internal/rules/builtin`, register it in `builtin.go`, declare the id in a catalog with `kind: go`, and add a passing and a failing fixture test. Return one `Finding` per concrete problem with `Path` and `Line` where known; set `Outcome.Evidence` to explain a pass.

## Outcome rules: satisfiers without Go

An outcome rule asks whether a result is achieved and lists the ways it can be. Each satisfier is a CEL predicate with a name; the first that holds makes the rule pass and is reported as the evidence (`satisfied by goreleaser`). Catalog authors add satisfiers in YAML; repositories add their own with `accept:` and can never remove the requirement.

```yaml
- id: release/sbom
  kind: outcome
  severity: error
  when: release.exists
  satisfiers:
    - name: goreleaser
      when: file(".goreleaser.yaml")                 # optional guard
      expr: has(yaml(".goreleaser.yaml").sboms)
    - name: workflow-command
      expr: tagged_workflows().exists(w, steps(w).exists(s, s.run.matches("\\bsyft\\b")))
  partials:                                          # graded results when nothing satisfies
    - name: sbom-outside-release
      severity: warning
      expr: workflows().exists(w, steps(w).exists(s, s.run.matches("\\bsyft\\b")))
      message: "an SBOM producer exists but no release workflow invokes it"
  message: "no release step generates an SBOM"
  requirement: …
  fix: { human: …, agent: … }
```

In a repository:

```yaml
rules:
  release/sbom:
    accept:
      - name: own-script
        expr: tagged_workflows().exists(w, steps(w).exists(s, s.run.contains("scripts/sbom.sh")))
```

Accessors that make this practical: `workflows()`, `tagged_workflows()` (release pipelines: workflows triggered by `on.push.tags`), and `steps(path)`, which flattens every job's steps into `{job, name, uses, run, with}` with empty values for absent fields, so expressions never fail on a missing key. The same pattern suits signing, changelog generation and coverage gates: define the outcome, list the known producers, grade by whether the producer sits in the right place.

## Organization catalogs: locks, floors, `--require`

A catalog that serves as an organization baseline can constrain what lower layers (team catalogs, the repository, its scopes and nested files) may do with a rule:

```yaml
rules:
  - id: security/no-tracked-secrets
    severity: error
    locked: true          # cannot be disabled, lowered or redefined by anything loaded after it
    exceptions: false     # no per-finding exceptions either; reserved for the few absolute rules
  - id: ci/actions-pinned
    severity: error
    min_severity: warning # may be lowered to warning (with a reason), never to info; may be disabled with a reason
```

Violations are configuration errors at load time (exit 2), not weaker checks at run time, and they name the catalog that set the constraint. Raising severity, adding `params` and `accept`, and reasoned exceptions on locked rules remain possible; the report shows every one.

Locks only hold for catalogs that are actually loaded. A repository could drop the baseline from `extends`, so enforcement belongs where the check runs: the organization's CI template and fleet runner pass `--require acme-baseline` (a catalog name, a ref, or `ref#sha256-<digest>` to pin the exact version). A repository whose effective configuration lacks that catalog fails with exit 2 before any rule runs, and in fleet mode becomes an error row. The flag is persistent, so `fix`, `explain` and `baseline` refuse under a weaker configuration too.

## Layers: organization, teams, repository

Repositories should not carry URLs and pins for every catalog. An organization publishes one **sources file** that names its catalogs:

```yaml
# sources.yaml
version: 1
catalogs:
  org: git+https://git.example.com/platform/standards.git//org/catalog.yaml@v3.2.0#sha256-<hex>
  team/platform: git+https://git.example.com/platform/standards.git//teams/platform/catalog.yaml@v3.2.0#sha256-<hex>
  team/mobile: https://standards.example.com/teams/mobile.yaml#sha256-<hex>
```

and a repository refers to it:

```yaml
version: 1
sources: git+https://git.example.com/platform/standards.git//sources.yaml@<commit>
extends: [fleetlint:recommended, org, team/platform]
```

- Names are `org` or `team/<name>`; nothing else is accepted. Targets are presets, https catalogs with a digest or git catalogs. A sources file given as a local path may also name local paths, resolved relative to itself; a remote one may not.
- The sources file itself is a local path, an https URL with a digest or a git reference, pinned the same way as a catalog. Rotating a catalog version is then one change to the sources file and one pin update per repository.
- With `sources`, resolution order is fixed however `extends` is written: presets, then `org`, then `team/*`, then catalogs the repository references directly, then its own `rules:`. Without `sources`, the written order applies as before.
- Every rule carries the layer that defined it (`preset`, `org`, `team`, `repo`): `explain` prints it, JSON has `rule.layer`, SARIF has it in the rule properties, and the JSON `catalogs` list has each catalog's layer and name.
- Locks and severity floors hold across catalogs: a team catalog cannot redefine a locked organization rule or set a severity below the organization's `min_severity`, and the floor still binds the repository after a team redefines the rule. A later layer lowering an earlier layer's severity is allowed above the floor and is recorded: it appears under `weakened` in JSON and under "Lowered severities" in the fleet report with its layer, next to what repositories lowered themselves.
- `--require org` and `--require team/platform` match the names from the sources file.

## Command rules

For logic the catalog cannot express, a repository's own `.fleetlint.yaml` may run a tracked executable:

```yaml
rules:
  repo/fixtures-match-schema:
    kind: command
    run: ./scripts/check-fixtures
    message: "fixtures are out of date"
    fix: { human: "make fixtures" }
```

Command rules execute repository code, so they run only with `--deep`, like `quality/check-passes`; without it they report as not applicable with that reason. The other constraints, all enforced before anything runs: only the repository's own config may declare one (remote catalogs are rejected); `run` is a relative path to a git-tracked, executable, regular file that is not world-writable, with no arguments. The program receives `{rule, facts, scope, root, params}` as JSON on stdin, runs with a scrubbed environment (`PATH`, `HOME`, `LANG`, `LC_ALL`, `TMPDIR`, `TERM`, `FLEETLINT=1`) in the scope directory, and has 60 seconds. It prints findings as JSON lines `{"message": "...", "path": "...", "line": 1}` on stdout, at most 1 MiB; exit 0 or 1 means it ran, anything else is a rule error, never a pass.
