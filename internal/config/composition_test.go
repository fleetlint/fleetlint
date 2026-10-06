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

// The composition rules, one case each. See docs: organisations, "What
// applies when".
func catHead(name, extra string) string {
	return "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: " + name + ", version: 1.0.0, engine: 2" + extra + "}\n"
}

func inlineRule(id, title, severity string) string {
	return "  - id: " + id + "\n    title: " + title + "\n    kind: expr\n    severity: " + severity + "\n    expr: \"true\"\n    message: m\n    requirement: r\n    rationale: r\n    fix: {human: h, agent: a}\n"
}

func writeAll(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func loadDir(t *testing.T, dir string) *config.Effective {
	t.Helper()
	eff, err := config.Load(dir, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	return eff
}

func TestIncludesResolveInOrderLaterWins(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"a.yaml":        catHead("a", "") + "rules:\n" + inlineRule("acme/x", "from a", "warning") + inlineRule("acme/only-a", "only a", "info"),
		"b.yaml":        catHead("b", "") + "rules:\n" + inlineRule("acme/x", "from b", "warning"),
		"c.yaml":        catHead("c", ", includes: [a.yaml, b.yaml]") + "rules: []\n",
		config.FileName: "version: 1\nextends: [c.yaml]\n",
	})
	eff := loadDir(t, dir)
	x, _ := ruleByID(eff, "acme/x")
	if x.Title != "from b" || x.Source != "b@1.0.0" {
		t.Errorf("the later include redefines: %+v", x)
	}
	if _, ok := ruleByID(eff, "acme/only-a"); !ok {
		t.Error("rules the later include does not touch stay")
	}
	if len(eff.Weakened) != 0 {
		t.Errorf("same severity: nothing weakened: %+v", eff.Weakened)
	}
}

func TestInlineBeatsUseWithinACatalog(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"c.yaml":        catHead("c", "") + "rules:\n  - use: lint/go-*\n" + inlineRule("lint/go-config", "our own go config rule", "error"),
		config.FileName: "version: 1\nextends: [c.yaml]\n",
	})
	eff := loadDir(t, dir)
	r, _ := ruleByID(eff, "lint/go-config")
	if r.Title != "our own go config rule" || r.Severity != model.SeverityError {
		t.Errorf("the inline definition replaces the selected one: %+v", r)
	}
	if _, ok := ruleByID(eff, "lint/go-linters-enabled"); !ok {
		t.Error("the rest of the glob is still selected")
	}
}

func TestSelectedRuleKeepsItsFirstCatalog(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"org.yaml":      catHead("org", "") + "rules:\n  - use: lint/go-config\n  - use: slop/focused-tests\n",
		config.FileName: "version: 1\nextends: [fleetlint:recommended, org.yaml]\n",
	})
	eff := loadDir(t, dir)
	r, _ := ruleByID(eff, "lint/go-config")
	if r.Source != "fleetlint-recommended@0.1.0" || r.Layer != catalog.LayerPreset {
		t.Errorf("a rule already loaded is not re-attributed by a later use: source=%s layer=%s", r.Source, r.Layer)
	}
	n := 0
	for _, x := range eff.Rules {
		if x.ID == "lint/go-config" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("loaded %d times", n)
	}
}

func TestInlineRedefinitionAcrossLayersIsRecorded(t *testing.T) {
	t.Parallel()
	lower := writeAll(t, map[string]string{
		"org.yaml":      catHead("org", "") + "rules:\n" + inlineRule("repo/editorconfig", "looser", "info"),
		config.FileName: "version: 1\nextends: [fleetlint:recommended, org.yaml]\n",
	})
	eff := loadDir(t, lower)
	r, _ := ruleByID(eff, "repo/editorconfig")
	if r.Title != "looser" || r.Severity != model.SeverityInfo {
		t.Fatalf("redefinition applies: %+v", r)
	}
	if len(eff.Weakened) != 1 || eff.Weakened[0].ID != "repo/editorconfig" || eff.Weakened[0].Layer != catalog.LayerRepo {
		t.Errorf("lowering across layers is recorded: %+v", eff.Weakened)
	}
	locked := writeAll(t, map[string]string{
		"org.yaml":      catHead("org", ", includes: [\"fleetlint:minimal\"]") + "rules: []\noverrides:\n  repo/no-tracked-env: {locked: true}\n",
		"team.yaml":     catHead("team", "") + "rules:\n" + inlineRule("repo/no-tracked-env", "redefined", "error"),
		config.FileName: "version: 1\nextends: [org.yaml, team.yaml]\n",
	})
	if _, err := config.Load(locked, config.Options{Loader: catalog.Loader{}}); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Errorf("a locked rule cannot be redefined: %v", err)
	}
}

func TestSelectAndTightenInOneCatalog(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"org.yaml":      catHead("org", "") + "rules:\n  - use: lint/*\noverrides:\n  lint/go-config: {severity: error, locked: true}\n  \"lint/python-*\": {raise: 1}\n",
		config.FileName: "version: 1\nextends: [org.yaml]\n",
	})
	eff := loadDir(t, dir)
	g, _ := ruleByID(eff, "lint/go-config")
	p, _ := ruleByID(eff, "lint/python-config")
	if g.Severity != model.SeverityError || !g.Locked || p.Severity != model.SeverityError {
		t.Errorf("overrides adjust the catalog's own selections: %+v / %+v", g, p)
	}
	if g.Source != "org@1.0.0" {
		t.Errorf("a selected rule is attributed to the selecting catalog: %s", g.Source)
	}
}

func TestParallelRuleAndDisabledOriginal(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"org.yaml": catHead("org", ", includes: [\"fleetlint:recommended\"]") + "rules:\n" + inlineRule("acme/go-config", "our stricter variant", "error") +
			"overrides:\n  lint/go-config: {enabled: false, reason: \"replaced by acme/go-config\"}\n",
		config.FileName: "version: 1\nextends: [org.yaml]\n",
	})
	eff := loadDir(t, dir)
	if _, ok := ruleByID(eff, "acme/go-config"); !ok {
		t.Error("the parallel rule exists")
	}
	if _, ok := ruleByID(eff, "lint/go-config"); ok {
		t.Error("the original is disabled")
	}
	if len(eff.Disabled) != 1 || eff.Disabled[0].ID != "lint/go-config" || eff.Disabled[0].Layer != catalog.LayerRepo {
		t.Errorf("the disable is recorded with its reason and layer: %+v", eff.Disabled)
	}
}

// A catalog that ships rules/ selects from it first; a shared id in its own
// library replaces the source's definition for that catalog.
func TestShippedLibraryIsSearchedFirst(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("c", 64)
	own := strings.Replace(inlineRule("lint/go-config", "acme's go config", "error"), "  - id:", "id:", 1)
	own = strings.ReplaceAll(own, "\n    ", "\n")
	ownerRule := strings.ReplaceAll(strings.Replace(inlineRule("acme/owner-file", "owner", "error"), "  - id:", "id:", 1), "\n    ", "\n")
	fetch := func(catalog.OCIRef) (*catalog.OCIArtifact, error) {
		return &catalog.OCIArtifact{Digest: digest, Files: map[string][]byte{
			"catalog.yaml":               []byte(catHead("acme", "") + "rules:\n  - use: acme/*\n  - use: lint/go-config\n  - use: repo/readme-present\n"),
			"rules/acme/owner-file.yaml": []byte(ownerRule),
			"rules/lint/go-config.yaml":  []byte(own),
			"templates/SECURITY.md":      []byte("acme\n"),
		}}, nil
	}
	dir := writeAll(t, map[string]string{config.FileName: "version: 1\nextends: [\"oci://registry.example/acme/catalog@sha256:" + digest + "\"]\n"})
	eff, err := config.Load(dir, config.Options{Loader: catalog.Loader{FetchOCI: fetch}})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := ruleByID(eff, "acme/owner-file"); !ok || r.Title != "owner" {
		t.Errorf("own library rule: %+v %v", r, ok)
	}
	if r, _ := ruleByID(eff, "lint/go-config"); r.Title != "acme's go config" {
		t.Errorf("own library wins over the source's for the same id: %q", r.Title)
	}
	if r, ok := ruleByID(eff, "repo/readme-present"); !ok || r.Source != "acme@1.0.0" {
		t.Errorf("the source library still serves the rest: %+v", r)
	}
}
