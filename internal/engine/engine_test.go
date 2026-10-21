package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/facts"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
	_ "github.com/fleetlint/fleetlint/internal/rules/builtin"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func run(t *testing.T, files map[string]string, cfg string) *engine.Run {
	t.Helper()
	if cfg != "" {
		files[config.FileName] = cfg
	}
	r := testutil.GitFixture(t, files)
	eff, err := config.Load(r.Root, config.Options{Now: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.Background(), r, eff)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func status(t *testing.T, run *engine.Run, id string) model.Result {
	t.Helper()
	for _, r := range run.Results {
		if r.Rule.ID == id && r.Scope == "" {
			return r
		}
	}
	t.Fatalf("rule %s not evaluated", id)
	return model.Result{}
}

// resultOf is status for a rule and capStatus for a capability.
func resultOf(t *testing.T, run *engine.Run, id string) model.Result {
	t.Helper()
	for _, r := range run.Results {
		if r.Rule.Capability == id {
			return capStatus(t, run, id)
		}
	}
	return status(t, run, id)
}

// capStatus is a capability's result at the root, as a reader would sum it
// up: an implementation that did not pass if there is one (each must pass;
// combine: any has already settled its own), otherwise one that passed (a
// stack's own before the stack-neutral one), otherwise a not-applicable one.
func capStatus(t *testing.T, run *engine.Run, capability string) model.Result {
	t.Helper()
	var passed, skipped *model.Result
	for i := range run.Results {
		r := &run.Results[i]
		if r.Scope != "" || r.Rule.Capability != capability {
			continue
		}
		switch r.Status {
		case model.StatusNotApplicable:
			if skipped == nil {
				skipped = r
			}
		case model.StatusPass:
			if passed == nil || passed.Rule.Stack == "any" {
				passed = r
			}
		case model.StatusFail, model.StatusError, model.StatusExcepted, model.StatusBaselined:
			return *r
		}
	}
	switch {
	case passed != nil:
		return *passed
	case skipped != nil:
		return *skipped
	}
	t.Fatalf("no implementation of %s evaluated", capability)
	return model.Result{}
}

func TestExprRulesPassAndFail(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{"go.mod": "module x\n", ".gitignore": "bin/\n"}, "version: 1\nextends: [fleetlint:minimal]\n")
	if s := status(t, out, "repo/gitignore-present"); s.Status != model.StatusPass {
		t.Fatalf("gitignore present should pass: %+v", s)
	}
	if s := status(t, out, "hooks/config-present"); s.Status != model.StatusNotApplicable {
		t.Fatalf("tier 3 repo: hooks rule n/a, got %+v", s)
	}
	out2 := run(t, map[string]string{"go.mod": "module x\n"}, "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 1}\n")
	s := status(t, out2, "hooks/config-present")
	if s.Status != model.StatusFail || s.Severity != model.SeverityError || len(s.Findings) != 1 {
		t.Fatalf("missing hooks at tier 1 should fail: %+v", s)
	}
}

func TestExceptionsCoverFindings(t *testing.T) {
	t.Parallel()
	cfg := `version: 1
extends: [fleetlint:minimal]
exceptions:
  - rule: repo/no-tracked-junk
    match: "**/*.log"
    reason: "parser fixtures"
  - rule: repo/no-tracked-junk
    match: ".DS_Store"
    reason: "expired one"
    until: 2020-01-01
`
	out := run(t, map[string]string{"logs/a.log": "x", ".DS_Store": "x"}, cfg)
	s := status(t, out, "repo/no-tracked-junk")
	if s.Status != model.StatusFail {
		t.Fatalf("expired exception must not cover: %+v", s)
	}
	var covered, open int
	for _, f := range s.Findings {
		if f.Exception != nil {
			covered++
		} else {
			open++
		}
	}
	if covered != 1 || open != 1 {
		t.Fatalf("want 1 covered + 1 open, got %d/%d", covered, open)
	}
	full := run(t, map[string]string{"logs/a.log": "x"}, cfg)
	if s := status(t, full, "repo/no-tracked-junk"); s.Status != model.StatusExcepted {
		t.Fatalf("fully covered rule should be excepted: %+v", s)
	}
}

func TestInlineRule(t *testing.T) {
	t.Parallel()
	cfg := `version: 1
extends: [fleetlint:minimal]
rules:
  - id: repo/arch-doc
    kind: expr
    severity: warning
    expr: file("docs/ARCHITECTURE.md")
    message: "no architecture doc"
    fix: { human: "write docs/ARCHITECTURE.md" }
`
	out := run(t, map[string]string{"README.md": "x"}, cfg)
	if s := status(t, out, "repo/arch-doc"); s.Status != model.StatusFail || s.Findings[0].Message != "no architecture doc" {
		t.Fatalf("inline rule should fail with its message: %+v", s)
	}
}

// Expressions are type-checked when the configuration loads, for every
// rule, whether or not it applies to this repository: a typo in a tier-1
// rule fails a tier-3 repository too, with the rule and the field named.
func TestCompileErrorsFailTheLoad(t *testing.T) {
	t.Parallel()
	for field, rule := range map[string]string{
		"expr":    "    expr: nosuchfn(\"x\")\n",
		"when":    "    when: nosuchfn()\n    expr: \"true\"\n",
		"foreach": "    foreach: \"1\"\n    expr: \"true\"\n",
	} {
		cfg := "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\nrules:\n  - id: repo/broken\n    kind: expr\n    severity: warning\n    tiers: [1]\n" + rule + "    message: x\n    fix: { human: x }\n"
		r := testutil.GitFixture(t, map[string]string{config.FileName: cfg})
		_, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
		if err == nil || !strings.Contains(err.Error(), "repo/broken: "+field) {
			t.Errorf("%s: %v", field, err)
		}
	}
}

func TestScopes(t *testing.T) {
	t.Parallel()
	cfg := `version: 1
extends: [fleetlint:recommended]
facts: {tier: 2}
scopes:
  - path: backend
    facts: {stacks: [python]}
  - path: web
    facts: {stacks: [node]}
    overrides:
      taskrunner/targets: {enabled: false, reason: "npm scripts"}
`
	out := run(t, map[string]string{
		"go.mod":                 "module x\n",
		"backend/pyproject.toml": "[tool.ruff]\nx=1\n[tool.mypy]\nstrict=true\n",
		"web/package.json":       "{}",
		"web/tsconfig.json":      `{"compilerOptions":{"strict":true}}`,
		"web/eslint.config.js":   "export default []",
	}, cfg)
	var sawBackendPython, sawWebTargets, sawRootTargets bool
	for _, r := range out.Results {
		if r.Scope == "backend" && r.Rule.ID == "lint/configured/python" {
			sawBackendPython = true
			if r.Status != model.StatusPass {
				t.Errorf("backend python lint should pass: %+v", r)
			}
		}
		if r.Rule.ID == "taskrunner/targets" {
			switch r.Scope {
			case "web":
				sawWebTargets = true
			case "":
				sawRootTargets = true
			}
		}
	}
	if !sawBackendPython {
		t.Error("backend scope should evaluate the python rule")
	}
	if sawWebTargets {
		t.Error("rule disabled in the web scope must not be evaluated there")
	}
	if !sawRootTargets {
		t.Error("a scope-level disable must not leak into the root")
	}
	var disabledInWeb bool
	for _, d := range out.Config.Disabled {
		if d.ID == "taskrunner/targets" && strings.HasPrefix(d.Where, "web") && d.Reason == "npm scripts" {
			disabledInWeb = true
		}
	}
	if !disabledInWeb {
		t.Errorf("scope disable must be recorded with its reason and scope: %+v", out.Config.Disabled)
	}
	if len(out.Scopes) != 3 {
		t.Fatalf("want root + 2 scopes, got %d", len(out.Scopes))
	}
}

func TestPublicRepoRules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteFiles(t, dir, map[string]string{"CLAUDE.md": "rules", "go.mod": "module x\n"})
	_ = os.WriteFile(filepath.Join(dir, config.FileName), []byte("version: 1\nextends: [fleetlint:minimal, fleetlint:slop]\nfacts: {visibility: public}\n"), 0o644)
	r := testutil.GitFixture(t, map[string]string{"CLAUDE.md": "rules", "go.mod": "module x\n", config.FileName: "version: 1\nextends: [fleetlint:minimal, fleetlint:slop]\nfacts: {visibility: public}\n"})
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.Background(), r, eff)
	if err != nil {
		t.Fatal(err)
	}
	if s := status(t, out, "public/no-agent-files"); s.Status != model.StatusFail {
		t.Fatalf("tracked CLAUDE.md in a public repo must fail: %+v", s)
	}
	if out.Facts.Tier != 1 || out.Facts.Sources["tier"] != "inferred" || out.Facts.TierReason != "public" {
		t.Fatalf("public repos default to tier 1, got %d (%s)", out.Facts.Tier, out.Facts.Sources["tier"])
	}
}

func TestWorkspaceAutoScopes(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{
		"go.work":                 "go 1.24\nuse (\n\t./api\n\t./worker\n)\n",
		"api/go.mod":              "module a\n",
		"worker/go.mod":           "module w\n",
		".golangci.yml":           "version: \"2\"\n",
		"Makefile":                "check: lint test\nlint:\n\tx\ntest:\n\tx\nfmt:\n\tx\ncover:\n\tx\naudit:\n\tx\ncheck-fast:\n\tx\n",
		".pre-commit-config.yaml": "repos: []\n",
	}, "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 2}\n")
	if len(out.Scopes) != 3 || out.Scopes[1].Path != "api" || out.Scopes[2].Path != "worker" {
		t.Fatalf("expected root + api + worker scopes, got %+v", out.Scopes)
	}
	var rootLint, apiLint, apiTaskrunner, rootTaskrunner bool
	for _, r := range out.Results {
		switch {
		case r.Scope == "" && r.Rule.ID == "lint/configured/go":
			rootLint = true
		case r.Scope == "api" && r.Rule.ID == "lint/configured/go":
			apiLint = r.Status == model.StatusPass
		case r.Scope == "api" && r.Rule.ID == "taskrunner/targets":
			apiTaskrunner = true
		case r.Scope == "" && r.Rule.ID == "taskrunner/targets":
			rootTaskrunner = true
		}
	}
	if rootLint {
		t.Error("per-stack rule must not run at a root that only hosts members")
	}
	if !apiLint {
		t.Error("per-stack rule must run inside a member and see the configuration at the root")
	}
	if apiTaskrunner || !rootTaskrunner {
		t.Error("scope: root rules run only at the root")
	}
	explicit := run(t, map[string]string{
		"go.work": "go 1.24\nuse ./api\n", "api/go.mod": "module a\n",
	}, "version: 1\nextends: [fleetlint:minimal]\nscopes: []\n")
	if len(explicit.Scopes) != 1 {
		t.Fatalf("scopes: [] must disable discovery, got %d scopes", len(explicit.Scopes))
	}
}

func TestForeachNamesEachFailingItem(t *testing.T) {
	t.Parallel()
	cfg := `version: 1
extends: [fleetlint:minimal]
rules:
  - id: ci/every-workflow-has-name
    kind: expr
    severity: warning
    foreach: glob(".github/workflows/*.yml")
    expr: has(yaml(item).name)
    message: "workflow has no name"
    fix: { human: "add name:" }
`
	out := run(t, map[string]string{
		".github/workflows/a.yml": "name: a\non: push\n",
		".github/workflows/b.yml": "on: push\n",
		".github/workflows/c.yml": "on: push\n",
	}, cfg)
	s := status(t, out, "ci/every-workflow-has-name")
	if s.Status != model.StatusFail || len(s.Findings) != 2 {
		t.Fatalf("want 2 findings, got %+v", s)
	}
	if s.Findings[0].Path != ".github/workflows/b.yml" || s.Findings[1].Path != ".github/workflows/c.yml" {
		t.Fatalf("findings should be located at the failing files: %+v", s.Findings)
	}
}

const relWorkflow = "on:\n  push:\n    tags: ['v*']\njobs:\n  rel:\n    steps:\n      - uses: actions/checkout@v4\n      - run: goreleaser release --clean\n"

func TestOutcomeRuleSatisfiersAcceptAndPartials(t *testing.T) {
	t.Parallel()
	cfg := "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1}\n"

	pass := run(t, map[string]string{"go.mod": "module x\n", ".goreleaser.yaml": "sboms:\n  - artifacts: archive\n", ".github/workflows/release.yml": relWorkflow}, cfg)
	if s := capStatus(t, pass, "release/sbom"); s.Status != model.StatusPass || s.Evidence != "satisfied by goreleaser" {
		t.Fatalf("goreleaser satisfier: %+v", s)
	}

	viaCmd := run(t, map[string]string{"go.mod": "module x\n", ".github/workflows/release.yml": relWorkflow + "      - run: syft scan dir:. -o spdx-json=sbom.json\n"}, cfg)
	if s := capStatus(t, viaCmd, "release/sbom"); s.Status != model.StatusPass || s.Evidence != "satisfied by workflow-command" {
		t.Fatalf("workflow-command satisfier: %+v", s)
	}

	fail := run(t, map[string]string{"go.mod": "module x\n", ".github/workflows/release.yml": relWorkflow}, cfg)
	if s := capStatus(t, fail, "release/sbom"); s.Status != model.StatusFail || s.Severity != model.SeverityWarning || s.Findings[0].Evidence != "unsatisfied" {
		t.Fatalf("no producer must fail at warning: %+v", s)
	}

	partial := run(t, map[string]string{"go.mod": "module x\n", ".github/workflows/release.yml": relWorkflow, ".github/workflows/ci.yml": "on: push\njobs:\n  a:\n    steps:\n      - run: syft scan dir:.\n"}, cfg)
	if s := capStatus(t, partial, "release/sbom"); s.Status != model.StatusFail || s.Severity != model.SeverityInfo || s.Findings[0].Evidence != "sbom-outside-release" {
		t.Fatalf("producer outside release should be an info partial: %+v", s)
	}

	accepted := run(t, map[string]string{"go.mod": "module x\n", ".github/workflows/release.yml": relWorkflow + "      - run: ./scripts/sbom.sh\n"},
		cfg+"overrides:\n  release/sbom:\n    accept:\n      - name: own-script\n        expr: tagged_workflows().exists(w, steps(w).exists(s, s.run.contains(\"scripts/sbom.sh\")))\n")
	if s := capStatus(t, accepted, "release/sbom"); s.Status != model.StatusPass || s.Evidence != "satisfied by own-script" {
		t.Fatalf("accept should add a satisfier: %+v", s)
	}

	notRelease := run(t, map[string]string{"go.mod": "module x\n"}, cfg)
	if s := capStatus(t, notRelease, "release/sbom"); s.Status != model.StatusNotApplicable {
		t.Fatalf("no release pipeline: n/a, got %+v", s)
	}
	cliffOnly := run(t, map[string]string{"go.mod": "module x\n", "cliff.toml": "[changelog]\n"}, cfg)
	if s := status(t, cliffOnly, "release/sbom"); s.Status != model.StatusNotApplicable || !strings.Contains(s.Evidence, "no published artifact detected") {
		t.Fatalf("a changelog generator publishes nothing: n/a with the note, got %+v", s)
	}
	npmWorkflow := "on:\n  push:\n    tags: ['v*']\njobs:\n  rel:\n    steps:\n      - uses: actions/checkout@v4\n"
	registryOnly := run(t, map[string]string{"package.json": "{}", ".github/workflows/release.yml": npmWorkflow + "      - run: npm publish --provenance\n"}, cfg)
	if s := capStatus(t, registryOnly, "release/sbom"); s.Status != model.StatusNotApplicable {
		t.Fatalf("a release that only publishes to a registry: n/a, got %+v", s)
	}
	if s := status(t, registryOnly, "release/sbom/npm"); s.Status != model.StatusNotApplicable || !strings.Contains(s.Evidence, "does not apply to the artifact kind npm") {
		t.Fatalf("the npm implementation says why: %s %q", s.Status, s.Evidence)
	}
	withAssets := run(t, map[string]string{"package.json": "{}", ".github/workflows/release.yml": npmWorkflow + "      - run: npm publish\n      - run: gh release upload v1 dist/app.tgz\n"}, cfg)
	if s := capStatus(t, withAssets, "release/sbom"); s.Status != model.StatusFail || s.Rule.ID != "release/sbom/release-files" {
		t.Fatalf("a registry release that also uploads files is checked for the files, got %+v", s)
	}
}

func TestCommandRule(t *testing.T) { // not parallel: uses t.Setenv to prove the environment is scrubbed
	script := "#!/bin/sh\nread input\ncase \"$input\" in *'\"rule\":\"repo/custom\"'*) ;; *) echo 'bad input' >&2; exit 3;; esac\n" +
		"[ -z \"$SECRET_TOKEN\" ] || { echo 'env not scrubbed' >&2; exit 3; }\n" +
		"[ -f notes.txt ] && printf '{\"message\":\"notes.txt must not exist\",\"path\":\"notes.txt\",\"line\":1}\\n' && exit 1\nexit 0\n"
	cfg := "version: 1\nextends: [fleetlint:minimal]\nrules:\n  - id: repo/custom\n    kind: command\n    severity: warning\n    run: ./scripts/check\n    message: x\n    fix: {human: h}\n"
	t.Setenv("SECRET_TOKEN", "leak")
	files := map[string]string{"go.mod": "module x\n", "scripts/check": script, "notes.txt": "x", config.FileName: cfg}
	r := testutil.GitFixture(t, files)
	if err := os.Chmod(filepath.Join(r.Root, "scripts/check"), 0o755); err != nil {
		t.Fatal(err)
	}
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.Background(), r, eff)
	if err != nil {
		t.Fatal(err)
	}
	if s := status(t, out, "repo/custom"); s.Status != model.StatusNotApplicable || !strings.Contains(s.Evidence, "--deep") {
		t.Fatalf("command rules must not run without --deep: %+v", s)
	}
	deep := engine.Options{Deep: true}
	out, err = engine.EvaluateWith(context.Background(), r, eff, deep)
	if err != nil {
		t.Fatal(err)
	}
	s := status(t, out, "repo/custom")
	if s.Status != model.StatusFail || len(s.Findings) != 1 || s.Findings[0].Path != "notes.txt" || s.Findings[0].Line != 1 {
		t.Fatalf("command finding: %+v", s)
	}

	// not executable: a rule error, never a pass
	if err := os.Chmod(filepath.Join(r.Root, "scripts/check"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = engine.EvaluateWith(context.Background(), r, eff, deep)
	if err != nil {
		t.Fatal(err)
	}
	if s := status(t, out, "repo/custom"); s.Status != model.StatusError || !strings.Contains(s.Err, "not executable") {
		t.Fatalf("non-executable must be a rule error: %+v", s)
	}

	// untracked script: a rule error
	untracked := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", config.FileName: cfg})
	testutil.WriteFiles(t, untracked.Root, map[string]string{"scripts/check": script})
	if err := os.Chmod(filepath.Join(untracked.Root, "scripts", "check"), 0o755); err != nil {
		t.Fatal(err)
	}
	eff2, err := config.Load(untracked.Root, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	if out, err = engine.EvaluateWith(context.Background(), untracked, eff2, deep); err != nil {
		t.Fatal(err)
	}
	if s := status(t, out, "repo/custom"); s.Status != model.StatusError || !strings.Contains(s.Err, "not a git-tracked") {
		t.Fatalf("untracked script must be a rule error: %+v", s)
	}
}

func TestNestedConfigInScope(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{
		"go.work":             "go 1.24\nuse (\n\t./api\n\t./worker\n)\n",
		"api/go.mod":          "module a\n",
		"api/.DS_Store":       "x",
		"api/.fleetlint.yaml": "version: 1\nexceptions:\n  - rule: acme/no-ds-store\n    reason: \"api fixture\"\noverrides:\n  lint/configured/go: {enabled: false, reason: \"api has no Go linter yet\"}\n",
		"worker/go.mod":       "module w\n",
		"worker/.DS_Store":    "x",
		".golangci.yml":       "version: \"2\"\n",
	}, "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 2}\nrules:\n  - {id: acme/no-ds-store, kind: expr, severity: error, foreach: 'tracked().filter(p, p.endsWith(\".DS_Store\"))', expr: \"false\", message: m, fix: {human: h}}\n")
	var apiJunk, workerJunk, apiLintSeen bool
	for _, r := range out.Results {
		switch {
		case r.Scope == "api" && r.Rule.ID == "acme/no-ds-store":
			apiJunk = r.Status == model.StatusExcepted
		case r.Scope == "worker" && r.Rule.ID == "acme/no-ds-store":
			workerJunk = r.Status == model.StatusFail
		case r.Scope == "api" && r.Rule.ID == "lint/configured/go":
			apiLintSeen = true
		}
	}
	if !apiJunk {
		t.Error("nested exception must cover the api scope's junk finding")
	}
	if !workerJunk {
		t.Error("the nested exception must not leak into the worker scope")
	}
	if apiLintSeen {
		t.Error("nested disable must remove the rule in that scope")
	}
	bad := map[string]string{
		"go.work": "go 1.24\nuse ./api\n", "api/go.mod": "module a\n",
		"api/.fleetlint.yaml": "version: 1\nrules:\n  - {id: x/y, kind: expr, expr: 'true'}\n",
	}
	r := testutil.GitFixture(t, bad)
	eff, _ := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
	if _, err := engine.Evaluate(context.Background(), r, eff); err == nil || !strings.Contains(err.Error(), "unknown field \"rules\"") {
		t.Fatalf("nested config defining a rule must be rejected, got %v", err)
	}
}

func TestMalformedBudgetIsRuleError(t *testing.T) {
	t.Parallel()
	cfg := "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 2}\noverrides:\n  quality/check-passes: {params: {budget: soon}}\n"
	files := map[string]string{"go.mod": "module x\n", "Makefile": "check:\n\ttrue\n", config.FileName: cfg}
	r := testutil.GitFixture(t, files)
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.EvaluateWith(context.Background(), r, eff, engine.Options{Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := status(t, out, "quality/check-passes"); s.Status != model.StatusError || !strings.Contains(s.Err, "budget") {
		t.Fatalf("malformed budget must surface: %+v", s)
	}
}

func TestNestedExceptionOnForbiddenRuleIsError(t *testing.T) {
	t.Parallel()
	org := "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: org, version: 1}\nrules:\n  - {id: org/strict, title: t, kind: expr, severity: error, locked: true, exceptions: false, expr: 'false', requirement: r, fix: {human: h}}\n"
	r := testutil.GitFixture(t, map[string]string{
		"go.work":             "go 1.24\nuse ./api\n",
		"api/go.mod":          "module a\n",
		"org.yaml":            org,
		config.FileName:       "version: 1\nextends: [./org.yaml]\n",
		"api/.fleetlint.yaml": "version: 1\nexceptions:\n  - {rule: org/strict, reason: \"nope\"}\n",
	})
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Evaluate(context.Background(), r, eff); err == nil || !strings.Contains(err.Error(), "forbids exceptions") {
		t.Fatalf("a nested exception on an exceptions:false rule must fail the run: %v", err)
	}
}

// Stacks and task runner live per directory under scopes: `path: .` is the
// root, and a member no longer inherits the root's stacks. Listing only the
// root keeps member discovery.
func TestStacksPerDirectory(t *testing.T) {
	t.Parallel()
	files := func() map[string]string {
		return map[string]string{
			"go.mod": "module x\n", "Makefile": "check:\n\tgo test ./...\n",
			"web/package.json": `{"scripts":{"check":"vitest"}}`,
		}
	}
	scopeFacts := func(out *engine.Run, path string) facts.Facts {
		for _, s := range out.Scopes {
			if s.Path == path {
				return s.Facts
			}
		}
		t.Fatalf("no scope %q in %+v", path, out.Scopes)
		return facts.Facts{}
	}
	out := run(t, files(), "version: 1\nextends: [fleetlint:minimal]\nscopes:\n  - {path: ., stacks: [go], taskrunner: make}\n  - {path: web, stacks: [node], taskrunner: npm-scripts}\n")
	if f := scopeFacts(out, ""); !reflect.DeepEqual(f.Stacks, []string{"go"}) || f.TaskRunner.Kind != "make" {
		t.Errorf("root: %v %s", f.Stacks, f.TaskRunner.Kind)
	}
	if f := scopeFacts(out, "web"); !reflect.DeepEqual(f.Stacks, []string{"node"}) || f.TaskRunner.Kind != "npm-scripts" {
		t.Errorf("web: %v %s", f.Stacks, f.TaskRunner.Kind)
	}
	// The 0.2 form still works for one release, now for the root only.
	old := run(t, files(), "version: 1\nextends: [fleetlint:minimal]\nfacts: {stacks: [go], taskrunner: make}\n")
	if !reflect.DeepEqual(scopeFacts(old, "web").Stacks, []string{"node"}) {
		t.Errorf("a member must not inherit the root's pinned stacks: %v", scopeFacts(old, "web").Stacks)
	}
	if len(old.Config.Deprecations) != 1 {
		t.Errorf("deprecations: %v", old.Config.Deprecations)
	}
	onlyRoot := run(t, files(), "version: 1\nextends: [fleetlint:minimal]\nscopes:\n  - {path: ., stacks: [go]}\n")
	if len(onlyRoot.Scopes) != 2 || len(onlyRoot.Config.Deprecations) != 0 {
		t.Errorf("listing only the root keeps discovery: %+v", onlyRoot.Scopes)
	}
}

// An artifact is checked by the implementation for its kind, whatever the
// stacks: an image pushed from a workflow without a stack is checked by
// release/signing/image, and signing by tag is a partial.
func TestArtifactImplementationsRunByKind(t *testing.T) {
	t.Parallel()
	image := "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - run: docker push ghcr.io/x/y:${GITHUB_REF_NAME}\n"
	unsigned := run(t, map[string]string{".github/workflows/release.yml": image}, tier1Public)
	if got := unsigned.Facts.ArtifactKinds(); len(got) != 1 || got[0] != "image" {
		t.Fatalf("artifacts: %v", unsigned.Facts.Artifacts)
	}
	if s := status(t, unsigned, "release/signing/image"); s.Status != model.StatusFail {
		t.Errorf("an unsigned image must fail: %s %q", s.Status, s.Evidence)
	}
	if s := status(t, unsigned, "release/signing/release-files"); s.Status != model.StatusNotApplicable {
		t.Errorf("no release files are published: %s %q", s.Status, s.Evidence)
	}
	if s := status(t, unsigned, "deps/vulnerability-scan/any"); s.Status != model.StatusNotApplicable {
		t.Errorf("no stack, no dependencies to scan: %s %q", s.Status, s.Evidence)
	}
	byTag := run(t, map[string]string{".github/workflows/release.yml": image + "      - run: cosign sign --yes ghcr.io/x/y:${GITHUB_REF_NAME}\n"}, tier1Public)
	if s := status(t, byTag, "release/signing/image"); s.Status != model.StatusFail || s.Severity != model.SeverityWarning || s.Findings[0].Evidence != "cosign-by-tag" {
		t.Errorf("signing by tag is a warning partial: %s %s %+v", s.Status, s.Severity, s.Findings)
	}
	signed := run(t, map[string]string{".github/workflows/release.yml": image + "      - run: cosign sign --yes ghcr.io/x/y@${DIGEST}\n"}, tier1Public)
	if s := status(t, signed, "release/signing/image"); s.Status != model.StatusPass || s.Evidence != "ghcr.io/x/y: satisfied by cosign-by-digest" {
		t.Errorf("a digest-signed image passes: %s %q", s.Status, s.Evidence)
	}
	// Images pushed on every push to main count too: :latest deserves a signature.
	onMain := run(t, map[string]string{".github/workflows/publish.yml": "on:\n  push:\n    branches: [main]\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: docker/build-push-action@v6\n        with:\n          push: true\n"}, tier1Public)
	if s := status(t, onMain, "release/signing/image"); s.Status != model.StatusFail {
		t.Errorf("an image pushed from main is published: %s %q", s.Status, s.Evidence)
	}
	// Pinned facts replace detection, an empty list says nothing is published.
	pinned := run(t, map[string]string{".github/workflows/release.yml": image}, "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public, artifacts: [npm]}\n")
	if got := pinned.Facts.ArtifactKinds(); len(got) != 1 || got[0] != "npm" || pinned.Facts.Sources["artifacts"] != "configured" {
		t.Errorf("pinned artifacts: %v %v", pinned.Facts.Artifacts, pinned.Facts.Sources["artifacts"])
	}
	if s := status(t, pinned, "release/signing/image"); s.Status != model.StatusNotApplicable {
		t.Errorf("a pinned list replaces detection: %s", s.Status)
	}
	none := run(t, map[string]string{".github/workflows/release.yml": image}, "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public, artifacts: []}\n")
	if s := status(t, none, "release/signing"); s.Status != model.StatusNotApplicable || !strings.Contains(s.Evidence, "no published artifact detected") {
		t.Errorf("nothing published: the capability says so: %+v", s)
	}
}

// A repository that publishes two kinds must satisfy each: a Go CLI shipped
// as release files and as an npm package needs provenance on both, and the
// report names the one that lacks it.
func TestEachPublishedArtifactIsChecked(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"go.mod":                        "module x\n",
		"package.json":                  "{\"name\": \"x\"}\n",
		".goreleaser.yaml":              "builds: [{main: .}]\n",
		".github/workflows/release.yml": "on:\n  push:\n    tags: ['v*']\npermissions:\n  id-token: write\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - run: goreleaser release --clean\n      - run: npm publish --provenance --access public\n",
	}
	out := run(t, files, tier1Public)
	if got := out.Facts.ArtifactKinds(); len(got) != 2 || got[0] != "npm" || got[1] != "release-files" {
		t.Fatalf("artifacts: %v", out.Facts.Artifacts)
	}
	if s := status(t, out, "release/provenance/npm"); s.Status != model.StatusPass {
		t.Errorf("npm provenance: %s %q", s.Status, s.Evidence)
	}
	if s := status(t, out, "release/provenance/release-files"); s.Status != model.StatusFail {
		t.Errorf("the release files have no attestation and npm's does not carry them: %s %q", s.Status, s.Evidence)
	}
	if s := capStatus(t, out, "release/provenance"); s.Rule.ID != "release/provenance/release-files" {
		t.Errorf("the capability sums up to the failing kind: %s", s.Rule.ID)
	}
	if s := status(t, out, "lint/configured/go"); s.Status != model.StatusFail {
		t.Errorf("stack capabilities are untouched: %s", s.Status)
	}
}

// An implementation keyed by artifact asks its own `when` per published
// artifact: trusted publishing applies to an npm package published from
// GitHub, not to one published from Gitea, and when no artifact applies the
// rule is not applicable.
func TestImplementationWhenIsAskedPerArtifact(t *testing.T) {
	t.Parallel()
	// Neither workflow has id-token: write, so a checked package fails.
	publish := "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm publish\n"
	files := map[string]string{"package.json": "{\"name\": \"x\"}\n", ".github/workflows/release.yml": publish, ".gitea/workflows/release.yml": publish}
	both := run(t, files, "version: 1\nextends: [fleetlint:recommended]\nfacts:\n  tier: 1\n  visibility: public\n  artifacts:\n    - {kind: npm, name: hub, workflows: [.github/workflows/release.yml]}\n    - {kind: npm, name: tea, workflows: [.gitea/workflows/release.yml]}\n")
	if s := status(t, both, "release/trusted-publishing/npm"); s.Status != model.StatusFail || len(s.Findings) != 1 || !strings.HasPrefix(s.Findings[0].Message, "hub: ") {
		t.Errorf("the GitHub one is checked, the Gitea one skipped: %s %+v", s.Status, s.Findings)
	}
	teaOnly := run(t, files, "version: 1\nextends: [fleetlint:recommended]\nfacts:\n  tier: 1\n  visibility: public\n  artifacts:\n    - {kind: npm, name: tea, workflows: [.gitea/workflows/release.yml]}\n")
	if s := status(t, teaOnly, "release/trusted-publishing/npm"); s.Status != model.StatusNotApplicable || !strings.HasPrefix(s.Evidence, "when: ") {
		t.Errorf("no artifact applies: not applicable with the when: %s %q", s.Status, s.Evidence)
	}
}
