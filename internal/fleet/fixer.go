package fleet

import (
	"fmt"
	"io"
	"strings"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/fixplan"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// Fixer plans the mechanical fixes of each repository, lists them on W and
// writes them when Apply is set, into clean working trees only unless
// AllowDirty is set; Rules limits it to those rules. Its Fix method is an
// Options.Fix.
type Fixer struct {
	W                       io.Writer
	Apply, AllowDirty       bool
	Rules                   []string
	changes, repos, refused int
}

// Fix plans and lists the repository's changes and, with Apply, writes
// them; it returns how many it wrote, or ErrDirty when it refused to.
func (f *Fixer) Fix(name string, r *repo.Repo, run *engine.Run) (int, error) {
	// A rule that cannot be planned here is left to a person; the others go ahead.
	var skipped []string
	changes, _, err := fixplan.PlanEach(r, run, f.Rules, func(id string, err error) error {
		skipped = append(skipped, fmt.Sprintf("  skipped %s: %v\n", id, err))
		return nil
	})
	if err != nil || len(changes)+len(skipped) == 0 {
		return 0, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", name)
	for _, c := range changes {
		fmt.Fprintf(&b, "  %-7s %s\n", c.Kind, c.Path)
	}
	b.WriteString(strings.Join(skipped, ""))
	dirty := f.Apply && !f.AllowDirty && len(changes) > 0 && DirtyCount(r) > 0
	if dirty {
		f.refused++
		fmt.Fprintf(&b, "  not applied: %v\n", ErrDirty)
	} else if len(changes) > 0 {
		f.repos++
		f.changes += len(changes)
	}
	if _, err := io.WriteString(f.W, b.String()); err != nil {
		return 0, err
	}
	if dirty {
		return 0, ErrDirty
	}
	if !f.Apply {
		return 0, nil
	}
	if err := fix.Apply(changes); err != nil {
		return 0, fmt.Errorf("fix: %w", err)
	}
	return len(changes), nil
}

// Summary ends the listing with what was planned or written.
func (f *Fixer) Summary() error {
	var err error
	switch {
	case f.refused > 0:
		_, err = fmt.Fprintf(f.W, "\napplied %d change(s) in %d repositories; %d repositories with uncommitted changes were left alone (--allow-dirty writes into them too). Nothing is committed.\n", f.changes, f.repos, f.refused)
	case f.changes == 0:
		_, err = fmt.Fprintln(f.W, "nothing to fix")
	case f.Apply:
		_, err = fmt.Fprintf(f.W, "\napplied %d change(s) in %d repositories; the report shows the state after them. Nothing is committed.\n", f.changes, f.repos)
	default:
		_, err = fmt.Fprintf(f.W, "\n%d change(s) planned in %d repositories. Re-run with --apply to write them (`fleetlint fix` in a repository shows the diff).\n", f.changes, f.repos)
	}
	return err
}
