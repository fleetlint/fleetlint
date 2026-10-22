// Package fixpr is `fleetlint fix --apply --pr`: the mechanical fixes,
// made in a temporary worktree of the default branch, committed on the
// branch fleetlint/fixes, pushed, and offered as a pull request whose
// description lists the changes and the report. The checkout it starts
// from is not touched, so a protected main and work in progress are both
// fine. fleetlint owns the branch: every run rebuilds it from the default
// branch and force-pushes it, and the open pull request is updated.
package fixpr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/fixplan"
	"github.com/fleetlint/fleetlint/internal/forge"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/proc"
	"github.com/fleetlint/fleetlint/internal/pullreq"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/report"
)

// Branch is the branch the fixes are committed on.
const Branch = "fleetlint/fixes"

// Title is the pull request's title.
const Title = "Apply fleetlint fixes"

// Options tune a run.
type Options struct {
	// Rules limits the fixes to these rules; empty fixes every rule.
	Rules []string
	// Require lists catalogs the configuration must extend, as for check.
	Require []string
	// Version names the binary in the description.
	Version string
	// GitEnv is added to git's environment, such as the credentials a
	// fleet clone was made with.
	GitEnv []string
	// Client reaches the forge; the zero value reads tokens from the
	// environment.
	Client forge.Client
	// Open opens the pull request; nil is pullreq.Open. Tests replace it.
	Open func(ctx context.Context, c forge.Client, dir, kind, remote string, pr forge.PullRequest) (forge.Opened, error)
}

// Result says what a run did.
type Result struct {
	// Changes is how many changes the commit holds; 0 means nothing was
	// pushed or opened.
	Changes int
	Base    string
	forge.Opened
	// Unchanged is true when the branch already held these fixes, so
	// nothing was pushed.
	Unchanged bool
}

// Open plans the fixes of the default branch's configuration and, when
// there are any, commits them on Branch and opens or updates the pull
// request.
func Open(ctx context.Context, r *repo.Repo, opts Options) (Result, error) {
	remote := r.RemoteURL() //nolint:contextcheck // git runs under the repository's own bounded context
	if !r.HasGit() || remote == "" {
		return Result{}, errors.New("--pr needs a git repository with a remote named origin")
	}
	kind := forgeKind(r, remote)
	base, err := defaultBranch(ctx, r, opts, kind, remote)
	if err != nil {
		return Result{}, err
	}
	if err := gitIn(ctx, r.Root, opts.GitEnv, "fetch", "-q", "origin", "+refs/heads/"+base+":refs/remotes/origin/"+base); err != nil {
		return Result{}, err
	}
	// The branch from an earlier run, if any, for the lease and the no-op check.
	_ = gitIn(ctx, r.Root, opts.GitEnv, "fetch", "-q", "origin", "+refs/heads/"+Branch+":refs/remotes/origin/"+Branch)
	dir, err := os.MkdirTemp("", "fleetlint-fix-")
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = gitIn(context.WithoutCancel(ctx), r.Root, opts.GitEnv, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
		_ = gitIn(context.WithoutCancel(ctx), r.Root, opts.GitEnv, "worktree", "prune")
	}()
	if err := gitIn(ctx, r.Root, opts.GitEnv, "worktree", "add", "-q", "--detach", dir, "refs/remotes/origin/"+base); err != nil {
		return Result{}, err
	}
	wt, err := repo.Open(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	run, changes, err := plan(ctx, wt, opts)
	if err != nil || len(changes) == 0 {
		return Result{Base: base}, err
	}
	if err := fix.Apply(changes); err != nil {
		return Result{}, err
	}
	res := Result{Changes: len(changes), Base: base}
	if err := commit(ctx, dir, opts.GitEnv, run); err != nil {
		return Result{}, err
	}
	if same := gitIn(ctx, dir, opts.GitEnv, "diff", "--quiet", "refs/remotes/origin/"+Branch, "HEAD"); same == nil {
		res.Unchanged = true
	} else if err := gitIn(ctx, dir, opts.GitEnv, "push", "-q", "--force-with-lease="+Branch, "origin", "HEAD:refs/heads/"+Branch); err != nil {
		return Result{}, err
	}
	open := opts.Open
	if open == nil {
		open = pullreq.Open
	}
	res.Opened, err = open(ctx, opts.Client, dir, kind, remote, forge.PullRequest{Head: Branch, Base: base, Title: Title, Body: body(run, changes, opts.Version)})
	if err != nil {
		return res, fmt.Errorf("branch %s is pushed, but the pull request could not be opened: %w", Branch, err)
	}
	return res, nil
}

// forgeKind is the forge the remote is on: the configured one, else detected.
func forgeKind(r *repo.Repo, remote string) string {
	if eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}}); err == nil && eff.File.Facts.Forge != "" {
		return eff.File.Facts.Forge
	}
	kind, _ := facts.ForgeOf(remote)
	return kind
}

// defaultBranch asks the forge, then the remote's HEAD as git knows it.
func defaultBranch(ctx context.Context, r *repo.Repo, opts Options, kind, remote string) (string, error) {
	if opts.Client.CanOpen(kind, remote) {
		if b, err := opts.Client.DefaultBranch(ctx, kind, remote); err == nil {
			return b, nil
		}
	}
	if head, err := r.Git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil { //nolint:contextcheck // git runs under the repository's own bounded context
		return strings.TrimPrefix(head, "origin/"), nil
	}
	out, err := gitOut(ctx, r.Root, opts.GitEnv, "ls-remote", "--symref", "origin", "HEAD")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
				return strings.Fields(ref)[0], nil
			}
		}
	}
	return "", errors.New("cannot tell the default branch: set origin/HEAD (git remote set-head origin --auto) or a forge token")
}

// plan checks the worktree with its own configuration and plans its fixes.
func plan(ctx context.Context, wt *repo.Repo, opts Options) (*engine.Run, []fix.Change, error) {
	eff, err := config.Load(wt.Root, config.Options{Loader: catalog.RemoteLoader(ctx)})
	if err == nil {
		err = eff.Requires(opts.Require)
	}
	if err != nil {
		return nil, nil, err
	}
	run, err := engine.Evaluate(ctx, wt, eff)
	if err != nil {
		return nil, nil, err
	}
	fixplan.MarkFixable(wt, run)
	changes, _, err := fixplan.PlanEach(wt, run, opts.Rules, func(string, error) error { return nil })
	return run, changes, err
}

// commit stages and commits the fixes as whoever runs fleetlint, with the
// repository's hooks off: the repository may be a clone of someone else's.
func commit(ctx context.Context, dir string, env []string, run *engine.Run) error {
	msg := "chore: apply fleetlint fixes\n\nFixes " + strings.Join(fixedRules(run), ", ") + ".\n"
	for _, args := range [][]string{
		{"checkout", "-q", "-B", Branch},
		{"add", "-A"},
		{"commit", "-q", "--no-verify", "-m", msg},
	} {
		if err := gitIn(ctx, dir, env, args...); err != nil {
			return err
		}
	}
	return nil
}

// fixedRules are the failing rules whose fix is mechanical, sorted.
func fixedRules(run *engine.Run) []string {
	seen := map[string]bool{}
	var out []string
	for _, res := range run.Results {
		if res.Status == model.StatusFail && res.Fixable && !seen[res.Rule.ID] {
			seen[res.Rule.ID] = true
			out = append(out, res.Rule.ID)
		}
	}
	sort.Strings(out)
	return out
}

// body is the pull request's description: the changes, then the report
// of the default branch as it is.
func body(run *engine.Run, changes []fix.Change, version string) string {
	var b bytes.Buffer
	b.WriteString("## Changes\n\n")
	for _, c := range changes {
		line := "- " + c.Kind + " `" + c.Path + "`"
		if c.From != "" {
			line += " (template from catalog " + c.From + ")"
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nRules fixed: `" + strings.Join(fixedRules(run), "`, `") + "`\n\n## Report before the fixes\n\n")
	_ = report.Write(&b, run, report.FormatMarkdown, report.Options{Version: version, Catalog: catalog.ModuleVersion()}) // a bytes.Buffer write cannot fail
	b.WriteString("\n---\nOpened by `fleetlint fix --apply --pr`. The next run rebuilds `" + Branch + "` from the default branch and force-pushes it, so commit your own changes on another branch.\n")
	return b.String()
}

// gitIn runs git in dir with hooks off and the extra environment.
func gitIn(ctx context.Context, dir string, env []string, args ...string) error {
	_, err := gitOut(ctx, dir, env, args...)
	return err
}

func gitOut(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := proc.Command(ctx, "git", append([]string{"-C", dir, "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Env = proc.GitEnv(env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
