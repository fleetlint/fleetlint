package fleet

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/fixplan"
	"github.com/fleetlint/fleetlint/internal/fixpr"
	"github.com/fleetlint/fleetlint/internal/forge"
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
	// Require and Version go to the pull requests of PullRequest; Client
	// opens them, knowing the --from hosts and --forge and --token.
	Require []string
	Version string
	Client  forge.Client
	opened  int
}

// PullRequest commits the repository's fixes on the fixes branch and opens
// or updates its pull request (fleetlint fix --apply --pr), with the
// credentials the clone was made with. Its PullRequest method is an
// Options.PullRequest.
func (f *Fixer) PullRequest(ctx context.Context, e Entry, r *repo.Repo) (string, error) {
	res, err := fixpr.Open(ctx, r, fixpr.Options{Rules: f.Rules, Require: f.Require, Version: f.Version, GitEnv: cloneAuth(e), Client: f.Client})
	if err != nil {
		fmt.Fprintf(f.W, "%s: %v\n", e.Name, err) //nolint:errcheck // the listing; the note carries the error too
		return "", err
	}
	if res.Changes == 0 {
		return "", nil
	}
	f.opened++
	f.repos++
	f.changes += res.Changes
	verb := "opened"
	if res.Updated {
		verb = "updated"
	}
	note := fmt.Sprintf("%d fix(es) proposed on %s: %s pull request %s", res.Changes, fixpr.Branch, verb, res.URL)
	_, err = fmt.Fprintf(f.W, "%s: %s\n", e.Name, note)
	return note, err
}

// Fix plans and lists the repository's changes and, with Apply, writes
// them; it returns how many it wrote, or ErrNotApplied with the reason
// when it refused to.
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
	var refused error
	if f.Apply && !f.AllowDirty && len(changes) > 0 {
		refused = fix.CheckClean(r)
	}
	if refused != nil {
		f.refused++
		fmt.Fprintf(&b, "  not applied: %v\n", refused)
	} else if len(changes) > 0 {
		f.repos++
		f.changes += len(changes)
	}
	if _, err := io.WriteString(f.W, b.String()); err != nil {
		return 0, err
	}
	if refused != nil {
		return 0, fmt.Errorf("%w: %w", ErrNotApplied, refused)
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
	case f.opened > 0:
		_, err = fmt.Fprintf(f.W, "\nproposed %d change(s) in %d pull requests on %s; working trees are untouched.\n", f.changes, f.opened, fixpr.Branch)
	case f.refused > 0:
		_, err = fmt.Fprintf(f.W, "\napplied %d change(s) in %d repositories; %d repositories with uncommitted changes (or a state git could not read) were left alone (--allow-dirty writes into them too). Nothing is committed.\n", f.changes, f.repos, f.refused)
	case f.changes == 0:
		_, err = fmt.Fprintln(f.W, "nothing to fix")
	case f.Apply:
		_, err = fmt.Fprintf(f.W, "\napplied %d change(s) in %d repositories; the report shows the state after them. Nothing is committed.\n", f.changes, f.repos)
	default:
		_, err = fmt.Fprintf(f.W, "\n%d change(s) planned in %d repositories. Re-run with --apply to write them (`fleetlint fix` in a repository shows the diff).\n", f.changes, f.repos)
	}
	return err
}
