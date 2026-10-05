// Package builtin holds the rules that have to be code: those that execute
// something in the repository. Importing it registers them; the catalog
// carries their metadata under the same id. Every other rule is data in the
// catalog.
package builtin

import (
	"github.com/fleetlint/fleetlint/internal/rules"
)

func init() {
	rules.Register(checkPasses{})
}
