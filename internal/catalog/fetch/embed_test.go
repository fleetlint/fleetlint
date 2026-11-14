package fetch

import (
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/testutil"
)

// A pin reads what the catalog module embeds, no less: a top-level path the
// catalog adds must be added to pinnedPaths, or pinned runs miss it.
func TestPinnedPathsAreWhatTheCatalogEmbeds(t *testing.T) {
	t.Parallel()
	var embedded []string
	for _, p := range testutil.BuiltinCatalog(t).EmbedPatterns {
		top, _, _ := strings.Cut(strings.TrimPrefix(p, "all:"), "/")
		embedded = append(embedded, top)
	}
	slices.Sort(embedded)
	embedded = slices.Compact(embedded)
	pinned := slices.Sorted(slices.Values(pinnedPaths))
	if !slices.Equal(embedded, pinned) {
		t.Errorf("the catalog module embeds %v, a pin reads %v", embedded, pinned)
	}
}
