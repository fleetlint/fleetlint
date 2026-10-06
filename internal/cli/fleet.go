package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fleet"
	"github.com/fleetlint/fleetlint/internal/forge"
)

func fleetCmd(g *globals, code *int) *cobra.Command {
	var specPath, out, cache string
	var from []string
	var update, failOnFindings bool
	cmd := &cobra.Command{
		Use:   "fleet [path...]",
		Short: "Check many repositories and write one report (matrix, per-repo tasks, history)",
		Long: `fleet reads a fleet file (--repos) and/or repository paths given as arguments,
runs check in each, and writes fleet.html, fleet.md, fleet.json, actions/<repo>.md
and history/<timestamp>.json into --out. URL entries are cloned into --cache.
--from lists an owner's repositories through the forge API (GITHUB_TOKEN or
GITEA_TOKEN from the environment, if set) and clones them over https with the
same token; without one, public repositories only.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := buildSpec(specPath, args)
			if err == nil {
				err = addDiscovered(cmd.Context(), spec, from)
			}
			if err != nil {
				return &configError{err}
			}
			rep, err := fleet.Run(cmd.Context(), spec, fleet.Options{Deep: g.deep, Update: update, Cache: cache, Require: g.require, Version: Version})
			if err != nil {
				return err
			}
			if err := fleet.Write(out, rep); err != nil {
				return err
			}
			failing, errs := tally(rep)
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%d repositories checked, %d with errors, %d could not be checked. Report: %s\n",
				len(rep.Repos), failing, errs, filepath.Join(out, "fleet.html"))
			if failOnFindings && (failing > 0 || errs > 0) {
				*code = ExitFindings
			}
			return err
		},
	}
	cmd.Flags().StringVar(&specPath, "repos", "", "fleet file (version, cache, repos: [{path|url, name, team}])")
	cmd.Flags().StringArrayVar(&from, "from", nil, "discover repositories on a forge: github:<owner> or gitea:<host>/<owner> (repeatable; archived repositories and forks are skipped)")
	cmd.Flags().StringVar(&out, "out", "fleet-report", "output directory")
	cmd.Flags().StringVar(&cache, "cache", "", "directory for cloned url entries (overrides the fleet file)")
	cmd.Flags().BoolVar(&update, "update", false, "git pull cached clones before checking")
	cmd.Flags().BoolVar(&failOnFindings, "fail", false, "exit 1 if any repository has errors")
	cmd.Flags().BoolVar(&g.deep, "deep", false, "also run rules that execute repository code")
	return cmd
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

func buildSpec(specPath string, paths []string) (*fleet.Spec, error) {
	spec := &fleet.Spec{Version: 1}
	if specPath != "" {
		loaded, err := fleet.LoadSpec(specPath)
		if err != nil {
			return nil, err
		}
		spec = loaded
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		spec.Repos = append(spec.Repos, fleet.Entry{Path: abs, Name: filepath.Base(abs)})
	}
	return spec, nil
}

// addDiscovered appends the repositories of each --from source and makes
// sure the run has something to check and somewhere to clone into.
func addDiscovered(ctx context.Context, spec *fleet.Spec, from []string) error {
	names := map[string]bool{}
	for _, e := range spec.Repos {
		names[e.Name] = true
	}
	for _, s := range from {
		entries, err := discover(ctx, s)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if names[e.Name] {
				return fmt.Errorf("%s: repository name %q is already in the fleet", s, e.Name)
			}
			names[e.Name] = true
			spec.Repos = append(spec.Repos, e)
		}
	}
	if len(spec.Repos) == 0 {
		return errors.New("no repositories: pass paths, --repos fleet.yaml or --from github:<owner>")
	}
	if len(from) > 0 && spec.Cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("no cache directory for clones (set --cache): %w", err)
		}
		spec.Cache = filepath.Join(dir, "fleetlint", "repos")
	}
	return nil
}

// discover lists one forge source as fleet entries, without archived
// repositories and forks.
func discover(ctx context.Context, source string) ([]fleet.Entry, error) {
	src, err := forge.ParseSource(source)
	if err != nil {
		return nil, err
	}
	client := forge.Client{}
	repos, err := client.List(ctx, src)
	if err != nil {
		return nil, err
	}
	entries := make([]fleet.Entry, 0, len(repos))
	for _, fr := range repos {
		if fr.Archived || fr.Fork {
			continue
		}
		e := fleet.Entry{Name: fr.Name, URL: fr.CloneURL, Visibility: string(facts.VisibilityPublic), Token: client.Token(src)}
		if fr.Private {
			e.Visibility = string(facts.VisibilityPrivate)
		}
		if err := e.Normalize(); err != nil {
			return nil, fmt.Errorf("%s: repository %s: %w", source, fr.Name, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
