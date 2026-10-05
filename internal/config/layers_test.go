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

const layerRule = `  - id: %ID
    title: t
    kind: expr
    severity: %SEV
    expr: file("X")
    requirement: r
    fix: {human: h}
`

func layerCatalog(name string, rules ...string) string {
	b := "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: " + name + ", version: 1.0.0}\nrules:\n"
	for i := 0; i+1 < len(rules); i += 2 {
		b += strings.NewReplacer("%ID", rules[i], "%SEV", rules[i+1]).Replace(layerRule)
	}
	return b
}

// loadLayered writes the files into a temp repository and loads its config.
func loadLayered(t *testing.T, files map[string]string) (*config.Effective, error) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return config.Load(dir, config.Options{Loader: catalog.Loader{}})
}

func TestSourcesResolveInLayerOrder(t *testing.T) {
	t.Parallel()
	// team is written before org; the fixed order must still let team override org.
	eff, err := loadLayered(t, map[string]string{
		config.FileName:               "version: 1\nsources: standards/sources.yaml\nextends: [team/mobile, org, fleetlint:minimal]\nrules:\n  repo/own: {kind: expr, severity: info, expr: 'true', message: m, fix: {human: h}}\n  org/kept: {severity: warning, reason: local tooling}\n",
		"standards/sources.yaml":      "version: 1\ncatalogs:\n  org: org.yaml\n  team/mobile: teams/mobile.yaml\n",
		"standards/org.yaml":          layerCatalog("acme", "org/kept", "error", "org/lowered", "error"),
		"standards/teams/mobile.yaml": layerCatalog("mobile", "org/lowered", "warning", "team/own", "error"),
	})
	if err != nil {
		t.Fatal(err)
	}
	layers := map[string]string{}
	sev := map[string]model.Severity{}
	for _, r := range eff.Rules {
		layers[r.ID], sev[r.ID] = r.Layer, r.Severity
	}
	want := map[string]string{"org/kept": "org", "org/lowered": "team", "team/own": "team", "repo/own": "repo", "repo/gitignore-present": "preset"}
	for id, layer := range want {
		if layers[id] != layer {
			t.Errorf("rule %s: layer %q, want %q", id, layers[id], layer)
		}
	}
	if sev["org/lowered"] != model.SeverityWarning {
		t.Errorf("the team catalog must override the org rule, got %s", sev["org/lowered"])
	}
	byLayer := map[string]string{}
	for _, w := range eff.Weakened {
		byLayer[w.Layer] = w.ID
	}
	if byLayer["team"] != "org/lowered" || byLayer["repo"] != "org/kept" {
		t.Errorf("weakenings must name their layer, got %+v", eff.Weakened)
	}
	if err := eff.Requires([]string{"org", "team/mobile", "acme"}); err != nil {
		t.Errorf("--require must accept aliases and names: %v", err)
	}
}

func TestSourcesRejected(t *testing.T) {
	t.Parallel()
	org := layerCatalog("acme", "org/a", "error")
	cases := map[string]map[string]string{
		"unknown team alias": {
			config.FileName: "version: 1\nsources: s.yaml\nextends: [org, team/web]\n",
			"s.yaml":        "version: 1\ncatalogs:\n  org: org.yaml\n", "org.yaml": org,
		},
		"alias that is not org or team": {
			config.FileName: "version: 1\nsources: s.yaml\nextends: [platform]\n",
			"s.yaml":        "version: 1\ncatalogs:\n  platform: org.yaml\n", "org.yaml": org,
		},
		"unknown key in sources": {
			config.FileName: "version: 1\nsources: s.yaml\nextends: [org]\n",
			"s.yaml":        "version: 1\nsigners: []\ncatalogs:\n  org: org.yaml\n", "org.yaml": org,
		},
		"missing sources file": {
			config.FileName: "version: 1\nsources: s.yaml\nextends: [org]\n",
		},
		"plain http target": {
			config.FileName: "version: 1\nsources: s.yaml\nextends: [org]\n",
			"s.yaml":        "version: 1\ncatalogs:\n  org: http://example.com/org.yaml\n",
		},
	}
	for name, files := range cases {
		if _, err := loadLayered(t, files); err == nil {
			t.Errorf("%s: expected a config error", name)
		}
	}
}

func TestRemoteSourcesCannotNameLocalPaths(t *testing.T) {
	t.Parallel()
	body := []byte("version: 1\ncatalogs:\n  org: ../org.yaml\n")
	l := catalog.Loader{Fetch: func(string) ([]byte, error) { return body, nil }}
	if _, err := l.LoadSources("https://acme.example/sources.yaml#sha256-" + catalog.Digest(body)); err == nil {
		t.Fatal("a remote sources file must not point at local paths")
	}
	if _, err := l.LoadSources("https://acme.example/sources.yaml"); err == nil {
		t.Fatal("a remote sources file must be pinned")
	}
}

func TestFloorHoldsAcrossCatalogs(t *testing.T) {
	t.Parallel()
	org := strings.Replace(layerCatalog("acme", "org/floored", "error"), "severity: error\n", "severity: error\n    min_severity: warning\n", 1)
	files := func(teamSeverity, repoRules string) map[string]string {
		return map[string]string{
			config.FileName: "version: 1\nsources: s.yaml\nextends: [org, team/a]\n" + repoRules,
			"s.yaml":        "version: 1\ncatalogs:\n  org: org.yaml\n  team/a: team.yaml\n",
			"org.yaml":      org, "team.yaml": layerCatalog("a", "org/floored", teamSeverity),
		}
	}
	if _, err := loadLayered(t, files("info", "")); err == nil || !strings.Contains(err.Error(), "floor") {
		t.Fatalf("a team catalog below the org floor must be rejected, got %v", err)
	}
	if _, err := loadLayered(t, files("warning", "")); err != nil {
		t.Fatalf("a team catalog at the floor is allowed: %v", err)
	}
	// The floor survives the team's redefinition: the repository still cannot go below it.
	if _, err := loadLayered(t, files("warning", "rules:\n  org/floored: {severity: info, reason: r}\n")); err == nil {
		t.Fatal("the org floor must still bind the repository after a team redefinition")
	}
}
