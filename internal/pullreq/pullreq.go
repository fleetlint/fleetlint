// Package pullreq opens or updates the pull request for a pushed branch:
// through the forge's API when a token for it is set, else with the
// forge's CLI (gh, tea or glab), which brings its own login.
package pullreq

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/fleetlint/fleetlint/internal/forge"
	"github.com/fleetlint/fleetlint/internal/proc"
)

// Open opens the pull request from pr.Head in the checkout at dir, whose
// remote is on a forge of the given kind. An empty pr.Base is the default
// branch. Without a token, the CLI opens a new one only: one already open
// for the branch keeps its description.
func Open(ctx context.Context, c forge.Client, dir, kind, remote string, pr forge.PullRequest) (forge.Opened, error) {
	if c.CanOpen(kind, remote) {
		if pr.Base == "" {
			base, err := c.DefaultBranch(ctx, kind, remote)
			if err != nil {
				return forge.Opened{}, err
			}
			pr.Base = base
		}
		return c.OpenPullRequest(ctx, kind, remote, pr)
	}
	for _, cli := range clis(kind) {
		if _, err := exec.LookPath(cli); err != nil {
			continue
		}
		out, err := run(ctx, dir, cli, cliArgs(cli, pr)...)
		return forge.Opened{URL: lastURL(out)}, err
	}
	return forge.Opened{}, errors.New("no token for the forge (GITHUB_TOKEN, GITEA_TOKEN, GITLAB_TOKEN) and neither gh, tea nor glab is installed")
}

// clis are the forge CLIs to try, the remote's own first.
func clis(kind string) []string {
	switch kind {
	case "gitea":
		return []string{"tea", "gh", "glab"}
	case "gitlab":
		return []string{"glab", "gh", "tea"}
	}
	return []string{"gh", "tea", "glab"}
}

func cliArgs(cli string, pr forge.PullRequest) []string {
	var args []string
	switch cli {
	case "gh":
		args = []string{"pr", "create", "--title", pr.Title, "--body", pr.Body, "--head", pr.Head}
	case "tea":
		args = []string{"pr", "create", "--title", pr.Title, "--description", pr.Body, "--head", pr.Head}
	case "glab":
		args = []string{"mr", "create", "--yes", "--title", pr.Title, "--description", pr.Body, "--source-branch", pr.Head}
	}
	if pr.Base == "" {
		return args
	}
	switch cli {
	case "glab":
		return append(args, "--target-branch", pr.Base)
	default:
		return append(args, "--base", pr.Base)
	}
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := proc.Command(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// lastURL is the last https URL a CLI printed: the pull request it opened.
func lastURL(out string) string {
	fields := strings.Fields(out)
	for i := len(fields) - 1; i >= 0; i-- {
		if strings.HasPrefix(fields[i], "https://") {
			return fields[i]
		}
	}
	return ""
}
