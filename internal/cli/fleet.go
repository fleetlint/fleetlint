package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/fixplan"
	"github.com/fleetlint/fleetlint/internal/fleet"
)

// fleetFlags are the fleet command's flags.
type fleetFlags struct {
	specPath, out, cache               string
	from, discoverRoots, exclude, rule []string
	update, fail, fix, apply           bool
}

func fleetCmd(g *globals, code *int) *cobra.Command {
	var f fleetFlags
	cmd := &cobra.Command{
		Use:   "fleet [path...]",
		Short: "Check many repositories and write one report (matrix, per-repo tasks, history)",
		Long: `fleet reads a fleet file (--repos) and/or repository paths given as arguments,
runs check in each, and writes fleet.html, fleet.md, fleet.json, actions/<repo>.md
and history/<timestamp>.json into --out. URL entries are cloned into --cache.
--from lists an owner's repositories through the forge API (GITHUB_TOKEN or
GITEA_TOKEN from the environment, if set) and clones them over https with the
same token; without one, public repositories only. --discover walks a
directory for git repositories (groups follow the directory layout) and
lists project directories that are in no repository of their own.
--fix plans the mechanical fixes (fleetlint fix) in every local repository;
with --apply it writes them and the report shows the state after them.
Nothing is committed.`,
		RunE: func(cmd *cobra.Command, args []string) error { return runFleet(cmd, g, code, f, args) },
	}
	cmd.Flags().StringVar(&f.specPath, "repos", "", "fleet file (version, cache, include, defaults, groups, repos: [{path|url, name, team, group, tier, visibility}])")
	cmd.Flags().StringArrayVar(&f.from, "from", nil, "discover repositories on a forge: github:<owner> or gitea:<host>/<owner> (repeatable; archived repositories and forks are skipped)")
	cmd.Flags().StringArrayVar(&f.discoverRoots, "discover", nil, fmt.Sprintf("find git repositories under a directory, up to %d levels down (repeatable)", fleet.DiscoverDepth))
	cmd.Flags().StringArrayVar(&f.exclude, "exclude", nil, "with --discover: skip paths matching this glob, relative to the directory (e.g. '*/third_party/*'; repeatable)")
	cmd.Flags().StringVar(&f.out, "out", "fleet-report", "output directory")
	cmd.Flags().StringVar(&f.cache, "cache", "", "directory for cloned url entries (overrides the fleet file)")
	cmd.Flags().BoolVar(&f.update, "update", false, "git pull cached clones before checking")
	cmd.Flags().BoolVar(&f.fail, "fail", false, "exit 1 if any repository has errors")
	cmd.Flags().BoolVar(&f.fix, "fix", false, "plan the mechanical fixes in every local repository (dry run)")
	cmd.Flags().BoolVar(&f.apply, "apply", false, "with --fix: write the changes; nothing is committed")
	cmd.Flags().StringArrayVar(&f.rule, "rule", nil, "with --fix: only fix this rule (repeatable)")
	cmd.Flags().BoolVar(&g.deep, "deep", false, "also run rules that execute repository code")
	return cmd
}

func runFleet(cmd *cobra.Command, g *globals, code *int, f fleetFlags, args []string) error {
	spec, err := fleetSpec(cmd.Context(), f, args)
	if err != nil {
		return &configError{err}
	}
	asked := &fleet.ForgeLookup{}
	opts := fleet.Options{
		Deep: g.deep, Update: f.update, Cache: f.cache, Require: g.require, Version: Version,
		MarkFixable: fixplan.MarkFixable, Visibility: asked.Lookup,
	}
	fixer := &fleet.Fixer{W: cmd.OutOrStdout(), Apply: f.apply, Rules: f.rule}
	if f.fix {
		opts.Fix = fixer.Fix
	}
	rep, err := fleet.Run(cmd.Context(), spec, opts)
	if err != nil {
		return err
	}
	for _, n := range asked.Notes() {
		fmt.Fprintln(cmd.ErrOrStderr(), n) //nolint:errcheck // a note on stderr
	}
	for _, r := range rep.Repos {
		for _, d := range r.Deprecations {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s: %s\n", r.Name, config.FileName, d) //nolint:errcheck // a warning on stderr
		}
	}
	if f.fix {
		if err := fixer.Summary(); err != nil {
			return err
		}
	}
	if err := fleet.Write(f.out, rep); err != nil {
		return err
	}
	failing, errs := tally(rep)
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%d repositories checked, %d with errors, %d could not be checked. Report: %s\n",
		len(rep.Repos), failing, errs, filepath.Join(f.out, "fleet.html"))
	if f.fail && (failing > 0 || errs > 0) {
		*code = ExitFindings
	}
	return err
}

// fleetSpec assembles the repositories from the fleet file, the arguments,
// --discover and --from.
func fleetSpec(ctx context.Context, f fleetFlags, args []string) (*fleet.Spec, error) {
	if !f.fix && (f.apply || len(f.rule) > 0) {
		return nil, errors.New("--apply and --rule need --fix")
	}
	return fleet.Assemble(ctx, fleet.Sources{SpecPath: f.specPath, Paths: args, Discover: f.discoverRoots, Exclude: f.exclude, From: f.from})
}

// tally counts repositories with error findings and repositories that could not be checked.
func tally(rep *fleet.Report) (failing, errs int) {
	for _, r := range rep.Repos {
		switch {
		case r.Err != "":
			errs++
		case r.Summary.Errors > 0 || r.Summary.Error > 0:
			failing++
		}
	}
	return failing, errs
}
