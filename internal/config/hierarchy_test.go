package config_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog/fetch"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/model"
)

// Catalogs apply in the order extends lists them: a team catalog written
// before the org catalog applies first, and the org's definition replaces
// it. A team catalog that includes the org catalog always comes after it.
func TestCatalogsApplyInWrittenOrder(t *testing.T) {
	t.Parallel()
	org := remote("org.yaml", catHead("acme", ", label: org")+"rules:\n"+inlineRule("acme/x", "org", "error"))
	team := remote("team.yaml", catHead("team-a", ", label: team")+"rules:\n"+inlineRule("acme/x", "team", "warning"))
	dir := writeAll(t, map[string]string{
		config.FileName: "version: 1\nextends: [" + team + ", " + org + ", fleetlint:minimal]\n",
	})
	eff := loadDir(t, dir)
	x, _ := ruleByID(eff, "acme/x")
	if x.Title != "org" || x.Label != "org" {
		t.Errorf("written order: the org catalog, listed second, redefines the team's rule: %+v", x)
	}
	if eff.Catalogs[0].Label != "team" || eff.Catalogs[1].Label != "org" || eff.Catalogs[2].Ref != "fleetlint:minimal" {
		t.Errorf("order: %s %s %s", eff.Catalogs[0].Label, eff.Catalogs[1].Label, eff.Catalogs[2].Label)
	}
	if len(eff.Weakened) != 0 {
		t.Errorf("the org raises the severity, nothing is weakened: %+v", eff.Weakened)
	}
	h := eff.Hierarchy()
	if len(h) != 4 || h[0].Order != 1 || h[0].Origin != config.OriginFetched || h[2].Origin != config.OriginPreset || h[3].Origin != config.OriginRepository || h[3].Order != 4 {
		t.Errorf("hierarchy: %+v", h)
	}

	nested := remote("team2.yaml", catHead("team-b", ", label: team, includes: [\""+org+"\"]")+"rules:\n"+inlineRule("acme/x", "team", "warning"))
	dir = writeAll(t, map[string]string{
		config.FileName: "version: 1\nextends: [" + nested + ", " + org + "]\n",
	})
	eff = loadDir(t, dir)
	x, _ = ruleByID(eff, "acme/x")
	if x.Title != "team" || x.Label != "team" {
		t.Errorf("a team that includes the org comes after it whatever extends says: %+v", x)
	}
	if eff.Catalogs[0].Label != "org" || eff.Catalogs[1].Label != "team" || len(eff.Catalogs) != 3 {
		t.Errorf("order: %+v", eff.Hierarchy())
	}
	if len(eff.Weakened) != 1 || eff.Weakened[0].Label != "team" {
		t.Errorf("the team's lowering is recorded under its label: %+v", eff.Weakened)
	}
}

// Written order holds for files of the repository too: a local catalog
// listed first binds what comes after it, so the org catalog listed second
// cannot redefine what the local one locked. Whoever writes extends decides;
// the sources file's baseline is how an organisation goes first everywhere.
func TestWrittenOrderHoldsForLocalFiles(t *testing.T) {
	t.Parallel()
	org := remote("org.yaml", catHead("acme", ", label: org")+"rules:\n"+inlineRule("acme/x", "org", "error"))
	local := catHead("local", ", label: platform") + "rules:\n" + strings.Replace(inlineRule("acme/x", "local", "info"), "severity: info\n", "severity: info\n    locked: true\n", 1)
	dir := writeAll(t, map[string]string{
		"local.yaml":    local,
		config.FileName: "version: 1\nextends: [local.yaml, " + org + "]\n",
	})
	if _, err := config.Load(dir, config.Options{Loader: testLoader()}); err == nil || !strings.Contains(err.Error(), "locked by local@1.0.0") {
		t.Errorf("the local catalog, listed first, locks the rule for the org listed after it: %v", err)
	}
	dir = writeAll(t, map[string]string{
		"local.yaml":    local,
		config.FileName: "version: 1\nextends: [" + org + ", local.yaml]\n",
	})
	eff := loadDir(t, dir)
	x, _ := ruleByID(eff, "acme/x")
	if x.Title != "local" || x.Label != "platform" || eff.Catalogs[0].Label != "org" {
		t.Errorf("listed second, the local catalog redefines the org's unlocked rule under its own label: %+v %+v", x, eff.Hierarchy())
	}
	if len(eff.Weakened) != 1 || eff.Weakened[0].Label != "platform" {
		t.Errorf("its lowering is recorded under its label: %+v", eff.Weakened)
	}
}

// Free labels: a catalog carries what it declares, else the sources-file
// name it is loaded under, else its own name; a local file is always the
// repository's.
func TestLabels(t *testing.T) {
	t.Parallel()
	declared := remote("org.yaml", catHead("acme", ", label: compliance/pci")+"rules: []\n")
	unlabelled := remote("plain.yaml", catHead("plain-catalog", "")+"rules: []\n")
	dir := writeAll(t, map[string]string{
		"s.yaml":        "version: 1\ncatalogs:\n  org: " + declared + "\n  platform/base: " + unlabelled + "\n",
		config.FileName: "version: 1\nsources: s.yaml\nextends: [org, platform/base, " + remote("direct.yaml", catHead("direct", "")+"rules: []\n") + "]\n",
	})
	eff := loadDir(t, dir)
	labels := map[string]string{}
	for _, c := range eff.Catalogs {
		labels[c.Metadata.Name] = c.Label
	}
	want := map[string]string{"acme": "compliance/pci", "plain-catalog": "platform/base", "direct": "direct", config.FileName: catalog.LabelRepo}
	for name, label := range want {
		if labels[name] != label {
			t.Errorf("%s: label %q, want %q", name, labels[name], label)
		}
	}
	if err := eff.Requires([]string{"org", "platform/base", "acme", "direct"}); err != nil {
		t.Errorf("--require takes aliases and names: %v", err)
	}
	plain := writeAll(t, map[string]string{
		"c.yaml":        catHead("c", "") + "rules:\n" + inlineRule("acme/x", "x", "info"),
		"d.yaml":        catHead("d", ", layer: security-team") + "rules:\n" + inlineRule("acme/y", "y", "info"),
		config.FileName: "version: 1\nextends: [c.yaml, d.yaml]\n",
	})
	eff = loadDir(t, plain)
	if r, _ := ruleByID(eff, "acme/x"); r.Label != "c" {
		t.Errorf("an unlabelled local catalog is labelled by its name: %s", r.Label)
	}
	if r, _ := ruleByID(eff, "acme/y"); r.Label != "security-team" || len(eff.Deprecations) != 1 || !strings.Contains(eff.Deprecations[0], "metadata.layer") {
		t.Errorf("the 0.5 spelling is read and deprecated: %s %v", r.Label, eff.Deprecations)
	}

	bad := writeAll(t, map[string]string{
		config.FileName: "version: 1\nextends: [" + remote("bad.yaml", catHead("bad", ", label: \" org \"")+"rules: []\n") + "]\n",
	})
	if _, err := config.Load(bad, config.Options{Loader: testLoader()}); err == nil || !strings.Contains(err.Error(), "metadata.label") {
		t.Errorf("a label with surrounding spaces is refused: %v", err)
	}
}

// One version of a named catalog per run: the sources file pins one and a
// team catalog includes another.
func TestOneVersionPerCatalogName(t *testing.T) {
	t.Parallel()
	v1 := remote("org-v1.yaml", catHead("acme", ", label: org")+"rules: []\n")
	v2 := remote("org-v2.yaml", strings.Replace(catHead("acme", ", label: org"), "version: 1.0.0", "version: 2.0.0", 1)+"rules: []\n")
	team := remote("team.yaml", catHead("team-a", ", includes: [\""+v1+"\"]")+"rules: []\n")
	dir := writeAll(t, map[string]string{
		config.FileName: "version: 1\nextends: [" + v2 + ", " + team + "]\n",
	})
	_, err := config.Load(dir, config.Options{Loader: testLoader()})
	if err == nil || !strings.Contains(err.Error(), "one version of a catalog per run") {
		t.Errorf("two versions of acme: %v", err)
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
			"org.yaml":      catHead("acme", ", label: org, catalog: {version: v9.9.9}") + "rules: []\n",
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
		fetch := func(fetch.OCIRef) (*fetch.OCIArtifact, error) {
			return &fetch.OCIArtifact{Digest: digest, Files: map[string][]byte{"catalog.yaml": []byte(meta + "rules: []\n")}}, nil
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
		"org.yaml":      catHead("acme", ", label: org") + "rules:\n" + inlineRule("acme/x", "x", "error"),
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
	org := remote("org.yaml", catHead("acme", ", label: org, includes: [\"fleetlint:minimal\"]")+"rules: []\nexceptions:\n  - {rule: repo/no-tracked-junk, match: \"fixtures/*.log\", reason: \"parser fixtures\"}\n")
	dir := writeAll(t, map[string]string{
		config.FileName: "version: 1\nextends: [" + org + "]\nexceptions:\n  - {rule: repo/gitignore-present, reason: \"generated\"}\n",
	})
	eff := loadDir(t, dir)
	if len(eff.Exceptions) != 2 {
		t.Fatalf("both exceptions: %+v", eff.Exceptions)
	}
	byRule := map[string]model.Exception{}
	for _, ex := range eff.Exceptions {
		byRule[ex.Rule] = ex
	}
	if ex := byRule["repo/no-tracked-junk"]; ex.Label != "org" || ex.Where != org {
		t.Errorf("catalog exception attributed to the catalog: %+v", ex)
	}
	if ex := byRule["repo/gitignore-present"]; ex.Label != catalog.LabelRepo || ex.Where != config.FileName {
		t.Errorf("repository exception attributed to the repository: %+v", ex)
	}
	forbidden := writeAll(t, map[string]string{
		"org.yaml":      catHead("acme", ", label: org, includes: [\"fleetlint:minimal\"]") + "rules: []\noverrides:\n  repo/no-tracked-env: {exceptions: false}\n",
		"team.yaml":     catHead("t", ", label: team") + "rules: []\nexceptions:\n  - {rule: repo/no-tracked-env, reason: \"legacy\"}\n",
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
	eff, err := load(t, "version: 1\nextends: [fleetlint:minimal]\nrules:\n  - use: lint/configured/go\n  - {id: acme/own, kind: expr, severity: warning, expr: \"true\", message: m}\noverrides:\n  repo/gitignore-present: {severity: error}\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ruleByID(eff, "lint/configured/go"); !ok {
		t.Error("the repository may select library rules")
	}
	if r, _ := ruleByID(eff, "acme/own"); r.Title != "acme/own" || r.Requirement != "m" || r.Label != catalog.LabelRepo {
		t.Errorf("a terse repository rule gets its defaults: %+v", r)
	}
	if r, _ := ruleByID(eff, "repo/gitignore-present"); r.Severity != model.SeverityError {
		t.Error("the repository may raise")
	}
}
