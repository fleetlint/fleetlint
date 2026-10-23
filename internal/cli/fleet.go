package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/fixplan"
	"github.com/fleetlint/fleetlint/internal/fixpr"
	"github.com/fleetlint/fleetlint/internal/fleet"
	"github.com/fleetlint/fleetlint/internal/forge"
)

// fleetFlags are the fleet command's flags.
type fleetFlags struct {
	specPath, out, cache               string
	from, discoverRoots, exclude, rule []string
	update, fail, fix, apply           bool
	deepClones, allowDirty, pr         bool
	keepHistory, jobs                  int
	forge                              forgeFlags
}

func fleetCmd(g *globals, code *int) *cobra.Command {
	var f fleetFlags
	cmd := &cobra.Command{
		Use:   "fleet [path...]",
		Short: "Check many repositories and write one report (matrix, per-repo tasks, history)",
		Long: `fleet reads a fleet file (--repos) and/or repository paths given as arguments,
runs check in each, and writes fleet.html, fleet.md, fleet.json, actions/<repo>.md
and history/<timestamp>.json into --out. URL entries are cloned into --cache.
--from lists an owner's repositories through the forge API (GITHUB_TOKEN,
GITEA_TOKEN or GITLAB_TOKEN from the environment, if set) and clones them over
https with the same token; without one, public repositories only. --discover walks a
directory for git repositories (groups follow the directory layout) and
lists project directories that are in no repository of their own.
--fix plans the mechanical fixes (fleetlint fix) in every local repository
(a dry run, kept out of history/); with --apply it writes them, into clean working trees only unless --allow-dirty
is given, and the report shows the state after them. Nothing is committed.
--fix --apply --pr proposes the fixes instead, as a pull request per repository
from fleetlint/fixes, clones included (fleetlint fix --apply --pr).
--deep runs rules that execute repository code in the local repositories;
clones (url entries, --from) run that code only with --deep-clones.`,
		RunE: func(cmd *cobra.Command, args []string) error { return runFleet(cmd, g, code, f, args) },
	}
	cmd.Flags().StringVar(&f.specPath, "repos", "", "fleet file (version, cache, include, defaults, groups, repos: [{path|url, name, team, group, tier, visibility}])")
	cmd.Flags().StringArrayVar(&f.from, "from", nil, "discover repositories on a forge: github:<owner>, gitea:<host>/<owner> or gitlab:<host>/<group> with its subgroups (repeatable; archived repositories and forks are skipped)")
	cmd.Flags().StringArrayVar(&f.discoverRoots, "discover", nil, fmt.Sprintf("find git repositories under a directory, up to %d levels down (repeatable)", fleet.DiscoverDepth))
	cmd.Flags().StringArrayVar(&f.exclude, "exclude", nil, "with --discover: skip paths matching this glob, relative to the directory (e.g. '*/third_party/*'; repeatable)")
	cmd.Flags().StringVar(&f.out, "out", "fleet-report", "output directory")
	cmd.Flags().StringVar(&f.cache, "cache", "", "directory for cloned url entries (overrides the fleet file)")
	cmd.Flags().BoolVar(&f.update, "update", false, "git pull cached clones before checking")
	cmd.Flags().BoolVar(&f.fail, "fail", false, "exit 1 if any repository has errors")
	cmd.Flags().IntVar(&f.jobs, "jobs", 4, "check this many repositories at once")
	addForgeFlags(cmd, &f.forge)
	cmd.Flags().IntVar(&f.keepHistory, "keep-history", 0, "keep only the latest N runs in history/ (0 keeps all)")
	cmd.Flags().BoolVar(&f.fix, "fix", false, "plan the mechanical fixes in every local repository (dry run)")
	cmd.Flags().BoolVar(&f.apply, "apply", false, "with --fix: write the changes; nothing is committed")
	cmd.Flags().StringArrayVar(&f.rule, "rule", nil, "with --fix: only fix this rule (repeatable)")
	cmd.Flags().BoolVar(&f.pr, "pr", false, "with --fix --apply: propose each repository's fixes as a pull request from "+fixpr.Branch+" (clones too) instead of writing working trees")
	cmd.Flags().BoolVar(&f.allowDirty, "allow-dirty", false, "with --fix --apply: write into working trees that have uncommitted changes")
	cmd.Flags().BoolVar(&g.deep, "deep", false, "also run rules that execute repository code (local repositories only)")
	cmd.Flags().BoolVar(&f.deepClones, "deep-clones", false, "with --deep: also execute the code of cloned repositories (url entries, --from), which you may not control")
	return cmd
}

func runFleet(cmd *cobra.Command, g *globals, code *int, f fleetFlags, args []string) error {
	spec, client, err := fleetInputs(cmd.Context(), g, f, args)
	if err != nil {
		return err
	}
	asked := &fleet.ForgeLookup{Client: client}
	opts := fleet.Options{
		Deep: g.deep, DeepClones: f.deepClones, Update: f.update, Cache: f.cache, Require: g.require, Version: Version,
		MarkFixable: fixplan.MarkFixable, Visibility: asked.Lookup, Jobs: f.jobs,
	}
	fixer := &fleet.Fixer{W: cmd.OutOrStdout(), Apply: f.apply, AllowDirty: f.allowDirty, Rules: f.rule, Require: g.require, Version: Version, Client: client}
	switch {
	case f.pr:
		opts.PullRequest = fixer.PullRequest
	case f.fix:
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
	if err := fleet.WriteWith(f.out, rep, fleet.WriteOptions{Keep: f.keepHistory, NoHistory: f.fix && !f.apply}); err != nil {
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
func fleetSpec(ctx context.Context, g *globals, f fleetFlags, args []string, client forge.Client) (*fleet.Spec, error) {
	if !f.fix && (f.apply || len(f.rule) > 0) {
		return nil, errors.New("--apply and --rule need --fix")
	}
	if f.allowDirty && !f.apply {
		return nil, errors.New("--allow-dirty needs --fix --apply")
	}
	if f.pr && (!f.apply || f.allowDirty) {
		return nil, errors.New("--pr needs --fix --apply and works without --allow-dirty (no working tree is written)")
	}
	if f.deepClones && !g.deep {
		return nil, errors.New("--deep-clones needs --deep")
	}
	return fleet.Assemble(ctx, fleet.Sources{SpecPath: f.specPath, Paths: args, Discover: f.discoverRoots, Exclude: f.exclude, From: f.from, Client: client})
}

// fleetInputs are the run's repositories and its forge client.
func fleetInputs(ctx context.Context, g *globals, f fleetFlags, args []string) (*fleet.Spec, forge.Client, error) {
	client, err := fleetClient(f)
	if err != nil {
		return nil, client, err
	}
	spec, err := fleetSpec(ctx, g, f, args, client)
	if err != nil {
		return nil, client, &configError{err}
	}
	return spec, client, nil
}

// fleetClient is the forge client of the run: --forge and --token, and
// the hosts of the --from sources, which the user named.
func fleetClient(f fleetFlags) (forge.Client, error) {
	hosts := make([]string, 0, len(f.from))
	for _, s := range f.from {
		if src, err := forge.ParseSource(s); err == nil {
			hosts = append(hosts, src.Host)
		}
	}
	return f.forge.client(hosts...)
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
