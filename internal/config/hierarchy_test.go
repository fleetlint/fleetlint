package config_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/model"
)

// The layer order is fixed however extends is written: a team catalog
// written before the org catalog still resolves after it.
func TestLayerOrderIsFixedWithoutASourcesFile(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"team.yaml":     catHead("team-a", ", layer: team") + "rules:\n" + inlineRule("acme/x", "team", "warning"),
		"org.yaml":      catHead("acme", ", layer: org") + "rules:\n" + inlineRule("acme/x", "org", "error"),
		config.FileName: "version: 1\nextends: [team.yaml, org.yaml, fleetlint:minimal]\n",
	})
	eff := loadDir(t, dir)
	x, _ := ruleByID(eff, "acme/x")
	if x.Title != "team" || x.Layer != catalog.LayerTeam {
		t.Errorf("team resolves after org whatever extends says: %+v", x)
	}
	if eff.Catalogs[0].Ref != "fleetlint:minimal" || eff.Catalogs[1].Layer != catalog.LayerOrg || eff.Catalogs[2].Layer != catalog.LayerTeam {
		t.Errorf("order: %s %s %s", eff.Catalogs[0].Layer, eff.Catalogs[1].Layer, eff.Catalogs[2].Layer)
	}
	if len(eff.Weakened) != 1 || eff.Weakened[0].Layer != catalog.LayerTeam {
		t.Errorf("the team's lowering is recorded: %+v", eff.Weakened)
	}
}

// A sources-file alias and a declared layer must agree; a catalog that
// declares nothing takes the alias's layer, and the repository's otherwise.
func TestDeclaredLayerAndAlias(t *testing.T) {
	t.Parallel()
	mismatch := writeAll(t, map[string]string{
		"org.yaml":      catHead("acme", ", layer: team") + "rules: []\n",
		"s.yaml":        "version: 1\ncatalogs:\n  org: org.yaml\n",
		config.FileName: "version: 1\nsources: s.yaml\nextends: [org]\n",
	})
	if _, err := config.Load(mismatch, config.Options{Loader: catalog.Loader{}}); err == nil || !strings.Contains(err.Error(), "metadata.layer") {
		t.Errorf("alias org with layer team: %v", err)
	}
	plain := writeAll(t, map[string]string{
		"c.yaml":        catHead("c", "") + "rules:\n" + inlineRule("acme/x", "x", "info"),
		config.FileName: "version: 1\nextends: [c.yaml]\n",
	})
	if r, _ := ruleByID(loadDir(t, plain), "acme/x"); r.Layer != catalog.LayerRepo {
		t.Errorf("a catalog that declares no layer is the repository's: %s", r.Layer)
	}
}

// One catalog version per run.
func TestOneCatalogVersionPerRun(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"catalog declares another version": {map[string]string{
			"org.yaml":      catHead("acme", ", layer: org, catalog: {version: v9.9.9}") + "rules: []\n",
			config.FileName: "version: 1\nextends: [org.yaml]\n",
		}, "builds on catalog v9.9.9"},
		"repository and sources file disagree": {map[string]string{
			"s.yaml":        "version: 1\ncatalog: {version: v1.0.0}\ncatalogs:\n  org: fleetlint:minimal\n",
			config.FileName: "version: 1\nsources: s.yaml\ncatalog: {version: v2.0.0}\nextends: [org]\n",
		}, "one catalog version per run"},
	}
	for name, c := range cases {
		dir := writeAll(t, c.files)
		if _, err := config.Load(dir, config.Options{Loader: catalog.Loader{}}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A remote catalog that builds on presets without saying which version is refused;
	// one that names the version the binary ships is fine.
	digest := strings.Repeat("d", 64)
	remote := func(meta string) map[string]string {
		return map[string]string{config.FileName: "version: 1\nextends: [\"oci://registry.example/acme/catalog@sha256:" + digest + "\"]\n", "meta": meta}
	}
	for name, c := range map[string]struct {
		meta string
		ok   bool
	}{
		"undeclared": {catHead("acme", ", includes: [\"fleetlint:minimal\"]"), false},
		"declared":   {catHead("acme", ", includes: [\"fleetlint:minimal\"], catalog: {version: "+catalog.ModuleVersion()+"}"), true},
	} {
		files := remote(c.meta)
		meta := files["meta"]
		delete(files, "meta")
		fetch := func(catalog.OCIRef) (*catalog.OCIArtifact, error) {
			return &catalog.OCIArtifact{Digest: digest, Files: map[string][]byte{"catalog.yaml": []byte(meta + "rules: []\n")}}, nil
		}
		_, err := config.Load(writeAll(t, files), config.Options{Loader: catalog.Loader{FetchOCI: fetch}})
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v", name, err)
		}
		if !c.ok && !strings.Contains(err.Error(), "metadata.catalog") {
			t.Errorf("%s: the error names what to add: %v", name, err)
		}
	}
}

// The sources file's extends is the baseline every repository gets; the
// repository adds to it and cannot leave it out.
func TestSourcesBaselineExtends(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"org.yaml":      catHead("acme", ", layer: org") + "rules:\n" + inlineRule("acme/x", "x", "error"),
		"s.yaml":        "version: 1\nextends: [fleetlint:minimal, org]\ncatalogs:\n  org: org.yaml\n",
		config.FileName: "version: 1\nsources: s.yaml\n",
	})
	eff := loadDir(t, dir)
	if _, ok := ruleByID(eff, "acme/x"); !ok {
		t.Error("the baseline org catalog is loaded without the repository naming it")
	}
	if _, ok := ruleByID(eff, "repo/no-tracked-junk"); !ok {
		t.Error("the baseline preset is loaded")
	}
	bad := writeAll(t, map[string]string{"s.yaml": "version: 1\nextends: [team/x]\ncatalogs:\n  org: fleetlint:minimal\n", config.FileName: "version: 1\nsources: s.yaml\n"})
	if _, err := config.Load(bad, config.Options{Loader: catalog.Loader{}}); err == nil || !strings.Contains(err.Error(), "extends") {
		t.Errorf("a baseline entry must be a preset or a catalog name: %v", err)
	}
}

// A catalog grants exceptions to every repository extending it, attributed
// to the catalog, and cannot grant one a catalog before it forbade.
func TestCatalogExceptions(t *testing.T) {
	t.Parallel()
	dir := writeAll(t, map[string]string{
		"org.yaml":      catHead("acme", ", layer: org, includes: [\"fleetlint:minimal\"]") + "rules: []\nexceptions:\n  - {rule: repo/no-tracked-junk, match: \"fixtures/*.log\", reason: \"parser fixtures\"}\n",
		config.FileName: "version: 1\nextends: [org.yaml]\nexceptions:\n  - {rule: repo/gitignore-present, reason: \"generated\"}\n",
	})
	eff := loadDir(t, dir)
	if len(eff.Exceptions) != 2 {
		t.Fatalf("both exceptions: %+v", eff.Exceptions)
	}
	byRule := map[string]model.Exception{}
	for _, ex := range eff.Exceptions {
		byRule[ex.Rule] = ex
	}
	if ex := byRule["repo/no-tracked-junk"]; ex.Layer != catalog.LayerOrg || ex.Where != "org.yaml" {
		t.Errorf("catalog exception attributed to the catalog: %+v", ex)
	}
	if ex := byRule["repo/gitignore-present"]; ex.Layer != catalog.LayerRepo || ex.Where != config.FileName {
		t.Errorf("repository exception attributed to the repository: %+v", ex)
	}
	forbidden := writeAll(t, map[string]string{
		"org.yaml":      catHead("acme", ", layer: org, includes: [\"fleetlint:minimal\"]") + "rules: []\noverrides:\n  repo/no-tracked-env: {exceptions: false}\n",
		"team.yaml":     catHead("t", ", layer: team") + "rules: []\nexceptions:\n  - {rule: repo/no-tracked-env, reason: \"legacy\"}\n",
		config.FileName: "version: 1\nextends: [org.yaml, team.yaml]\n",
	})
	if _, err := config.Load(forbidden, config.Options{Loader: catalog.Loader{}}); err == nil || !strings.Contains(err.Error(), "forbids exceptions") {
		t.Errorf("a team cannot grant what the org forbade: %v", err)
	}
}

// The repository adjusts and defines; it does not redefine or set policy.
func TestRepositoryGrammar(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		cfg, want string
	}{
		"redefine":  {"version: 1\nextends: [fleetlint:minimal]\nrules:\n  - {id: repo/no-tracked-junk, title: t, kind: expr, severity: info, expr: \"true\", message: m}\n", "does not redefine"},
		"policy":    {"version: 1\nextends: [fleetlint:minimal]\noverrides:\n  repo/no-tracked-junk: {locked: true}\n", "policy a catalog sets"},
		"rescope":   {"version: 1\nextends: [fleetlint:minimal]\noverrides:\n  repo/no-tracked-junk: {when: \"false\"}\n", "when and tiers"},
		"old shape": {"version: 1\nextends: [fleetlint:minimal]\nrules:\n  repo/no-tracked-junk: {enabled: false, reason: r}\n", "sequence"},
	} {
		if _, err := load(t, c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	eff, err := load(t, "version: 1\nextends: [fleetlint:minimal]\nrules:\n  - use: lint/go-config\n  - {id: acme/own, kind: expr, severity: warning, expr: \"true\", message: m}\noverrides:\n  repo/gitignore-present: {severity: error}\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ruleByID(eff, "lint/go-config"); !ok {
		t.Error("the repository may select library rules")
	}
	if r, _ := ruleByID(eff, "acme/own"); r.Title != "acme/own" || r.Requirement != "m" || r.Layer != catalog.LayerRepo {
		t.Errorf("a terse repository rule gets its defaults: %+v", r)
	}
	if r, _ := ruleByID(eff, "repo/gitignore-present"); r.Severity != model.SeverityError {
		t.Error("the repository may raise")
	}
}
