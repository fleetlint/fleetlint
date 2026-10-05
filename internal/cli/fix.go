package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

func fixCmd(g *globals, code *int) *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "fix [rule-id...]",
		Short: "Show (or with --apply, perform) the mechanical fixes for failing rules",
		Long: `fix plans the declarative actions attached to failing rules: creating files from
templates, untracking files, adding .gitignore patterns. It never overwrites an
existing file. Without --apply it only prints what it would do. Rules without
actions print their instruction instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, eff, err := load(cmd.Context(), g)
			if err != nil {
				return err
			}
			run, err := engine.Evaluate(cmd.Context(), r, eff)
			if err != nil {
				return err
			}
			changes, manual, err := planFixes(r, run, args)
			if err != nil {
				return &configError{err}
			}
			out := cmd.OutOrStdout()
			if err := printPlan(out, changes, manual, apply); err != nil {
				return err
			}
			if !apply || len(changes) == 0 {
				return nil
			}
			if err := fix.Apply(changes); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "\napplied %d change(s). Run `fleetlint check` to confirm.\n", len(changes))
			*code = ExitOK
			return err
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the changes (default is a dry run)")
	return cmd
}

type manualFix struct {
	rule model.Result
}

func planFixes(r *repo.Repo, run *engine.Run, only []string) ([]fix.Change, []manualFix, error) {
	want := map[string]bool{}
	for _, id := range only {
		want[id] = true
	}
	var changes []fix.Change
	var manual []manualFix
	seen := map[string]bool{}
	for _, res := range run.Results {
		if res.Status != model.StatusFail || (len(want) > 0 && !want[res.Rule.ID]) {
			continue
		}
		planned, err := planRule(r, run, res)
		if err != nil {
			return nil, nil, fmt.Errorf("rule %s: %w", res.Rule.ID, err)
		}
		if len(planned) == 0 {
			manual = append(manual, manualFix{res})
			continue
		}
		changes = appendUnique(changes, seen, planned)
	}
	return changes, manual, nil
}

// markFixable flags the failing results whose fix is mechanical here: the
// rule has actions and they plan at least one change for this repository.
// A rule with a template is not fixable when the file already exists.
func markFixable(r *repo.Repo, run *engine.Run) {
	for i := range run.Results {
		res := &run.Results[i]
		if res.Status != model.StatusFail {
			continue
		}
		planned, err := planRule(r, run, *res)
		res.Fixable = err == nil && len(planned) > 0
	}
}

// appendUnique drops changes already planned for the same path. The root
// scope sees member files too, so one file can be planned from two scopes,
// and applying the same untrack or create twice fails.
func appendUnique(changes []fix.Change, seen map[string]bool, planned []fix.Change) []fix.Change {
	for _, ch := range planned {
		key := ch.Kind + " " + ch.Path
		if ch.Kind != "append" && seen[key] {
			continue
		}
		seen[key] = true
		changes = append(changes, ch)
	}
	return changes
}

// planRule returns the changes for one failing rule in its scope, with paths
// made repo-relative; an empty result means the fix is not mechanical.
func planRule(r *repo.Repo, run *engine.Run, res model.Result) ([]fix.Change, error) {
	actions, err := fix.Decode(res.Rule.Fix.Actions)
	if err != nil || len(actions) == 0 {
		return nil, err
	}
	scope := r
	if res.Scope != "" {
		var err error
		if scope, err = r.Scoped(res.Scope); err != nil {
			return nil, err
		}
	}
	p := project(run, res.Scope)
	p.Vars = map[string]string{}
	if id := fix.DetectLicense(r); id != "" {
		p.Vars["license"] = id
	}
	planned, err := fix.PlanFor(scope, p, actions, catalog.EmbeddedTemplates{}, res.Findings)
	if err != nil {
		return nil, err
	}
	if res.Scope != "" {
		for i := range planned {
			planned[i].Path = res.Scope + "/" + planned[i].Path
		}
	}
	return planned, nil
}

// project describes a scope for the templates: its stack and, for the root,
// the nested projects that share its hook configuration and workflow.
func project(run *engine.Run, scope string) fix.Project {
	p := fix.Project{Stack: primaryStack(run, scope), Runner: run.Facts.TaskRunner.Kind}
	if scope == "" {
		p.Container = run.Facts.Container
	}
	for _, s := range run.Scopes {
		// Further stacks in the same directory share its hooks and workflow.
		if s.Path == scope && len(s.Facts.Stacks) > 1 {
			for _, extra := range s.Facts.Stacks[1:] {
				p.Nested = append(p.Nested, fix.Nested{Stack: extra})
			}
		}
	}
	if scope != "" {
		return p
	}
	for _, s := range run.Scopes {
		if s.Path != "" && len(s.Facts.Stacks) > 0 {
			p.Nested = append(p.Nested, fix.Nested{Path: s.Path, Stack: s.Facts.Stacks[0]})
		}
	}
	return p
}

func primaryStack(run *engine.Run, scope string) string {
	for _, s := range run.Scopes {
		if s.Path == scope && len(s.Facts.Stacks) > 0 {
			return s.Facts.Stacks[0]
		}
	}
	return ""
}

func printPlan(w io.Writer, changes []fix.Change, manual []manualFix, apply bool) error {
	var b strings.Builder
	if len(changes) == 0 && len(manual) == 0 {
		b.WriteString("nothing to fix\n")
	}
	for _, c := range changes {
		b.WriteString(c.Diff)
		b.WriteString("\n")
	}
	if len(changes) > 0 && !apply {
		fmt.Fprintf(&b, "%d change(s) planned. Re-run with --apply to write them.\n", len(changes))
	}
	if len(manual) > 0 {
		b.WriteString("\nneeds a person or an agent:\n")
		for _, m := range manual {
			fmt.Fprintf(&b, "  %-28s %s\n", m.rule.Rule.ID, m.rule.Rule.Fix.Human)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
