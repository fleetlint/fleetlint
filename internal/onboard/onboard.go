// Package onboard opens the onboarding pull request of `fleetlint init --pr`:
// a branch with the new configuration, pushed, and a pull request opened
// through the forge's API or CLI whose description is the report.
package onboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/proc"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/forge"
	"github.com/fleetlint/fleetlint/internal/pullreq"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/report"
)

// Branch is the branch the configuration is committed on.
const Branch = "fleetlint/onboarding"

// ErrNotReady wraps the reasons the repository cannot be onboarded now: no
// git, uncommitted changes, an onboarding branch left from before.
var ErrNotReady = errors.New("cannot open the onboarding pull request")

// Open commits the new config on a branch and opens a pull request whose
// body is the Markdown report, the way Renovate onboards. The commit is
// authored by whoever runs it; nothing identifies the tool as an author. If
// no forge CLI is available, the branch and commit remain and the user is
// told on out how to open the PR.
func Open(ctx context.Context, out io.Writer, r *repo.Repo, run *engine.Run, version string) error {
	if !r.HasGit() {
		return fmt.Errorf("%w: --pr needs a git repository", ErrNotReady)
	}
	status, err := r.Git("status", "--porcelain", "--untracked-files=no") //nolint:contextcheck // git runs under the repository's own bounded context
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("%w: --pr needs a clean working tree (uncommitted changes present)", ErrNotReady)
	}
	if _, err := r.Git("rev-parse", "-q", "--verify", "refs/heads/"+Branch); err == nil { //nolint:contextcheck // git runs under the repository's own bounded context
		return fmt.Errorf("%w: branch %s already exists; delete it or finish the earlier onboarding first", ErrNotReady, Branch)
	}
	for _, args := range [][]string{
		{"checkout", "-q", "-b", Branch},
		{"add", config.FileName},
		{"commit", "-q", "-m", "chore: add fleetlint configuration\n\nAdds .fleetlint.yaml with the discovered facts. See the pull request for the current findings."},
	} {
		if _, err := r.Git(args...); err != nil { //nolint:contextcheck // git runs under the repository's own bounded context
			return err
		}
	}
	var body bytes.Buffer
	if err := report.Write(&body, run, report.FormatMarkdown, report.Options{Version: version, Catalog: catalog.ModuleVersion()}); err != nil {
		return err
	}
	body.WriteString("\n---\nOpened by `fleetlint init --pr`. Merge to adopt the configuration; findings are informational until the hook or CI enforces them.\n")
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := push(ctx, r.Root); err != nil {
		_, werr := fmt.Fprintf(out, "the commit is on branch %s, which is checked out, but pushing it failed (%v).\nPush it with `git push -u origin %s`, then open a pull request with the report as its description.\n", Branch, err, Branch)
		return werr
	}
	if _, err := pullreq.Open(ctx, forge.Client{}, r.Root, run.Facts.Forge, r.RemoteURL(), forge.PullRequest{Head: Branch, Title: "Add fleetlint configuration", Body: body.String()}); err != nil { //nolint:contextcheck // the remote is read under the repository's own bounded context
		_, werr := fmt.Fprintf(out, "branch %s is pushed to origin and checked out here, but the pull request could not be opened (%v).\nOpen it on the forge from %s, with the report as its description.\n", Branch, err, Branch)
		return werr
	}
	_, err = fmt.Fprintf(out, "pushed %s and opened a pull request from it\n", Branch)
	return err
}

func push(ctx context.Context, dir string) error {
	cmd := proc.Command(ctx, "git", "-C", dir, "push", "-q", "-u", "origin", Branch)
	cmd.Env = proc.GitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
