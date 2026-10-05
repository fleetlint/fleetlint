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
