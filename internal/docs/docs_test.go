package docs_test

import (
	"strings"
	"testing"

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
