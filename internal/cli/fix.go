package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/fixplan"
	"github.com/fleetlint/fleetlint/internal/fixpr"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

func fixCmd(g *globals, code *int) *cobra.Command {
	var apply, allowDirty, pr bool
	cmd := &cobra.Command{
		Use:   "fix [rule-id...]",
		Short: "Show (or with --apply, perform) the mechanical fixes for failing rules",
		Long: `fix plans the declarative actions attached to failing rules: creating files from
templates, untracking files, adding .gitignore patterns. It never overwrites an
existing file. Without --apply it only prints what it would do, and --apply
writes only into a clean working tree unless --allow-dirty is given. Rules
without actions print their instruction instead.

--apply --pr leaves the working tree alone: it fixes the default branch in a
temporary worktree, commits on fleetlint/fixes, force-pushes that branch and
opens or updates its pull request, through the forge's API with GITHUB_TOKEN,
GITEA_TOKEN or GITLAB_TOKEN, else with gh, tea or glab.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if pr {
				if !apply || allowDirty {
					return &configError{errors.New("--pr needs --apply and works without --allow-dirty (the working tree is not touched)")}
				}
				return runFixPR(cmd, g, code, args)
			}
			return runFix(cmd, g, code, args, apply, allowDirty)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the changes (default is a dry run)")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "with --apply: write although the working tree has uncommitted changes")
	cmd.Flags().BoolVar(&pr, "pr", false, "with --apply: commit the fixes of the default branch on "+fixpr.Branch+" and open a pull request")
	return cmd
}

func runFixPR(cmd *cobra.Command, g *globals, code *int, rules []string) error {
	r, err := repo.Open(cmd.Context(), g.path)
	if err != nil {
		return &configError{err}
	}
	res, err := fixpr.Open(cmd.Context(), r, fixpr.Options{Rules: rules, Require: g.require, Version: Version})
	out := cmd.OutOrStdout()
	if err != nil {
		return err
	}
	*code = ExitOK
	switch {
	case res.Changes == 0:
		_, err = fmt.Fprintf(out, "nothing to fix on %s\n", res.Base)
	case res.Updated:
		_, err = fmt.Fprintf(out, "%d change(s) on %s; updated pull request %s\n", res.Changes, fixpr.Branch, orText(res.URL, "(no URL returned)"))
	default:
		_, err = fmt.Fprintf(out, "%d change(s) on %s; opened pull request %s\n", res.Changes, fixpr.Branch, orText(res.URL, "(no URL returned)"))
	}
	return err
}

func orText(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func runFix(cmd *cobra.Command, g *globals, code *int, rules []string, apply, allowDirty bool) error {
	r, eff, err := load(cmd.Context(), g)
	if err != nil {
		return err
	}
	run, err := engine.Evaluate(cmd.Context(), r, eff)
	if err != nil {
		return err
	}
	changes, manual, err := fixplan.Plan(r, run, rules)
	if err != nil {
		return &configError{err}
	}
	if apply && len(changes) > 0 && !allowDirty {
		if err := fix.CheckClean(r); err != nil {
			return &configError{err}
		}
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
}

func printPlan(w io.Writer, changes []fix.Change, manual []model.Result, apply bool) error {
	var b strings.Builder
	if len(changes) == 0 && len(manual) == 0 {
		b.WriteString("nothing to fix\n")
	}
	for _, c := range changes {
		b.WriteString(c.Diff)
		if c.From != "" {
			b.WriteString("  (template from catalog " + c.From + ")\n")
		}
		b.WriteString("\n")
	}
	if len(changes) > 0 && !apply {
		fmt.Fprintf(&b, "%d change(s) planned. Re-run with --apply to write them.\n", len(changes))
	}
	if len(manual) > 0 {
		b.WriteString("\nneeds a person or an agent:\n")
		for _, m := range manual {
			fmt.Fprintf(&b, "  %-28s %s\n", m.Rule.ID, m.Rule.Fix.Human)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
