// Package builtin holds the Go-implemented rules shipped with fleetlint.
// Importing it registers them. Each rule lives in its own file with its
// tests; the catalog carries the metadata under the same id.
package builtin

import (
	"github.com/fleetlint/fleetlint/internal/rules"
)

func init() {
	rules.Register(trackedJunk{})
	rules.Register(lockfileCommitted{})
	rules.Register(actionsPinned{})
	rules.Register(checkPasses{})
}
