package docs_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fleetlint/fleetlint/internal/docs"
)

// The generated pages are published as HTML: placeholders in angle brackets
// must survive as text, and every page names the catalog it describes.
func TestGeneratedPagesAreSafeMarkdown(t *testing.T) {
	t.Parallel()
	pages, err := docs.Generate(docs.Options{IndexFile: "_index.md"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range pages {
		seen[p.Path] = true
		text := string(p.Content)
		if !strings.Contains(text, "from catalog ") || !strings.HasSuffix(text, ".\n") {
			t.Errorf("%s does not end with the catalog version", p.Path)
		}
		inFence := false
		for i, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "```") {
				inFence = !inFence
			}
			if inFence || strings.HasPrefix(line, "See: ") {
				continue
			}
			for j, part := range strings.Split(line, "`") {
				if j%2 == 0 && strings.Contains(part, "<") {
					t.Errorf("%s:%d: a bare angle bracket would be dropped as HTML: %s", p.Path, i+1, line)
				}
			}
		}
	}
	if !seen["rules/_index.md"] || !seen["cel-reference.md"] || !seen["rules/repo--no-tracked-junk.md"] {
		t.Errorf("expected the index, the CEL reference and a page per rule")
	}
}

func generated(t *testing.T) map[string]string {
	t.Helper()
	pages, err := docs.Generate(docs.Options{IndexFile: "_index.md"})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, p := range pages {
		byPath[p.Path] = string(p.Content)
	}
	return byPath
}

// combine left the catalog format at engine level 7: no page explains it.
func TestCapabilityPagesHaveNoCombine(t *testing.T) {
	t.Parallel()
	for path, text := range generated(t) {
		if strings.Contains(text, "Combine") || strings.Contains(text, "combine:") {
			t.Errorf("%s mentions combine", path)
		}
	}
}

// An implementation that declares its capability does not apply says why,
// and nothing about a check or a fix.
func TestNotApplicablePage(t *testing.T) {
	t.Parallel()
	page := generated(t)["rules/types--strict--go.md"]
	for _, want := range []string{"[types/strict](types--strict.md) does not apply to go", "## Why it does not apply", "no separate type checker", "https://fleetlint.org/stacks/#go"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q:\n%s", want, page)
		}
	}
	for _, unwanted := range []string{"Severity", "## Check", "## Fix", "## Applies when", "strict mode"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("has %q:\n%s", unwanted, page)
		}
	}
}

// A computed locate is never raw CEL inside a sentence.
func TestLocateIsProse(t *testing.T) {
	t.Parallel()
	pages := generated(t)
	for path, text := range pages {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "A finding") && strings.ContainsAny(line, "()[]?") {
				t.Errorf("%s: %s", path, line)
			}
		}
	}
	if page := pages["rules/hooks--secret-scan.md"]; !strings.Contains(page, "points at the first existing file of `.pre-commit-config.yaml`, `lefthook.yml`") {
		t.Errorf("hooks/secret-scan:\n%s", page)
	}
}

// An implementation without a fix of its own links its capability's
// instead of presenting the capability's text as its own.
func TestInheritedFixLinksCapability(t *testing.T) {
	t.Parallel()
	pages := generated(t)
	impl, capability := pages["rules/quality--duplication-gate--any.md"], pages["rules/quality--duplication-gate.md"]
	fix := capability[strings.Index(capability, "## Fix\n\n")+8:]
	fix = fix[:strings.Index(fix, "\n")]
	if !strings.Contains(impl, "no fix of its own: see [quality/duplication-gate](quality--duplication-gate.md#fix)") || strings.Contains(impl, fix) {
		t.Errorf("implementation repeats the capability's fix %q:\n%s", fix, impl)
	}
	inherited := 0
	for _, text := range pages {
		inherited += strings.Count(text, "Agent instruction (from [")
	}
	if inherited == 0 {
		t.Error("no implementation marks an agent instruction it inherits")
	}
}

// The CEL reference lists the extension functions the environment enables.
func TestCELReferenceListsExtensions(t *testing.T) {
	t.Parallel()
	page := generated(t)["cel-reference.md"]
	for _, want := range []string{"## Extension functions", "`.split()`", "`.lowerAscii()`", "`.flatten()`", "`lists.range()`", "https://pkg.go.dev/cel.dev/cel-go"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// The tier section lists every capability and standalone rule once, in the
// row of the tiers it runs at; implementations follow their capability.
func TestRuleIndexTiers(t *testing.T) {
	t.Parallel()
	index := generated(t)["rules/_index.md"]
	section := index[strings.Index(index, "## What runs at each tier"):]
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if cells := strings.Split(line, " | "); len(cells) == 3 && strings.HasPrefix(line, "| ") {
			rows[strings.TrimPrefix(cells[0], "| ")] = cells[1]
		}
	}
	for tiers, id := range map[string]string{"1, 2, 3": "repo/no-tracked-junk", "1, 2": "hooks/secret-scan", "1": "release/checksums"} {
		if !strings.Contains(rows[tiers], "["+id+"]") {
			t.Errorf("tiers %s does not list %s: %q", tiers, id, rows[tiers])
		}
	}
	if strings.Contains(section, "[types/strict/go]") || strings.Count(section, "[types/strict]") != 1 {
		t.Error("a capability is listed once, its implementations not at all")
	}
}

// Generated pages are told apart from written ones by their last line; only
// generated pages in the directories the generator writes are orphans.
func TestOrphans(t *testing.T) {
	t.Parallel()
	gen := func(body string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(body + "\n\n---\n\nGenerated by `fleetlint docs` from catalog v0.1.0.\n")}
	}
	fsys := fstest.MapFS{
		"cel-reference.md":            gen("# CEL"),
		"old-reference.md":            gen("# gone"),
		"getting-started.md":          {Data: []byte("# Hand written\n\nGenerated by `fleetlint docs` from catalog v1 is quoted here.\n\nmore\n")},
		"rules/kept.md":               gen("# kept"),
		"rules/removed--rule.md":      gen("# removed"),
		"rules/notes.md":              {Data: []byte("# notes\n")},
		"rules/image.png":             gen("not markdown"),
		"versions/v0.1/rules/old.md":  gen("# a released snapshot"),
		"versions/v0.1/cel-reference": gen("# snapshot"),
	}
	pages := []docs.Page{{Path: "cel-reference.md"}, {Path: "rules/kept.md"}, {Path: "other/new.md"}}
	got, err := docs.Orphans(fsys, pages)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"old-reference.md", "rules/removed--rule.md"}; !slices.Equal(got, want) {
		t.Errorf("orphans %v, want %v", got, want)
	}
	if !docs.IsGenerated([]byte("x\n---\n\nGenerated by `fleetlint docs` from catalog v0.7.0.\n")) || docs.IsGenerated([]byte("Generated by `fleetlint docs` from catalog v0.7.0.\nmore\n")) {
		t.Error("IsGenerated reads the last line")
	}
}
