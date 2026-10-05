// Package rules defines the interface Go-implemented rules satisfy and the
// registry the engine looks them up in. A catalog declares `kind: go` with an
// id; the implementation registered under that id does the work. Keeping the
// metadata in the catalog and the logic here means docs, severity and tiers
// stay data while detection stays testable code.
package rules

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// Context is what a rule gets to look at.
type Context struct {
	Repo   *repo.Repo
	Facts  facts.Facts
	Params map[string]any
	// Deep is true when the user opted into checks that execute repository
	// code (`--deep`). Rules that need it return not-applicable otherwise.
	Deep bool
}

// ErrNeedsDeep is returned by a checker when the rule only works in deep mode.
var ErrNeedsDeep = errors.New("needs --deep")

// Outcome is what a Go rule returns: findings (empty = pass) and optional
// evidence explaining a pass, e.g. which satisfier matched.
type Outcome struct {
	Findings []model.Finding
	Evidence string
}

// Checker is a Go-implemented rule.
type Checker interface {
	// ID must equal the catalog rule id this implementation serves.
	ID() string
	// Check evaluates the rule. An error means the rule could not run.
	Check(ctx Context) (Outcome, error)
}

// DeepChecker is a rule that runs repository code and therefore needs a
// context for cancellation and timeouts.
type DeepChecker interface {
	Checker
	CheckDeep(ctx context.Context, c Context) ([]model.Finding, string, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Checker{}
)

// Register adds a checker; it panics on a duplicate id because that is a
// programming error visible at startup.
func Register(c Checker) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[c.ID()]; dup {
		panic(fmt.Sprintf("rules: duplicate registration for %s", c.ID()))
	}
	registry[c.ID()] = c
}

// Lookup returns the checker for a rule id.
func Lookup(id string) (Checker, bool) {
	mu.RLock()
	defer mu.RUnlock()
	c, ok := registry[id]
	return c, ok
}

// IDs lists registered rule ids, sorted.
func IDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// StringList reads a []string parameter with a default.
func StringList(params map[string]any, key string, def []string) []string {
	v, ok := params[key]
	if !ok {
		return def
	}
	list, ok := v.([]any)
	if !ok {
		return def
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
