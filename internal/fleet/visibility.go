package fleet

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/forge"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// ForgeLookup asks the forge for each repository whose visibility is
// unknown and keeps the failures for one note per host instead of one per
// repository. Its Lookup method is an Options.Visibility.
type ForgeLookup struct {
	Client  forge.Client
	mu      sync.Mutex     // fleet checks repositories in parallel
	hidden  map[string]int // host -> repositories it did not show
	unnamed map[string]int // Gitea host -> repositories not asked, the user did not name it
	failed  []string
}

// Lookup returns the visibility the forge reports, or "" when it is not
// known locally and the forge does not say.
func (v *ForgeLookup) Lookup(ctx context.Context, r *repo.Repo, f facts.Facts) facts.Visibility {
	if f.Visibility != facts.VisibilityUnknown {
		return ""
	}
	public, asked, host, err := v.Client.RemoteVisibility(ctx, f.Forge, r.RemoteURL()) //nolint:contextcheck // git lookups run under the repository's own bounded context
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case !asked:
		return ""
	case errors.Is(err, forge.ErrUnknownHost):
		v.unnamed = increment(v.unnamed, host)
	case errors.Is(err, forge.ErrNotFound):
		v.hidden = increment(v.hidden, host)
	case err != nil:
		v.failed = append(v.failed, fmt.Sprintf("%s (%v)", r.Name(), err))
	case public:
		return facts.VisibilityPublic
	default:
		return facts.VisibilityPrivate
	}
	return ""
}

func increment(m map[string]int, k string) map[string]int {
	if m == nil {
		m = map[string]int{}
	}
	m[k]++
	return m
}

// Notes explains, per host, why visibilities stayed unknown.
func (v *ForgeLookup) Notes() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for _, host := range slices.Sorted(maps.Keys(v.hidden)) {
		hint := "GITEA_TOKEN"
		if host == "github.com" {
			hint = "GITHUB_TOKEN, e.g. GITHUB_TOKEN=$(gh auth token)"
		}
		out = append(out, fmt.Sprintf("visibility: %s does not show %s without a token; left unknown (set %s, or visibility in the fleet file)", host, Plural(v.hidden[host], "repository", "repositories"), hint))
	}
	for _, host := range slices.Sorted(maps.Keys(v.unnamed)) {
		out = append(out, fmt.Sprintf("visibility: %s was not asked about %s; left unknown (set GITEA_HOST=%s to let fleetlint ask it, and GITEA_TOKEN for private repositories)", host, Plural(v.unnamed[host], "repository", "repositories"), host))
	}
	if len(v.failed) > 0 {
		out = append(out, "visibility: lookup failed for "+strings.Join(v.failed, ", ")+"; left unknown")
	}
	return out
}

// Plural writes a count with the noun in the right number.
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
