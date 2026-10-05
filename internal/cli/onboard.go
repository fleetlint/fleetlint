package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/report"
)

const onboardingBranch = "fleetlint/onboarding"

// openOnboardingPR commits the new config on a branch and opens a pull
// request whose body is the Markdown report, the way Renovate onboards. The
// commit is authored by whoever runs it; nothing identifies the tool as an
// author. If no forge CLI is available, the branch and commit remain and the
// user is told how to open the PR.
func openOnboardingPR(cmd *cobra.Command, r *repo.Repo, run *engine.Run) error {
	if !r.HasGit() {
		return &configError{errors.New("--pr needs a git repository")}
	}
	status, err := r.Git("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if status != "" {
		return &configError{errors.New("--pr needs a clean working tree (uncommitted changes present)")}
	}
	if _, err := r.Git("rev-parse", "-q", "--verify", "refs/heads/"+onboardingBranch); err == nil {
		return &configError{fmt.Errorf("branch %s already exists; delete it or finish the earlier onboarding first", onboardingBranch)}
	}
	for _, args := range [][]string{
		{"checkout", "-q", "-b", onboardingBranch},
		{"add", config.FileName},
		{"commit", "-q", "-m", "chore: add fleetlint configuration\n\nAdds .fleetlint.yaml with the discovered facts. See the pull request for the current findings."},
	} {
		if _, err := r.Git(args...); err != nil {
			return err
		}
	}
	var body bytes.Buffer
	if err := report.Write(&body, run, report.FormatMarkdown, report.Options{}); err != nil {
		return err
	}
	body.WriteString("\n---\nOpened by `fleetlint init --pr`. Merge to adopt the configuration; findings are informational until the hook or CI enforces them.\n")
	out := cmd.OutOrStdout()
	if err := createPR(cmd.Context(), r.Root, body.String()); err != nil {
		_, werr := fmt.Fprintf(out, "branch %s with the commit is ready; could not open the pull request automatically (%v).\nPush it and open the PR with the contents of the report as description.\n", onboardingBranch, err)
		return werr
	}
	_, err = fmt.Fprintf(out, "opened a pull request from %s\n", onboardingBranch)
	return err
}

// createPR pushes the branch and opens the PR with gh (GitHub) or tea (Gitea),
// whichever is installed.
func createPR(ctx context.Context, dir, body string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	push := exec.CommandContext(ctx, "git", "-C", dir, "push", "-q", "-u", "origin", onboardingBranch) //nolint:gosec // fixed git arguments; dir is the repository root
	push.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := push.CombinedOutput(); err != nil {
		return fmt.Errorf("push: %w: %s", err, strings.TrimSpace(string(out)))
	}
	title := "Add fleetlint configuration"
	switch {
	case lookPath("gh"):
		return runIn(ctx, dir, "gh", "pr", "create", "--title", title, "--body", body, "--head", onboardingBranch)
	case lookPath("tea"):
		return runIn(ctx, dir, "tea", "pr", "create", "--title", title, "--description", body, "--head", onboardingBranch)
	}
	return errors.New("neither gh nor tea is installed")
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runIn(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // callers pass the fixed names gh or tea
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
