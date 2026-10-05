# Contributing

## Setup

Go 1.26 or newer and `make`. `make tools` installs the pinned golangci-lint, govulncheck and gocover-cobertura; the full check also needs `gitleaks` and, for the changed-line coverage gate, `diff-cover`.

```sh
git clone https://github.com/fleetlint/fleetlint && cd fleetlint
make check          # fmt, lint, test with -race, audit; what CI runs
make check-fast     # the pre-push subset
```

Two ways to run the gates: on your machine (the default), or inside the dev container with `make check DEVCONTAINER=1` (needs Docker and the devcontainer CLI). The container gives you Go and runs `make tools`; gitleaks, diff-cover and prek still need installing in it.

`prek install` turns on the commit hooks (`uv tool install prek` if you do not have it).

## Making a change

- One concern per pull request. Open an issue first for a new rule family or a format change; the catalog and config formats are frozen at 1.0 and changes need a compatibility note.
- Every rule change ships with a passing and a failing fixture in `internal/engine/` and a regenerated `docs/` (`make docs`; CI runs `fleetlint docs --check`).
- Commits follow Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`); the changelog is generated from them. No AI tool as author or co-author in commit messages, which `public/no-ai-attribution` enforces on this repository.
- Run `fleetlint check` on the repository itself before pushing; it should stay at zero errors.

## Adding a rule

Rules are YAML in `internal/catalog/presets/`. Read `docs/writing-rules.md`, then: declare `severity`, give it a `requirement` someone could verify by hand and a `fix.human` that fits on one line, and prefer an outcome rule with satisfiers when several tools can meet the requirement. If a new accessor is needed, add it to `internal/celenv/` with a test in `celenv_test.go`.

## Reporting a vulnerability

See `SECURITY.md`; do not open a public issue.
