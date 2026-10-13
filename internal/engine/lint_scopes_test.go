package engine_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/model"
)

func scoped(t *testing.T, results []model.Result, scope, id string) model.Result {
	t.Helper()
	for _, r := range results {
		if r.Scope == scope && r.Rule.ID == id {
			return r
		}
	}
	t.Fatalf("rule %s not evaluated in %q", id, scope)
	return model.Result{}
}

// Linters, formatters and type checkers that read their configuration from a
// parent directory pass in a workspace member when the configuration sits at
// the root.
func TestLintConfigAtTheWorkspaceRoot(t *testing.T) {
	t.Parallel()
	const cfg = "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1}\nscopes:\n  - path: api\n  - path: packages/web\n  - path: crates/a\n  - path: crates/b\n  - path: svc\n"
	out := run(t, map[string]string{
		"go.work":       "go 1.24\nuse ./api\n",
		"api/go.mod":    "module a\n",
		".golangci.yml": "version: \"2\"\nformatters:\n  enable: [gofumpt]\nlinters:\n  enable: [errcheck, govet, staticcheck, errorlint, gosec, gocognit, funlen]\n",

		"packages/web/package.json":  "{}",
		"packages/web/tsconfig.json": `{"extends": "../../tsconfig.base.json", "include": ["src"]}`,
		"tsconfig.base.json":         `{"compilerOptions": {"strict": true, "noUncheckedIndexedAccess": true, "exactOptionalPropertyTypes": true, "noImplicitOverride": true, "noFallthroughCasesInSwitch": true}}`,
		"eslint.config.mjs":          "export default [];\n",
		".prettierrc.json":           "{}",

		"Cargo.toml":          "[workspace]\nmembers = [\"crates/a\", \"crates/b\"]\n[workspace.lints.clippy]\nunwrap_used = \"deny\"\nexpect_used = \"deny\"\npanic = \"deny\"\ntodo = \"deny\"\ndbg_macro = \"deny\"\n",
		"crates/a/Cargo.toml": "[package]\nname = \"a\"\n[lints]\nworkspace = true\n",
		"crates/b/Cargo.toml": "[package]\nname = \"b\"\n",

		"pyproject.toml":     "[tool.ruff.lint]\nselect = [\"ALL\"]\n[tool.mypy]\nstrict = true\n",
		"svc/pyproject.toml": "[project]\nname = \"svc\"\n",
	}, cfg)
	for scope, ids := range map[string][]string{
		"api":          {"lint/configured/go", "lint/strict/go", "format/configured/go"},
		"packages/web": {"lint/configured/node", "types/strict/node", "format/configured/node"},
		"crates/a":     {"lint/configured/rust", "lint/strict/rust"},
		"svc":          {"lint/configured/python", "lint/strict/python", "types/strict/python"},
	} {
		for _, id := range ids {
			if s := scoped(t, out.Results, scope, id); s.Status != model.StatusPass {
				t.Errorf("%s in %s: %s %q %+v", id, scope, s.Status, s.Err, s.Findings)
			}
		}
	}
	if s := scoped(t, out.Results, "crates/b", "lint/configured/rust"); s.Status != model.StatusFail || !strings.Contains(s.Findings[0].Message, "workspace = true") {
		t.Errorf("a crate that does not inherit the workspace lints is told how: %s %+v", s.Status, s.Findings)
	}
}
