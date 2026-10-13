package engine_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// capabilitySource is a catalog with two capabilities: qa/linted has Go,
// Node and stack-neutral implementations, qa/typed only a Go one.
func capabilitySource() *catalog.Source {
	files := fstest.MapFS{
		"presets/p.yaml":   {Data: []byte("apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: p, engine: 3}\nrules:\n  - use: qa/*\n")},
		"stacks/go.yaml":   {Data: []byte("id: go\ntitle: Go\ndetect: [{file: go.mod}]\n")},
		"stacks/node.yaml": {Data: []byte("id: node\ntitle: Node\ndetect: [{file: package.json}]\n")},
		"stacks/rust.yaml": {Data: []byte("id: rust\ntitle: Rust\ndetect: [{file: Cargo.toml}]\n")},
		"rules/qa/linted.yaml": {Data: []byte(`id: qa/linted
title: Code is linted
kind: capability
severity: warning
when: '!file("SKIP")'
requirement: Every stack has a linter configured.
rationale: r
refs: ["https://example.org/lint"]
fix: {human: Configure the stack's linter., agent: a}
`)},
		"rules/qa/linted/go.yaml": {Data: []byte(`id: qa/linted/go
kind: outcome
satisfiers: [{name: golangci, expr: 'file(".golangci.yml")'}]
message: no Go linter configured
refs: ["https://example.org/go"]
`)},
		"rules/qa/linted/node.yaml": {Data: []byte(`id: qa/linted/node
kind: expr
expr: file("eslint.config.js")
message: no eslint configuration
`)},
		"rules/qa/linted/any.yaml": {Data: []byte(`id: qa/linted/any
kind: outcome
satisfiers: [{name: megalinter, expr: 'file(".mega-linter.yml")'}]
message: no linter covers this stack
`)},
		"rules/qa/typed.yaml":    {Data: []byte("id: qa/typed\ntitle: Types are checked\nkind: capability\nseverity: info\nrequirement: r\nfix: {human: h}\n")},
		"rules/qa/typed/go.yaml": {Data: []byte("id: qa/typed/go\nkind: expr\nexpr: \"true\"\n")},
	}
	return &catalog.Source{Presets: files, Rules: files, Stacks: files, Templates: files, Version: "v9.9.9"}
}

func runWith(t *testing.T, files map[string]string, cfg string) *engine.Run {
	t.Helper()
	files[config.FileName] = cfg
	r := testutil.GitFixture(t, files)
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{Source: capabilitySource()}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.Background(), r, eff)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func shown(run *engine.Run, id string) bool {
	for _, r := range run.Results {
		if r.Rule.ID == id && r.Scope == "" {
			return true
		}
	}
	return false
}

const capCfg = "version: 1\nextends: [fleetlint:p]\nfacts: {tier: 1}\n"

func TestCapabilityImplementationsPerStack(t *testing.T) {
	t.Parallel()
	out := runWith(t, map[string]string{"go.mod": "module x\n", "package.json": "{}"}, capCfg)
	goRes, nodeRes := status(t, out, "qa/linted/go"), status(t, out, "qa/linted/node")
	if goRes.Status != model.StatusFail || nodeRes.Status != model.StatusFail {
		t.Fatalf("each stack is checked by its own implementation: go=%s node=%s", goRes.Status, nodeRes.Status)
	}
	if goRes.Rule.Title != "Code is linted" || goRes.Rule.Severity != model.SeverityWarning || goRes.Rule.Capability != "qa/linted" || goRes.Rule.Stack != "go" {
		t.Errorf("an implementation inherits what it does not set: %+v", goRes.Rule)
	}
	if len(goRes.Rule.Refs) != 2 || goRes.Rule.Fix.Human != "Configure the stack's linter." {
		t.Errorf("references add up and the fix is inherited: %v %q", goRes.Rule.Refs, goRes.Rule.Fix.Human)
	}
	if s := status(t, out, "qa/linted/any"); s.Status != model.StatusNotApplicable {
		t.Errorf("the stack-neutral implementation stands back when every stack has its own: %s", s.Status)
	}
	if shown(out, "qa/linted") {
		t.Error("a fully implemented capability adds no result of its own")
	}
	if s := status(t, out, "qa/typed"); s.Status != model.StatusNotApplicable || !strings.Contains(s.Evidence, "no implementation for node") {
		t.Errorf("a stack without an implementation is reported on the capability: %s %q", s.Status, s.Evidence)
	}
}

func TestStackNeutralImplementation(t *testing.T) {
	t.Parallel()
	out := runWith(t, map[string]string{"go.mod": "module x\n", ".mega-linter.yml": "x"}, capCfg)
	if s := status(t, out, "qa/linted/go"); s.Status != model.StatusPass || s.Evidence != "satisfied by megalinter" {
		t.Errorf("the neutral satisfiers count for a stack's outcome implementation: %s %q", s.Status, s.Evidence)
	}
	rust := runWith(t, map[string]string{"Cargo.toml": "[package]\n"}, capCfg)
	if s := status(t, rust, "qa/linted/any"); s.Status != model.StatusFail {
		t.Errorf("a stack without its own implementation falls to the neutral one: %s", s.Status)
	}
	if shown(rust, "qa/linted") {
		t.Error("the neutral implementation closes the gap")
	}
}

func TestCapabilityOverridesAndExceptions(t *testing.T) {
	t.Parallel()
	cfg := capCfg + `overrides:
  qa/linted: {severity: error}
  qa/linted/node: {enabled: false, reason: "no js here yet"}
exceptions:
  - {rule: qa/linted, reason: "migrating"}
`
	out := runWith(t, map[string]string{"go.mod": "module x\n", "package.json": "{}"}, cfg)
	s := status(t, out, "qa/linted/go")
	if s.Rule.Severity != model.SeverityError || s.Status != model.StatusExcepted {
		t.Errorf("the capability's override and exception reach the implementation: %s %s", s.Rule.Severity, s.Status)
	}
	if shown(out, "qa/linted/node") {
		t.Error("an implementation can be switched off on its own")
	}
	gated := runWith(t, map[string]string{"go.mod": "module x\n", "SKIP": "x"}, capCfg)
	if s := status(t, gated, "qa/linted/go"); s.Status != model.StatusNotApplicable || !strings.Contains(s.Evidence, "capability when") {
		t.Errorf("the capability's when gates its implementations: %s %q", s.Status, s.Evidence)
	}
}

func TestPresetVersionComesFromTheSource(t *testing.T) {
	t.Parallel()
	out := runWith(t, map[string]string{"go.mod": "module x\n"}, capCfg)
	if s := status(t, out, "qa/linted/go"); s.Rule.SelectedBy != "p@v9.9.9" {
		t.Errorf("a preset without a version takes the catalog's: %q", s.Rule.SelectedBy)
	}
}

func TestBrokenImplementationsFailTheLoad(t *testing.T) {
	t.Parallel()
	for name, files := range map[string]fstest.MapFS{
		"no capability": {"rules/qa/x/go.yaml": {Data: []byte("id: qa/x/go\nkind: expr\nexpr: \"true\"\n")}},
		"stacks key":    {"rules/qa/linted.yaml": capabilitySource().Rules.(fstest.MapFS)["rules/qa/linted.yaml"], "rules/qa/linted/go.yaml": {Data: []byte("id: qa/linted/go\nkind: expr\nstacks: [go]\nexpr: \"true\"\n")}},
		"no kind":       {"rules/qa/linted.yaml": capabilitySource().Rules.(fstest.MapFS)["rules/qa/linted.yaml"], "rules/qa/linted/go.yaml": {Data: []byte("id: qa/linted/go\nexpr: \"true\"\n")}},
		"checking cap":  {"rules/qa/c.yaml": {Data: []byte("id: qa/c\ntitle: t\nkind: capability\nseverity: info\nrequirement: r\nexpr: \"true\"\nfix: {human: h}\n")}},
	} {
		src := &catalog.Source{Rules: files, Version: "x"}
		if _, err := src.Library(); err == nil {
			t.Errorf("%s: want a load error", name)
		}
	}
}

// A foreach item the expression builds (a map literal, map()) locates its
// finding like one an accessor returns.
func TestForeachItemsBuiltInCEL(t *testing.T) {
	t.Parallel()
	cfg := `version: 1
rules:
  - id: acme/built
    kind: expr
    severity: warning
    foreach: '["a.txt", "b.txt"].map(p, {"path": p, "line": 2, "name": "job-" + p, "ok": p == "a.txt"})'
    expr: item.ok
    message: not ok
`
	out := run(t, map[string]string{"a.txt": "x\n", "b.txt": "y\n"}, cfg)
	s := status(t, out, "acme/built")
	if s.Status != model.StatusFail || len(s.Findings) != 1 {
		t.Fatalf("one failing item: %+v", s)
	}
	if f := s.Findings[0]; f.Path != "b.txt" || f.Line != 2 || f.Message != "not ok (job-b.txt)" {
		t.Fatalf("the built map gives path, line and label: %+v", f)
	}
}

func TestDisabledCapabilityIsRecordedOnce(t *testing.T) {
	t.Parallel()
	out := runWith(t, map[string]string{"go.mod": "module x\n"}, capCfg+"overrides:\n  qa/linted: {enabled: false, reason: \"not yet\"}\n")
	if d := out.Config.Disabled; len(d) != 1 || d[0].ID != "qa/linted" {
		t.Fatalf("one entry for the capability, none per implementation: %+v", d)
	}
	if shown(out, "qa/linted/go") {
		t.Error("its implementations are off")
	}
}

// Gitea and Forgejo do not take the token's scope from the workflow file.
func TestLeastPrivilegeIsGitHubOnly(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{"go.mod": "module x\n", ".gitea/workflows/ci.yml": "on: push\njobs:\n  a:\n    runs-on: x\n    steps: [{run: make check}]\n"}, withSlop)
	if s := status(t, out, "ci/least-privilege"); s.Status != model.StatusNotApplicable {
		t.Fatalf("a Gitea workflow is not checked: %s", s.Status)
	}
}

// A 0.3 configuration keeps working: an old id in overrides, exceptions and
// use: reaches the rules that replaced it, with a deprecation warning.
func TestRenamedIDsCarryOver(t *testing.T) {
	t.Parallel()
	cfg := `version: 1
extends: [fleetlint:recommended]
facts: {tier: 1, visibility: public}
rules:
  - use: quality/no-slop-markers
overrides:
  lint/go-config: {severity: error}
  lint/python-config: {enabled: false, reason: "migrating"}
exceptions:
  - {rule: lint/go-linters-enabled, reason: "ratchet"}
`
	out := run(t, map[string]string{"go.mod": "module x\n", ".golangci.yml": "version: \"2\"\nlinters:\n  default: none\n"}, cfg)
	if s := status(t, out, "lint/configured/go"); s.Rule.Severity != model.SeverityError {
		t.Errorf("an override of lint/go-config reaches lint/configured/go: %s", s.Rule.Severity)
	}
	if s := status(t, out, "lint/strict/go"); s.Status != model.StatusExcepted {
		t.Errorf("an exception for lint/go-linters-enabled reaches lint/strict/go: %s", s.Status)
	}
	if shown(out, "types/strict/python") || shown(out, "lint/configured/python") {
		t.Error("disabling lint/python-config disables both rules that replace it")
	}
	if !shown(out, "slop/marketing-words") {
		t.Error("use: of a renamed id selects its replacement")
	}
	if !strings.Contains(strings.Join(out.Config.Deprecations, "\n"), "lint/go-config was renamed to lint/configured/go") {
		t.Errorf("the rename is reported: %v", out.Config.Deprecations)
	}
}

// Implementations and capabilities come from libraries only: an inline
// `<capability>/any` would otherwise satisfy a locked capability.
func TestInlineImplementationsAreRefused(t *testing.T) {
	t.Parallel()
	for name, rule := range map[string]string{
		"implementation": "  - {id: qa/linted/any, kind: outcome, severity: info, message: m, requirement: r, fix: {human: h}, satisfiers: [{name: bypass, expr: 'true'}]}\n",
		"capability":     "  - {id: qa/other, kind: capability, severity: info, title: t, requirement: r, fix: {human: h}}\n",
	} {
		r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", config.FileName: capCfg + "rules:\n" + rule})
		_, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{Source: capabilitySource()}})
		if err == nil || !strings.Contains(err.Error(), "rules/ library") {
			t.Errorf("%s inline: %v", name, err)
		}
	}
}

func TestCapabilityParamsAndPartialsInherit(t *testing.T) {
	t.Parallel()
	files := capabilitySource().Rules.(fstest.MapFS)
	files["rules/qa/p.yaml"] = &fstest.MapFile{Data: []byte("id: qa/p\ntitle: t\nkind: capability\nseverity: info\nrequirement: r\nfix: {human: h}\nparams: {limit: 5, mode: strict}\n")}
	files["rules/qa/p/go.yaml"] = &fstest.MapFile{Data: []byte("id: qa/p/go\nkind: expr\nexpr: params.limit == 5 && params.mode == \"loose\"\nparams: {mode: loose}\n")}
	lib, err := (&catalog.Source{Rules: files, Version: "x"}).Library()
	if err != nil {
		t.Fatal(err)
	}
	if ids, _ := lib.Select("qa/p"); len(ids) != 2 {
		t.Fatalf("capability and implementation: %v", ids)
	}
	files["rules/qa/p/go.yaml"] = &fstest.MapFile{Data: []byte("id: qa/p/go\nkind: outcome\nsatisfiers: [{name: a, expr: 'true'}]\npartials: [{name: b, expr: 'true', message: m}]\n")}
	if _, err := (&catalog.Source{Rules: files, Version: "y"}).Library(); err == nil || !strings.Contains(err.Error(), "severity is required") {
		t.Errorf("a partial of an implementation states its severity: %v", err)
	}
}

func TestDisabledCapabilityTakesItsImplementationOverrides(t *testing.T) {
	t.Parallel()
	out := runWith(t, map[string]string{"go.mod": "module x\n"}, capCfg+"overrides:\n  qa/linted: {enabled: false, reason: r}\n  qa/linted/go: {severity: error}\n")
	if shown(out, "qa/linted/go") {
		t.Error("the implementation stays off")
	}
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", config.FileName: capCfg + "overrides:\n  qa/typed:\n    accept: [{name: a, expr: 'true'}]\n"})
	if _, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{Source: capabilitySource()}}); err == nil || !strings.Contains(err.Error(), "accept only applies to outcome rules") {
		t.Errorf("accept on a capability without outcome implementations is an error: %v", err)
	}
}
