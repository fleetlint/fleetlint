package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/fleet"
	"github.com/fleetlint/fleetlint/internal/forge"
	"github.com/fleetlint/fleetlint/internal/repo"
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
	cmd.Flags().StringVar(&f.specPath, "repos", "", "fleet file (version, cache, defaults, groups, repos: [{path|url, name, team, group, tier, visibility}])")
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
	asked := &visibilityLookup{}
	opts := fleet.Options{
		Deep: g.deep, Update: f.update, Cache: f.cache, Require: g.require, Version: Version,
		MarkFixable: markFixable, Visibility: asked.lookup,
	}
	fixer := &fleetFixer{w: cmd.OutOrStdout(), apply: f.apply, rules: f.rule}
	if f.fix {
		opts.Fix = fixer.fix
	}
	rep, err := fleet.Run(cmd.Context(), spec, opts)
	if err != nil {
		return err
	}
	asked.note(cmd.ErrOrStderr())
	if f.fix {
		if err := fixer.summary(); err != nil {
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
	spec, err := buildSpec(f.specPath, args)
	if err != nil {
		return nil, err
	}
	if err := addWalked(spec, f.discoverRoots, f.exclude); err != nil {
		return nil, err
	}
	return spec, addDiscovered(ctx, spec, f.from)
}

// fleetFixer plans the mechanical fixes of each repository, lists them and
// writes them with --apply.
type fleetFixer struct {
	w              io.Writer
	apply          bool
	rules          []string
	changes, repos int
}

func (f *fleetFixer) fix(name string, r *repo.Repo, run *engine.Run) (int, error) {
	// A rule that cannot be planned here is left to a person; the others go ahead.
	var skipped []string
	changes, _, err := planEach(r, run, f.rules, func(id string, err error) error {
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
	if len(changes) > 0 {
		f.repos++
		f.changes += len(changes)
	}
	if _, err := io.WriteString(f.w, b.String()); err != nil {
		return 0, err
	}
	if !f.apply {
		return 0, nil
	}
	if err := fix.Apply(changes); err != nil {
		return 0, fmt.Errorf("fix: %w", err)
	}
	return len(changes), nil
}

func (f *fleetFixer) summary() error {
	var err error
	switch {
	case f.changes == 0:
		_, err = fmt.Fprintln(f.w, "nothing to fix")
	case f.apply:
		_, err = fmt.Fprintf(f.w, "\napplied %d change(s) in %d repositories; the report shows the state after them. Nothing is committed.\n", f.changes, f.repos)
	default:
		_, err = fmt.Fprintf(f.w, "\n%d change(s) planned in %d repositories. Re-run with --apply to write them (`fleetlint fix` in a repository shows the diff).\n", f.changes, f.repos)
	}
	return err
}

// visibilityLookup asks the forge for each repository whose visibility is
// unknown and keeps the failures for one note instead of one per repository.
type visibilityLookup struct {
	hidden  map[string]int // host -> repositories it did not show
	unnamed map[string]int // Gitea host -> repositories not asked, the user did not name it
	failed  []string
}

func (v *visibilityLookup) lookup(ctx context.Context, r *repo.Repo, f facts.Facts) facts.Visibility {
	vis, host, err := lookupVisibility(ctx, r, f)
	switch {
	case errors.Is(err, forge.ErrUnknownHost):
		if v.unnamed == nil {
			v.unnamed = map[string]int{}
		}
		v.unnamed[host]++
	case errors.Is(err, forge.ErrNotFound):
		if v.hidden == nil {
			v.hidden = map[string]int{}
		}
		v.hidden[host]++
	case err != nil:
		v.failed = append(v.failed, fmt.Sprintf("%s (%v)", r.Name(), err))
	}
	return vis
}

func (v *visibilityLookup) note(w io.Writer) {
	for _, host := range slices.Sorted(maps.Keys(v.hidden)) {
		hint := "GITEA_TOKEN"
		if host == "github.com" {
			hint = "GITHUB_TOKEN, e.g. GITHUB_TOKEN=$(gh auth token)"
		}
		fmt.Fprintf(w, "visibility: %s does not show %s without a token; left unknown (set %s, or visibility in the fleet file)\n", host, plural(v.hidden[host], "repository", "repositories"), hint) //nolint:errcheck // a note on stderr
	}
	for _, host := range slices.Sorted(maps.Keys(v.unnamed)) {
		fmt.Fprintf(w, "visibility: %s was not asked about %s; left unknown (set GITEA_HOST=%s to let fleetlint ask it, and GITEA_TOKEN for private repositories)\n", host, plural(v.unnamed[host], "repository", "repositories"), host) //nolint:errcheck // a note on stderr
	}
	if len(v.failed) > 0 {
		fmt.Fprintf(w, "visibility: lookup failed for %s; left unknown\n", strings.Join(v.failed, ", ")) //nolint:errcheck // a note on stderr
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
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

// addWalked appends the repositories found under each --discover root.
func addWalked(spec *fleet.Spec, roots, exclude []string) error {
	if len(exclude) > 0 && len(roots) == 0 {
		return errors.New("--exclude needs --discover")
	}
	names := map[string]bool{}
	listed := map[string]int{} // path -> index of its fleet file entry
	for i, e := range spec.Repos {
		names[e.Name] = true
		if e.Path != "" {
			listed[absPath(e.Path)] = i
		}
	}
	for _, root := range roots {
		found, err := fleet.Discover(root, fleet.DiscoverDepth, exclude)
		if err != nil {
			return fmt.Errorf("--discover %s: %w", root, err)
		}
		for _, e := range found.Repos {
			if err := addFound(spec, names, listed, e); err != nil {
				return fmt.Errorf("--discover %s: %w", root, err)
			}
		}
		for _, u := range found.Unversioned {
			spec.Unversioned = append(spec.Unversioned, filepath.Join(root, u))
		}
	}
	return nil
}

// addFound appends a discovered repository; a fleet file entry for the
// same path adjusts it instead and only takes its group.
func addFound(spec *fleet.Spec, names map[string]bool, listed map[string]int, e fleet.Entry) error {
	if i, ok := listed[absPath(e.Path)]; ok {
		if spec.Repos[i].Group == "" {
			spec.Repos[i].Group = e.Group
		}
		return nil
	}
	if names[e.Name] {
		return fmt.Errorf("repository name %q is already in the fleet", e.Name)
	}
	names[e.Name] = true
	spec.Repos = append(spec.Repos, e)
	return nil
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
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
		return errors.New("no repositories: pass paths, --repos fleet.yaml, --discover <dir> or --from github:<owner>")
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

// sameHost reports whether a clone URL the forge returned points at the
// forge itself (for GitHub Enterprise, the host of GITHUB_API_URL), so its
// token never goes to a host the forge merely named.
func sameHost(cloneURL string, src forge.Source) bool {
	u, err := url.Parse(cloneURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	if strings.EqualFold(u.Hostname(), src.Host) {
		return true
	}
	api, err := url.Parse(os.Getenv("GITHUB_API_URL"))
	return src.Kind == "github" && err == nil && api.Hostname() != "" && strings.EqualFold(u.Hostname(), api.Hostname())
}

// discover lists one forge source as fleet entries, without archived
// repositories and forks.
func discover(ctx context.Context, source string) ([]fleet.Entry, error) {
	src, err := forge.ParseSource(source)
	if err != nil {
		return nil, err
	}
	client := forge.Client{Hosts: []string{src.Host}}
	repos, err := client.List(ctx, src)
	if err != nil {
		return nil, err
	}
	entries := make([]fleet.Entry, 0, len(repos))
	for _, fr := range repos {
		if fr.Archived || fr.Fork {
			continue
		}
		e := fleet.Entry{Name: fr.Name, URL: fr.CloneURL, Visibility: string(facts.VisibilityPublic)}
		if sameHost(fr.CloneURL, src) {
			e.Token = client.Token(src)
		}
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
