package fetch

import (
	"context"
	"testing"
)

// Calling the returned function fetches the public-good TUF root over the
// network, so nothing here calls it: the test pins only the shape.
func TestPublicTrustedRootReturnsAFetcher(t *testing.T) {
	t.Parallel()
	if PublicTrustedRoot(context.Background()) == nil {
		t.Fatal("no fetcher")
	}
}
