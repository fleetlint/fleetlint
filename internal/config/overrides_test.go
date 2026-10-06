package config_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/model"
)

const orgOverrides = `apiVersion: fleetlint.org/v1
kind: Catalog
metadata: {name: acme, version: 2.0.0, includes: ["fleetlint:minimal"]}
rules: []
overrides:
  repo/no-tracked-env: {locked: true, exceptions: false}
  hooks/secret-scan: {min_severity: warning}
  hooks/config-present: {severity: warning, reason: "being rolled out"}
  repo/gitignore-present: {enabled: false, reason: "generated repositories have none"}
`

func layered(repoCfg, org, team string) map[string]string {
	files := map[string]string{
		config.FileName: "version: 1\nsources: s.yaml\nextends: [org" + map[bool]string{true: ", team/a", false: ""}[team != ""] + "]\n" + repoCfg,
		"s.yaml":        "version: 1\ncatalogs:\n  org: org.yaml\n" + map[bool]string{true: "  team/a: team.yaml\n", false: ""}[team != ""],
		"org.yaml":      org,
	}
	if team != "" {
		files["team.yaml"] = team
	}
	return files
}

// A catalog adjusts the rules it includes without restating them, and what
// it sets binds the layers after it.
func TestCatalogOverrides(t *testing.T) {
	t.Parallel()
	eff, err := loadLayered(t, layered("", orgOverrides, ""))
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]model.Rule{}
	for _, r := range eff.Rules {
		rules[r.ID] = r
	}
	env := rules["repo/no-tracked-env"]
	if !env.Locked || env.ExceptionsAllowed() || env.PolicyBy != "acme@2.0.0" || env.Source == "acme@2.0.0" {
		t.Errorf("the included rule is locked by the catalog and keeps its origin: %+v", env)
	}
	if rules["hooks/secret-scan"].MinSeverity != "warning" || rules["hooks/config-present"].Severity != model.SeverityWarning {
		t.Errorf("floor and severity must apply: %+v / %+v", rules["hooks/secret-scan"], rules["hooks/config-present"])
	}
	if _, still := rules["repo/gitignore-present"]; still {
		t.Error("a rule disabled by the catalog is gone")
	}
	if len(eff.Disabled) != 1 || eff.Disabled[0].Layer != "org" || eff.Disabled[0].Reason == "" {
		t.Errorf("the disable is recorded with its layer and reason: %+v", eff.Disabled)
	}
	if len(eff.Weakened) != 1 || eff.Weakened[0].ID != "hooks/config-present" || eff.Weakened[0].Layer != "org" {
		t.Errorf("the lowered severity is recorded with its layer: %+v", eff.Weakened)
	}

	rejectedByRepo := map[string]string{
		"disable what the org locked":    "rules:\n  repo/no-tracked-env: {enabled: false, reason: r}\n",
		"except what forbids it":         "exceptions:\n  - {rule: repo/no-tracked-env, reason: r}\n",
		"go below the org's floor":       "rules:\n  hooks/secret-scan: {severity: info, reason: r}\n",
		"configure what the org removed": "rules:\n  repo/gitignore-present: {severity: info, reason: r}\n",
	}
	for name, cfg := range rejectedByRepo {
		if _, err := loadLayered(t, layered(cfg, orgOverrides, "")); err == nil {
			t.Errorf("%s: expected a config error", name)
		}
	}
}

func TestCatalogOverridesAreHeldToEarlierPolicy(t *testing.T) {
	t.Parallel()
	team := func(overrides string) string {
		return "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: team-a, version: 1.0.0}\nrules: []\noverrides:\n" + overrides
	}
	rejected := map[string]string{
		"unlock":                   "  repo/no-tracked-env: {locked: false}\n",
		"disable a locked rule":    "  repo/no-tracked-env: {enabled: false, reason: r}\n",
		"allow exceptions again":   "  repo/no-tracked-env: {exceptions: true}\n",
		"lower the floor":          "  hooks/secret-scan: {min_severity: info}\n",
		"lower without a reason":   "  hooks/secret-scan: {severity: warning}\n",
		"unknown rule":             "  nope/missing: {locked: true}\n",
		"floor above the severity": "  hooks/config-present: {min_severity: error}\n",
	}
	for name, ov := range rejected {
		if _, err := loadLayered(t, layered("", orgOverrides, team(ov))); err == nil {
			t.Errorf("%s: expected a config error", name)
		}
	}
	// Tightening further is allowed, and the team is named as the one who did.
	eff, err := loadLayered(t, layered("", orgOverrides, team("  hooks/secret-scan: {locked: true}\n  hooks/config-present: {severity: error}\n")))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range eff.Rules {
		if r.ID == "hooks/secret-scan" && (!r.Locked || r.PolicyBy != "team-a@1.0.0") {
			t.Errorf("team lock: %+v", r)
		}
	}
	own := "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: x, version: 1.0.0}\nrules:\n  - {id: a/b, title: t, kind: expr, severity: error, expr: 'true', requirement: r, fix: {human: h}}\noverrides:\n  a/b: {locked: true}\n"
	if _, err := loadLayered(t, layered("", own, "")); err == nil || !strings.Contains(err.Error(), "defined in this catalog") {
		t.Errorf("overriding a rule of the same catalog must be refused, got %v", err)
	}
}

// strict and oss are recommended plus overrides: strict raises every
// severity one step and forbids exceptions on the security rules; oss runs
// the public family whatever the visibility.
func TestStrictAndOssPresets(t *testing.T) {
	t.Parallel()
	strict, err := load(t, "version: 1\nextends: [fleetlint:strict]\n")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := load(t, "version: 1\nextends: [fleetlint:recommended]\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(strict.Rules) != len(rec.Rules) {
		t.Fatalf("strict has %d rules, recommended %d: strict only raises, never adds or drops", len(strict.Rules), len(rec.Rules))
	}
	for _, r := range rec.Rules {
		s, ok := ruleByID(strict, r.ID)
		if !ok {
			t.Fatalf("%s missing from strict", r.ID)
		}
		if r.Severity == model.SeverityError && s.Severity != model.SeverityError {
			t.Errorf("%s: error must stay error, got %s", r.ID, s.Severity)
		}
		if r.Severity != model.SeverityError && s.Severity <= r.Severity {
			t.Errorf("%s: %s not raised (strict: %s)", r.ID, r.Severity, s.Severity)
		}
	}
	if s, _ := ruleByID(strict, "hooks/secret-scan"); s.ExceptionsAllowed() {
		t.Error("strict forbids exceptions on secret scanning")
	}
	if _, err := load(t, "version: 1\nextends: [fleetlint:strict]\nexceptions:\n  - {rule: hooks/secret-scan, reason: x}\n"); err == nil || !strings.Contains(err.Error(), "forbids exceptions") {
		t.Errorf("an exception on a strict security rule is refused: %v", err)
	}

	oss, err := load(t, "version: 1\nextends: [fleetlint:oss]\nfacts: {visibility: private}\n")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"public/license": "true", "public/no-ai-attribution": "repo.has_git", "public/security-policy": "repo.tier == 1"} {
		if r, _ := ruleByID(oss, id); r.When != want {
			t.Errorf("%s: when = %q, want %q", id, r.When, want)
		}
	}
}

// when and tiers overrides change applicability, so a locked rule refuses them.
func TestOverridesCannotRescopeLockedRules(t *testing.T) {
	t.Parallel()
	team := `apiVersion: fleetlint.org/v1
kind: Catalog
metadata: {name: team-a, version: 1.0.0}
rules: []
overrides:
  repo/no-tracked-env: {when: "false"}
`
	_, err := loadLayered(t, layered("", orgOverrides, team))
	if err == nil || !strings.Contains(err.Error(), "when and tiers cannot change") {
		t.Errorf("rescoping a locked rule must fail: %v", err)
	}
	bad := strings.Replace(team, `{when: "false"}`, "{tiers: [4]}", 1)
	bad = strings.Replace(bad, "repo/no-tracked-env", "repo/gitignore-present", 1)
	org := strings.Replace(orgOverrides, "  repo/gitignore-present: {enabled: false, reason: \"generated repositories have none\"}\n", "", 1)
	if _, err := loadLayered(t, layered("", org, bad)); err == nil || !strings.Contains(err.Error(), "tiers must be 1, 2 or 3") {
		t.Errorf("tier 4 is refused: %v", err)
	}
	ok := strings.Replace(bad, "[4]", "[1, 2, 3]", 1)
	eff, err := loadLayered(t, layered("", org, ok))
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := ruleByID(eff, "repo/gitignore-present"); len(r.Tiers) != 3 {
		t.Errorf("tiers override applies: %+v", r.Tiers)
	}
}

// A pattern key applies to every matching rule before exact keys refine it;
// raise lifts severities step by step and stops at error.
func TestPatternOverridesAndRaise(t *testing.T) {
	t.Parallel()
	org := `apiVersion: fleetlint.org/v1
kind: Catalog
metadata: {name: acme, version: 1.0.0, includes: ["fleetlint:recommended"]}
rules: []
overrides:
  "release/*": {locked: true}
  "docs/*": {raise: 2}
  "*": {raise: 1}
  docs/adr-directory: {severity: info, reason: "decisions live in the wiki"}
`
	eff, err := loadLayered(t, layered("", org, ""))
	if err != nil {
		t.Fatal(err)
	}
	get := func(id string) model.Rule {
		r, ok := ruleByID(eff, id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		return r
	}
	if r := get("release/sbom"); !r.Locked || r.Severity != model.SeverityError {
		t.Errorf("release/* locked and error stays error: %+v", r)
	}
	if r := get("repo/editorconfig"); r.Severity != model.SeverityError {
		t.Errorf("warning raised once: %s", r.Severity)
	}
	if r := get("docs/architecture"); r.Severity != model.SeverityError {
		t.Errorf("info raised by two patterns is capped at error: %s", r.Severity)
	}
	if r := get("docs/adr-directory"); r.Severity != model.SeverityInfo {
		t.Errorf("an exact key refines the patterns: %s", r.Severity)
	}
	bad := strings.Replace(org, `"*": {raise: 1}`, `"*": {raise: 1, severity: error}`, 1)
	if _, err := loadLayered(t, layered("", bad, "")); err == nil || !strings.Contains(err.Error(), "raise must be positive") {
		t.Errorf("raise with severity is refused: %v", err)
	}
	if _, err := loadLayered(t, layered("", strings.Replace(org, `"docs/*"`, `"nothing/*"`, 1), "")); err != nil {
		t.Errorf("a pattern matching nothing is not an error: %v", err)
	}
}
