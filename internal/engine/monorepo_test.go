package engine_test

import (
	"maps"
	"testing"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
)

// expectScoped checks the status of each rule id in each scope.
func expectScoped(t *testing.T, out *engine.Run, want model.Status, cases map[string][]string) {
	t.Helper()
	for scope, ids := range cases {
		for _, id := range ids {
			if s := scoped(t, out.Results, scope, id); s.Status != want {
				t.Errorf("%s in %q: want %s, got %s %q evidence=%q %+v", id, scope, want, s.Status, s.Err, s.Evidence, s.Findings)
			}
		}
	}
}

// A Node member next to a Go root inherits the root's pins, scans and coverage floor.
func TestMonorepoNodeMemberOfGoRoot(t *testing.T) {
	t.Parallel()
	const cfg = "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public}\nscopes: [{path: frontend, stacks: [node]}]\n"
	base := map[string]string{
		"go.mod":                "module x\n\ngo 1.24.2\n",
		"main.go":               "package main\n",
		"frontend/package.json": `{"name": "web", "dependencies": {"react": "19.0.0"}}`,
	}
	pass := maps.Clone(base)
	maps.Copy(pass, map[string]string{
		"frontend/package-lock.json": "{}",
		".nvmrc":                     "22.11.0\n",
		".npmrc":                     "ignore-scripts=true\n",
		"codecov.yml":                "coverage:\n  status:\n    project:\n      default:\n        target: 80%\n",
		".github/workflows/ci.yml": "on: [push]\njobs:\n  scan:\n    runs-on: ubuntu-latest\n    steps:\n" +
			"      - run: osv-scanner scan -r .\n      - run: npm ci\n        working-directory: frontend\n",
	})
	ids := []string{
		"repo/toolchain-pinned/node", "repo/lockfile-committed/node", "deps/vulnerability-scan/node",
		"quality/coverage-threshold/node", "deps/install-scripts-disabled", "deps/locked-install/node",
	}
	expectScoped(t, run(t, pass, cfg), model.StatusPass, map[string][]string{
		"frontend": ids,
		"":         {"repo/toolchain-pinned/go", "deps/vulnerability-scan/go", "quality/coverage-threshold/go"},
	})
	expectScoped(t, run(t, maps.Clone(base), cfg), model.StatusFail, map[string][]string{"frontend": ids})
}

// The lockfile of an npm workspace root counts for its members, and both pass or fail together.
func TestMonorepoNpmWorkspaceLockfile(t *testing.T) {
	t.Parallel()
	const cfg = "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public}\nscopes: [{path: packages/a}]\n"
	base := map[string]string{
		"package.json":            `{"name": "root", "private": true, "workspaces": ["packages/*"], "devDependencies": {"eslint": "9.0.0"}}`,
		"packages/a/package.json": `{"name": "a", "dependencies": {"left-pad": "1.3.0"}}`,
	}
	pass := maps.Clone(base)
	pass["package-lock.json"] = "{}"
	ids := map[string][]string{"": {"repo/lockfile-committed/node"}, "packages/a": {"repo/lockfile-committed/node"}}
	expectScoped(t, run(t, pass, cfg), model.StatusPass, ids)
	expectScoped(t, run(t, maps.Clone(base), cfg), model.StatusFail, ids)

	// A lockfile in a parent directory that is no workspace root does not count.
	stray := map[string]string{
		"package-lock.json":       "{}",
		"packages/a/package.json": `{"name": "a", "dependencies": {"left-pad": "1.3.0"}}`,
	}
	expectScoped(t, run(t, stray, cfg), model.StatusFail, map[string][]string{"packages/a": {"repo/lockfile-committed/node"}})
}

// A root that only hosts members: the CI and task runner at the root cover the members.
func TestMonorepoRootHostingMembers(t *testing.T) {
	t.Parallel()
	const cfg = "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public}\nscopes: [{path: api, stacks: [go]}, {path: web, stacks: [node]}]\n"
	base := map[string]string{
		"README.md":          "# x\n",
		"api/go.mod":         "module x\n\ngo 1.24.2\n",
		"api/main.go":        "package main\n",
		"web/package.json":   `{"name": "web", "dependencies": {"react": "19.0.0"}}`,
		"web/pnpm-lock.yaml": "lockfileVersion: '9.0'\n",
	}
	vuln := map[string][]string{"api": {"deps/vulnerability-scan/go"}, "web": {"deps/vulnerability-scan/node"}}

	ci := maps.Clone(base)
	ci[".gitlab-ci.yml"] = "scan:\n  image: ghcr.io/google/osv-scanner:v2.0.0\n  script:\n    - osv-scanner scan -r .\n"
	expectScoped(t, run(t, ci, cfg), model.StatusPass, vuln)

	runner := maps.Clone(base)
	runner["Makefile"] = "audit:\n\tcd api && govulncheck ./...\n\tcd web && pnpm audit\n"
	expectScoped(t, run(t, runner, cfg), model.StatusPass, vuln)

	// An install line or a comment in the root runner is no scan.
	installOnly := maps.Clone(base)
	installOnly["Makefile"] = "tools:\n\tgo install golang.org/x/vuln/cmd/govulncheck@latest\n\t# pnpm audit\n"
	expectScoped(t, run(t, installOnly, cfg), model.StatusFail, vuln)
	expectScoped(t, run(t, maps.Clone(base), cfg), model.StatusFail, vuln)
}

// Files tools read from a parent directory satisfy a member; without them it fails.
func TestMonorepoInheritedFiles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, scope, id string
		member, root    map[string]string
	}{
		{
			"cargo workspace lockfile", "crates/a", "repo/lockfile-committed/rust",
			map[string]string{"crates/a/Cargo.toml": "[package]\nname = \"a\"\n[dependencies]\nserde = \"1\"\n"},
			map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"crates/a\"]\n", "Cargo.lock": "version = 4\n"},
		},
		{
			"uv workspace lockfile", "svc", "repo/lockfile-committed/python",
			map[string]string{"svc/pyproject.toml": "[project]\nname = \"svc\"\ndependencies = [\"httpx\"]\n"},
			map[string]string{"pyproject.toml": "[project]\nname = \"root\"\n[tool.uv.workspace]\nmembers = [\"svc\"]\n", "uv.lock": "version = 1\n"},
		},
		{
			"go.work toolchain", "api", "repo/toolchain-pinned/go",
			map[string]string{"api/go.mod": "module a\n\ngo 1.24\n"},
			map[string]string{"go.work": "go 1.24.2\n\nuse ./api\n"},
		},
		{
			"root .tool-versions", "svc", "repo/toolchain-pinned/python",
			map[string]string{"svc/pyproject.toml": "[project]\nname = \"svc\"\n"},
			map[string]string{".tool-versions": "python 3.13.1\n"},
		},
		{
			"root mise.toml", "web", "repo/toolchain-pinned/node",
			map[string]string{"web/package.json": `{"name": "web"}`},
			map[string]string{".config/mise.toml": "[tools]\nnode = \"22.11.0\"\n"},
		},
		{
			"root rust-toolchain.toml", "crates/a", "repo/toolchain-pinned/rust",
			map[string]string{"crates/a/Cargo.toml": "[package]\nname = \"a\"\n"},
			map[string]string{"rust-toolchain.toml": "[toolchain]\nchannel = \"1.85.0\"\n"},
		},
		{
			"root deny.toml", "crates/a", "deps/license-check/rust",
			map[string]string{"crates/a/Cargo.toml": "[package]\nname = \"a\"\n", "crates/a/Makefile": "audit:\n\tcargo deny check\n"},
			map[string]string{"deny.toml": "[licenses]\nallow = [\"MIT\"]\n"},
		},
		{
			"root coverage config", "svc", "quality/coverage-threshold/python",
			map[string]string{"svc/pyproject.toml": "[project]\nname = \"svc\"\n"},
			map[string]string{".coveragerc": "[report]\nfail_under = 80\n"},
		},
		{
			"root package.json script", "packages/a", "deps/unused-check/node",
			map[string]string{"packages/a/package.json": `{"name": "a"}`},
			map[string]string{"package.json": "{\n  \"name\": \"root\",\n  \"scripts\": {\n    \"deps\": \"knip --dependencies\"\n  }\n}\n"},
		},
		{
			"root Taskfile", "api", "deps/locked-install/rust",
			map[string]string{"api/Cargo.toml": "[package]\nname = \"a\"\n"},
			map[string]string{"Taskfile.yml": "version: '3'\ntasks:\n  build:\n    cmds:\n      - cargo build --locked --manifest-path api/Cargo.toml\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public}\nscopes: [{path: " + tc.scope + "}]\n"
			files := maps.Clone(tc.member)
			maps.Copy(files, tc.root)
			files["README.md"] = "# x\n"
			expectScoped(t, run(t, files, cfg), model.StatusPass, map[string][]string{tc.scope: {tc.id}})
			files = maps.Clone(tc.member)
			files["README.md"] = "# x\n"
			expectScoped(t, run(t, files, cfg), model.StatusFail, map[string][]string{tc.scope: {tc.id}})
		})
	}
}
