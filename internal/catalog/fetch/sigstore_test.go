package fetch

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
)

// Calling the returned function fetches the public-good TUF root over the
// network, so nothing here calls it: the test pins only the shape.
func TestPublicTrustedRootReturnsAFetcher(t *testing.T) {
	t.Parallel()
	if PublicTrustedRoot(context.Background()) == nil {
		t.Fatal("no fetcher")
	}
}

// The TUF fetch is swapped for stubs, so this test is not parallel.
func TestPublicTrustedRootHonoursContext(t *testing.T) {
	orig := fetchTrustedRoot
	t.Cleanup(func() { fetchTrustedRoot = orig })

	// A fetch that only ends with its context must not hang the run.
	fetchTrustedRoot = func(opts *tuf.Options) (*root.TrustedRoot, error) {
		<-opts.Context.Done()
		return nil, opts.Context.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := PublicTrustedRoot(ctx)(); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("want the context's error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fetch outlived its context")
	}
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := PublicTrustedRoot(cancelled)(); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}

	// A fetch that succeeds runs once per process and is cached.
	var calls atomic.Int32
	fetchTrustedRoot = func(opts *tuf.Options) (*root.TrustedRoot, error) {
		calls.Add(1)
		if opts.Context == nil {
			t.Error("no context handed to TUF")
		}
		return &root.TrustedRoot{}, nil
	}
	get := PublicTrustedRoot(context.Background())
	for range 2 {
		if tr, err := get(); err != nil || tr == nil {
			t.Fatalf("got %v, %v", tr, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("fetched %d times", calls.Load())
	}
}
