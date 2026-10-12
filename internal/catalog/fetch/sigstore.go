package fetch

import (
	"context"
	"fmt"
	"sync"

	"github.com/sigstore/sigstore-go/pkg/root"
)

// PublicTrustedRoot fetches the sigstore public-good trusted root through
// TUF once and caches it for the process. ctx is unused: the TUF client
// manages its own timeouts and on-disk cache.
func PublicTrustedRoot(_ context.Context) func() (root.TrustedMaterial, error) {
	var (
		once sync.Once
		tr   *root.TrustedRoot
		err  error
	)
	return func() (root.TrustedMaterial, error) {
		once.Do(func() { tr, err = root.FetchTrustedRoot() })
		if err != nil {
			return nil, fmt.Errorf("sigstore trusted root: %w", err)
		}
		return tr, nil
	}
}
