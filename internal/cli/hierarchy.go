package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/report"
)

// hierarchyCmd prints the catalogs of the repository's run in the order
// they applied, with their pins: what `check --format json` lists under
// `catalogs`, on its own.
func hierarchyCmd(g *globals) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "hierarchy",
		Short: "Print the catalogs in the order they apply, with their pins",
		Long:  "Lists every catalog of the run in the order it applied: how it was referenced, its label, what it calls itself, the digest or artifact that pins its content, what it builds on and what it contributed. A later catalog adjusts an earlier one within the earlier one's policy, so the order is the precedence.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, eff, err := load(cmd.Context(), g)
			if err != nil {
				return err
			}
			h := eff.Hierarchy()
			switch format {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(h)
			case "markdown":
				_, err := fmt.Fprint(cmd.OutOrStdout(), report.HierarchyMarkdown(h))
				return err
			case "table":
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), report.HierarchyIntro); err != nil {
					return err
				}
				return report.WriteHierarchy(cmd.OutOrStdout(), h)
			}
			return &configError{fmt.Errorf("unknown format %q (want table, json or markdown)", format)}
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "table", "output format: table, json, markdown")
	return cmd
}
