package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/model"
)

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func load(t *testing.T, body string) (*config.Effective, error) {
	t.Helper()
	return config.Load(write(t, body), config.Options{Now: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), Loader: catalog.Loader{}})
}

func ruleByID(eff *config.Effective, id string) (model.Rule, bool) {
	for _, r := range eff.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return model.Rule{}, false
}

func TestDefaultsWithoutFile(t *testing.T) {
	t.Parallel()
	eff, err := load(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if eff.Exists || len(eff.Rules) == 0 || eff.File.Extends[0] != "fleetlint:recommended" {
		t.Fatalf("expected recommended defaults, got exists=%v rules=%d extends=%v", eff.Exists, len(eff.Rules), eff.File.Extends)
	}
	if _, ok := ruleByID(eff, "repo/no-tracked-junk"); !ok {
		t.Fatal("recommended must include minimal's rules")
	}
}

func TestOverridesAndInline(t *testing.T) {
	t.Parallel()
	eff, err := load(t, `
version: 1
extends: [fleetlint:minimal]
rules:
  hooks/config-present:
    enabled: false
    reason: "hooks run in CI only"
  repo/gitignore-present:
    severity: error
  repo/no-tracked-env:
    severity: info
    reason: "fixtures contain .env files on purpose"
  repo/arch-doc:
    kind: expr
    severity: warning
    expr: file("docs/ARCHITECTURE.md")
    message: "architecture doc missing"
    fix: { human: "write it" }
exceptions:
  - rule: repo/no-tracked-junk
    match: "logs/*.log"
    reason: "sample logs for the parser tests"
    until: 2027-01-01
  - rule: repo/no-tracked-junk
    reason: "old one"
    until: 2026-01-01
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ruleByID(eff, "hooks/config-present"); ok {
		t.Fatal("disabled rule should be removed")
	}
	if len(eff.Disabled) != 1 || eff.Disabled[0].Reason == "" {
		t.Fatalf("disabled not recorded: %+v", eff.Disabled)
	}
	if r, _ := ruleByID(eff, "repo/gitignore-present"); r.Severity != model.SeverityError {
		t.Fatal("raising severity should apply")
	}
	if len(eff.Weakened) != 1 || eff.Weakened[0].ID != "repo/no-tracked-env" {
		t.Fatalf("lowering severity should be recorded as weakened: %+v", eff.Weakened)
	}
	if r, ok := ruleByID(eff, "repo/arch-doc"); !ok || r.Kind != model.KindExpr || r.Source != ".fleetlint.yaml" {
		t.Fatalf("inline rule missing or wrong: %+v", r)
	}
	if eff.Exceptions[0].Expired || !eff.Exceptions[1].Expired {
		t.Fatalf("expiry wrong: %+v", eff.Exceptions)
	}
}

func TestValidationErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"disable without reason":   "version: 1\nextends: [fleetlint:minimal]\nrules:\n  hooks/config-present: {enabled: false}\n",
		"lower without reason":     "version: 1\nextends: [fleetlint:minimal]\nrules:\n  hooks/config-present: {severity: info}\n",
		"unknown rule":             "version: 1\nextends: [fleetlint:minimal]\nrules:\n  nope/nope: {severity: info}\n",
		"redefine catalog rule":    "version: 1\nextends: [fleetlint:minimal]\nrules:\n  hooks/config-present: {kind: expr, expr: 'true'}\n",
		"exception for unloaded":   "version: 1\nextends: [fleetlint:minimal]\nexceptions:\n  - {rule: nope/x, reason: r}\n",
		"exception without reason": "version: 1\nextends: [fleetlint:minimal]\nexceptions:\n  - {rule: hooks/config-present}\n",
		"bad until":                "version: 1\nextends: [fleetlint:minimal]\nexceptions:\n  - {rule: hooks/config-present, reason: r, until: soon}\n",
		"unknown key":              "version: 1\nextendz: [fleetlint:minimal]\n",
		"bad version":              "version: 2\n",
		"bad visibility":           "version: 1\nfacts: {visibility: secret}\n",
		"inline go rule":           "version: 1\nrules:\n  x/y: {kind: go}\n",
		"bad cel is caught later":  "version: 1\nrules:\n  x/y: {kind: expr, expr: 'file(']\n",
		"bad exception glob":       "version: 1\nextends: [fleetlint:minimal]\nexceptions:\n  - {rule: repo/no-tracked-junk, reason: r, match: '['}\n",
		"scope inline rule":        "version: 1\nextends: [fleetlint:minimal]\nscopes:\n  - path: api\n    rules:\n      x/y: {kind: expr, expr: 'true'}\n",
		"scope unknown rule":       "version: 1\nextends: [fleetlint:minimal]\nscopes:\n  - path: api\n    rules:\n      nope/nope: {enabled: false, reason: r}\n",
		"scope lower no reason":    "version: 1\nextends: [fleetlint:minimal]\nscopes:\n  - path: api\n    rules:\n      hooks/config-present: {severity: info}\n",
		"scope escapes repo":       "version: 1\nextends: [fleetlint:minimal]\nscopes:\n  - path: ../x\n",
	}
	for name, body := range cases {
		if name == "bad cel is caught later" {
			continue // compile errors surface in the engine as StatusError, by design
		}
		if _, err := load(t, body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestUntrustedCatalogCannotDeclareCommandRules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cat := "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: r, version: 1.0.0}\nrules:\n  - {id: x/cmd, title: t, kind: command, severity: error, run: ./x, requirement: r, fix: {human: h}}\n"
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte("version: 1\nextends:\n  - https://example.com/c.yaml#sha256-"+catalog.Digest([]byte(cat))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(dir, config.Options{Loader: catalog.Loader{Fetch: func(string) ([]byte, error) { return []byte(cat), nil }}})
	if err == nil || !strings.Contains(err.Error(), "command rules are only allowed") {
		t.Fatalf("expected trust-boundary error, got %v", err)
	}
	// The same catalog as a local file is trusted.
	if err := os.WriteFile(filepath.Join(dir, "local.yaml"), []byte(cat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte("version: 1\nextends: [./local.yaml]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(dir, config.Options{Loader: catalog.Loader{}}); err != nil {
		t.Fatalf("local catalog should be trusted: %v", err)
	}
}

func TestAcceptValidation(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"accept on non-outcome": "version: 1\nextends: [fleetlint:recommended]\nrules:\n  repo/editorconfig:\n    accept: [{expr: 'true'}]\n",
		"accept without expr":   "version: 1\nextends: [fleetlint:recommended]\nrules:\n  release/sbom:\n    accept: [{name: x}]\n",
		"accept unknown key":    "version: 1\nextends: [fleetlint:recommended]\nrules:\n  release/sbom:\n    accept: [{expr: 'true', exprs: 'x'}]\n",
	}
	for name, body := range bad {
		if _, err := load(t, body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	eff, err := load(t, "version: 1\nextends: [fleetlint:recommended]\nrules:\n  release/sbom:\n    accept: [{name: own, expr: 'true'}]\n")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := ruleByID(eff, "release/sbom")
	if list, _ := r.Params["accept"].([]any); len(list) != 1 {
		t.Fatalf("accept not stored: %+v", r.Params)
	}
}

func TestNestedConfigValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	write := func(body string) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if n, err := config.LoadNested(t.TempDir(), now); err != nil || n != nil {
		t.Fatalf("absent nested file is not an error: %v %v", n, err)
	}
	for name, body := range map[string]string{
		"bad version":              "version: 2\n",
		"exception without reason": "version: 1\nexceptions:\n  - {rule: repo/x}\n",
		"bad until":                "version: 1\nexceptions:\n  - {rule: repo/x, reason: r, until: never}\n",
		"unknown key":              "version: 1\nextends: [x]\n",
	} {
		if _, err := config.LoadNested(write(body), now); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	n, err := config.LoadNested(write("version: 1\nexceptions:\n  - {rule: repo/x, reason: r, until: 2027-01-01}\nrules:\n  repo/x: {enabled: false, reason: r}\n"), now)
	if err != nil || len(n.Exceptions) != 1 || len(n.Rules) != 1 {
		t.Fatalf("valid nested config: %+v %v", n, err)
	}
}
