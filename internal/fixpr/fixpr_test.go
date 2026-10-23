package fixpr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/fixpr"
	"github.com/fleetlint/fleetlint/internal/forge"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// published is a repository whose main is on a bare origin, with a failing
// pre-commit hook and work in progress in its checkout.
func published(t *testing.T) (dir, remote string) {
	t.Helper()
	remote = t.TempDir()
	testutil.Git(t, remote, "init", "-q", "--bare")
	testutil.NoBackgroundGit(t, remote)
	r := testutil.GitFixture(t, map[string]string{
		"go.mod": "module x\n", ".DS_Store": "x",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n",
	}, "remote.origin.url", remote, "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	testutil.Git(t, r.Root, "push", "-q", "origin", "HEAD:refs/heads/main")
	testutil.Git(t, r.Root, "fetch", "-q", "origin")
	testutil.Git(t, r.Root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	testutil.WriteFiles(t, r.Root, map[string]string{".git/hooks/pre-commit": "#!/bin/sh\nexit 1\n", "go.mod": "module x // wip\n"})
	if err := os.Chmod(filepath.Join(r.Root, ".git/hooks/pre-commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	return r.Root, remote
}

func TestOpenProposesTheFixesOfTheDefaultBranch(t *testing.T) {
	t.Parallel()
	dir, remote := published(t)
	var got []forge.PullRequest
	opts := fixpr.Options{
		GitEnv: testutil.GitEnv()[len(os.Environ()):],
		Open: func(_ context.Context, _ forge.Client, _, _, _ string, pr forge.PullRequest) (forge.Opened, error) {
			got = append(got, pr)
			return forge.Opened{URL: "https://forge.example/pr/1", Updated: len(got) > 1}, nil
		},
	}
	r, err := repo.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := fixpr.Open(context.Background(), r, opts)
	if err != nil || res.Changes == 0 || res.Base != "main" || res.URL != "https://forge.example/pr/1" || res.Unchanged {
		t.Fatalf("first run: %+v %v", res, err)
	}
	pr := got[0]
	if pr.Head != fixpr.Branch || pr.Base != "main" || pr.Title != fixpr.Title || !strings.Contains(pr.Body, "untrack `.DS_Store`") || !strings.Contains(pr.Body, "repo/no-tracked-junk") || !strings.Contains(pr.Body, "## Report before the fixes") {
		t.Errorf("pull request: %+v", pr)
	}
	if files := testutil.Git(t, remote, "ls-tree", "-r", "--name-only", fixpr.Branch); strings.Contains(files, ".DS_Store") || !strings.Contains(files, ".gitignore") {
		t.Errorf("the branch holds the fixes (hooks off): %s", files)
	}
	if msg := testutil.Git(t, remote, "log", "-1", "--format=%s", fixpr.Branch); msg != "chore: apply fleetlint fixes" {
		t.Errorf("commit: %q", msg)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "go.mod")); string(b) != "module x // wip\n" || testutil.Git(t, dir, "branch", "--show-current") != "main" {
		t.Error("the checkout and its work in progress are untouched")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
		t.Error("nothing is written into the checkout")
	}
	if wts := testutil.Git(t, dir, "worktree", "list"); strings.Count(wts, "\n") != 0 {
		t.Errorf("the temporary worktree is gone: %s", wts)
	}

	again, err := fixpr.Open(context.Background(), r, opts)
	if err != nil || !again.Unchanged || !again.Updated || len(got) != 2 {
		t.Fatalf("a second run finds the branch as it is and updates the pull request: %+v %v", again, err)
	}

	// The pull request was merged and its branch deleted on the forge; the
	// checkout still has the remote-tracking ref, as a CI runner's cached clone does.
	testutil.Git(t, remote, "branch", "-D", fixpr.Branch)
	after, err := fixpr.Open(context.Background(), r, opts)
	if err != nil || after.Unchanged || testutil.Git(t, remote, "rev-parse", "--verify", "refs/heads/"+fixpr.Branch) == "" {
		t.Fatalf("a branch deleted on the forge is pushed again: %+v %v", after, err)
	}
}

func TestOpenWithNothingToFix(t *testing.T) {
	t.Parallel()
	dir, _ := published(t)
	opened := false
	r, _ := repo.Open(context.Background(), dir)
	res, err := fixpr.Open(context.Background(), r, fixpr.Options{
		Rules:  []string{"repo/editorconfig"},
		GitEnv: testutil.GitEnv()[len(os.Environ()):],
		Open: func(context.Context, forge.Client, string, string, string, forge.PullRequest) (forge.Opened, error) {
			opened = true
			return forge.Opened{}, nil
		},
	})
	if err != nil || res.Changes != 0 || opened {
		t.Fatalf("a rule that is not loaded fixes nothing and opens nothing: %+v %v", res, err)
	}
}
