package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/model"
)

// orgCatalog is an organization baseline: one locked rule, one rule with a
// severity floor, one rule that forbids exceptions.
const orgCatalog = `apiVersion: fleetlint.org/v1
kind: Catalog
metadata: {name: acme-baseline, version: 1.0.0}
rules:
  - id: org/locked
    title: locked
    kind: expr
    severity: error
    locked: true
    expr: file("LOCKED")
    requirement: r
    fix: {human: h}
  - id: org/floored
    title: floored
    kind: expr
    severity: error
    min_severity: warning
    expr: file("FLOORED")
    requirement: r
    fix: {human: h}
  - id: org/no-exceptions
    title: no exceptions
    kind: expr
    severity: error
    locked: true
    exceptions: false
    expr: file("NOEXC")
    requirement: r
    fix: {human: h}
`

// loadWithOrg writes the repo config in a temp dir, serving orgCatalog for
// any https reference. The repo config refers to it as org.yaml#sha256-<digest>.
func loadWithOrg(t *testing.T, repoCfg string) (*config.Effective, error) {
	t.Helper()
	dir := t.TempDir()
	ref := "https://acme.example/org.yaml#sha256-" + catalog.Digest([]byte(orgCatalog))
	body := strings.ReplaceAll(repoCfg, "$ORG", ref)
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.Load(dir, config.Options{Loader: catalog.Loader{Fetch: func(string) ([]byte, error) { return []byte(orgCatalog), nil }}})
}

func TestPolicyLocksAndFloors(t *testing.T) {
	t.Parallel()
	rejected := map[string]string{
		"disable locked":          "version: 1\nextends: [$ORG]\nrules:\n  org/locked: {enabled: false, reason: r}\n",
		"lower locked":            "version: 1\nextends: [$ORG]\nrules:\n  org/locked: {severity: warning, reason: r}\n",
		"lower below floor":       "version: 1\nextends: [$ORG]\nrules:\n  org/floored: {severity: info, reason: r}\n",
		"exception on forbidden":  "version: 1\nextends: [$ORG]\nexceptions:\n  - {rule: org/no-exceptions, reason: r}\n",
		"disable locked in scope": "version: 1\nextends: [$ORG]\nscopes:\n  - path: api\n    rules:\n      org/locked: {enabled: false, reason: r}\n",
		"repo cannot set lock":    "version: 1\nextends: [$ORG]\nrules:\n  org/floored: {locked: true}\n",
		"redefine locked inline":  "version: 1\nextends: [$ORG]\nrules:\n  org/locked: {kind: expr, expr: 'true', message: m, fix: {human: h}}\n",
		"params on locked":        "version: 1\nextends: [$ORG]\nrules:\n  org/locked: {params: {x: 1}}\n",
	}
	for name, cfg := range rejected {
		if _, err := loadWithOrg(t, cfg); err == nil {
			t.Errorf("%s: expected a config error", name)
		}
	}
	allowed := "version: 1\nextends: [$ORG]\nrules:\n  org/floored: {severity: warning, reason: \"team decision\"}\n  org/locked: {severity: error}\nexceptions:\n  - {rule: org/locked, reason: \"legacy service\", until: 2027-01-01}\n"
	eff, err := loadWithOrg(t, allowed)
	if err != nil {
		t.Fatalf("lowering to the floor, raising a locked rule and reasoned exceptions on it are allowed: %v", err)
	}
	if r, _ := ruleByID(eff, "org/floored"); r.Severity != model.SeverityWarning || len(eff.Weakened) != 1 {
		t.Fatalf("floor-respecting weakening must apply and be recorded: %+v %+v", r.Severity, eff.Weakened)
	}
	if r, _ := ruleByID(eff, "org/locked"); !r.Locked || r.Severity != model.SeverityError {
		t.Fatalf("locked rule keeps its lock and may be raised: %+v", r)
	}
	// Disabling a floored (but not locked) rule with a reason is allowed.
	if _, err := loadWithOrg(t, "version: 1\nextends: [$ORG]\nrules:\n  org/floored: {enabled: false, reason: r}\n"); err != nil {
		t.Fatalf("min_severity alone does not forbid disabling: %v", err)
	}
}

func TestLockedRuleCannotBeRedefinedByLaterCatalog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	other := "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: other, version: 1}\nrules:\n  - {id: org/locked, title: t, kind: expr, severity: info, expr: 'true', requirement: r, fix: {human: h}}\n"
	orgRef := "https://acme.example/org.yaml#sha256-" + catalog.Digest([]byte(orgCatalog))
	otherRef := "https://evil.example/other.yaml#sha256-" + catalog.Digest([]byte(other))
	cfg := "version: 1\nextends: [" + orgRef + ", " + otherRef + "]\n"
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	fetch := func(url string) ([]byte, error) {
		if strings.Contains(url, "evil") {
			return []byte(other), nil
		}
		return []byte(orgCatalog), nil
	}
	_, err := config.Load(dir, config.Options{Loader: catalog.Loader{Fetch: fetch}})
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("a later catalog must not redefine a locked rule: %v", err)
	}
}

func TestRequires(t *testing.T) {
	t.Parallel()
	eff, err := loadWithOrg(t, "version: 1\nextends: [fleetlint:minimal, $ORG]\n")
	if err != nil {
		t.Fatal(err)
	}
	digest := catalog.Digest([]byte(orgCatalog))
	for _, ok := range []string{"acme-baseline", "whatever#sha256-" + digest, "fleetlint:minimal", "fleetlint-minimal"} {
		if err := eff.Requires([]string{ok}); err != nil {
			t.Errorf("%s should be satisfied: %v", ok, err)
		}
	}
	for _, missing := range []string{"other-baseline", "x#sha256-" + strings.Repeat("0", 64), "fleetlint:recommended"} {
		if err := eff.Requires([]string{missing}); err == nil {
			t.Errorf("%s should be missing", missing)
		}
	}
	if err := eff.Requires(nil); err != nil {
		t.Error("no requirements is fine")
	}
}

func TestCatalogSeverityBelowOwnFloorIsRejected(t *testing.T) {
	t.Parallel()
	bad := "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules:\n  - {id: a/b, title: t, kind: expr, severity: info, min_severity: error, expr: 'true', requirement: r, fix: {human: h}}\n"
	if _, err := catalog.Parse([]byte(bad)); err == nil {
		t.Fatal("severity below its own min_severity must be rejected")
	}
	typo := strings.ReplaceAll(bad, "min_severity: error", "min_severity: severe")
	if _, err := catalog.Parse([]byte(typo)); err == nil {
		t.Fatal("unknown min_severity must be rejected")
	}
}
