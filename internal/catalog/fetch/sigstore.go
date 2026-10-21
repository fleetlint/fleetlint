package fetch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
)

// trustedRootTimeout bounds one fetch of the sigstore TUF root.
const trustedRootTimeout = 2 * time.Minute

// fetchTrustedRoot is the TUF fetch; a variable so tests can block it.
var fetchTrustedRoot = root.FetchTrustedRootWithOptions

// PublicTrustedRoot fetches the sigstore public-good trusted root through
// TUF once and caches it for the process. The fetch is bounded by ctx and
// trustedRootTimeout; a run that is cancelled does not wait for TUF.
func PublicTrustedRoot(ctx context.Context) func() (root.TrustedMaterial, error) {
	var (
		once sync.Once
		tr   *root.TrustedRoot
		err  error
	)
	return func() (root.TrustedMaterial, error) {
		once.Do(func() { tr, err = boundedTrustedRoot(ctx) })
		if err != nil {
			return nil, fmt.Errorf("sigstore trusted root: %w", err)
		}
		return tr, nil
	}
}

// boundedTrustedRoot runs the TUF fetch under a deadline. The TUF client
// only takes a context for background tasks, so the call itself is left
// in a goroutine when ctx ends.
func boundedTrustedRoot(ctx context.Context) (*root.TrustedRoot, error) {
	ctx, cancel := context.WithTimeout(ctx, trustedRootTimeout)
	defer cancel()
	type result struct {
		tr  *root.TrustedRoot
		err error
	}
	done := make(chan result, 1)
	fetch := fetchTrustedRoot
	go func() {
		tr, err := fetch(tuf.DefaultOptions().WithContext(ctx))
		done <- result{tr, err}
	}()
	select {
	case r := <-done:
		return r.tr, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
