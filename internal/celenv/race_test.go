package celenv_test

import (
	"context"
	"sync"
	"testing"

	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
)

// Parallel config loads type-check on one shared Env (fleet --jobs, parallel tests).
// cel-go builds its symbol catalog on the first undeclared reference while a
// comprehension copies it; one goroutine each makes -race see it every time.
func TestTypeCheckConcurrently(t *testing.T) {
	e, err := celenv.New(context.Background(), nil, facts.Facts{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() { _ = e.TypeCheck("nosuch.thing") })
	wg.Go(func() { _ = e.TypeCheck("[1].exists(x, x > 0)") })
	wg.Wait()
}
