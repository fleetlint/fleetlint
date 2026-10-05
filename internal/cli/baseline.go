package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/baseline"
	"github.com/fleetlint/fleetlint/internal/engine"
)

func baselineCmd(g *globals) *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Grandfather the current findings so only new ones fail (the file may only shrink afterwards)",
		Long: `baseline writes .fleetlint-baseline.json (or the path in .fleetlint.yaml) with a
fingerprint of every current finding. Findings in the file are reported as
baselined instead of failing. Re-running removes entries that no longer match and
never adds new ones, so the debt can only go down; --reset starts over.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, eff, err := load(cmd.Context(), g)
			if err != nil {
				return err
			}
			run, err := engine.Evaluate(cmd.Context(), r, eff)
			if err != nil {
				return err
			}
			path := eff.File.Baseline
			if path == "" {
				path = baseline.DefaultFile
			}
			abs, err := r.Abs(path)
			if err != nil {
				return &configError{err}
			}
			existing, exists, err := baseline.Load(abs)
			if err != nil {
				return &configError{err}
			}
			if !exists {
				existing = nil
			}
			entries := baseline.Build(run.Results)
			added, removed, err := baseline.Write(abs, entries, existing, reset, time.Now())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %d entries (%d added, %d removed). Commit it; `fleetlint check` now fails only on new findings.\n", path, len(entries)-countAdded(entries, existing, reset), added, removed)
			return err
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "grandfather everything currently failing, including findings newer than the existing baseline")
	return cmd
}

// countAdded is the number of current findings a non-reset write refused to add.
func countAdded(entries []string, existing *baseline.Baseline, reset bool) int {
	if reset || existing == nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !existing.Has(e) {
			n++
		}
	}
	return n
}
