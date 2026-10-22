package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/fixplan"
	"github.com/fleetlint/fleetlint/internal/model"
)

func fixCmd(g *globals, code *int) *cobra.Command {
	var apply, allowDirty bool
	cmd := &cobra.Command{
		Use:   "fix [rule-id...]",
		Short: "Show (or with --apply, perform) the mechanical fixes for failing rules",
		Long: `fix plans the declarative actions attached to failing rules: creating files from
templates, untracking files, adding .gitignore patterns. It never overwrites an
existing file. Without --apply it only prints what it would do, and --apply
writes only into a clean working tree unless --allow-dirty is given. Rules
without actions print their instruction instead.`,
		RunE: func(cmd *cobra.Command, args []string) error { return runFix(cmd, g, code, args, apply, allowDirty) },
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the changes (default is a dry run)")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "with --apply: write although the working tree has uncommitted changes")
	return cmd
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
