package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/cli"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

const onboardingBranch = "fleetlint/onboarding"

// onboardingRepo is a committed repository with a bare origin to push to.
func onboardingRepo(t *testing.T) (dir, remote string) {
	t.Helper()
	remote = t.TempDir()
	testutil.Git(t, remote, "init", "-q", "--bare")
	testutil.NoBackgroundGit(t, remote)
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", "README.md": "# x\n"},
		"remote.origin.url", remote, "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	return r.Root, remote
}

// toolbox puts only git and the given fake forge CLIs on PATH and isolates git
// from the user's configuration; each fake records its arguments one per line
// in <name>.log and exits with status. Not for parallel tests: it sets the environment.
func toolbox(t *testing.T, fakes map[string]int) string {
	t.Helper()
	for _, kv := range testutil.GitEnv()[len(os.Environ()):] {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	bin := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	for name, status := range fakes {
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + filepath.Join(bin, name+".log") + "\"\necho boom\nexit " + strconv.Itoa(status) + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return bin
}

func TestInitPROpensAPullRequest(t *testing.T) { // not parallel: toolbox sets the environment
	bin := toolbox(t, map[string]int{"gh": 0})
	dir, remote := onboardingRepo(t)

	code, out, errOut := runCLI(t, dir, "init", "--pr")
	if code != cli.ExitOK || !strings.Contains(out, "pushed "+onboardingBranch+" and opened a pull request") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	args, err := os.ReadFile(filepath.Join(bin, "gh.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pr\ncreate\n", "--title\nAdd fleetlint configuration\n", "--head\n" + onboardingBranch + "\n", "fleetlint init --pr", "| | Rule | Finding | Fix |"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("gh was not called with %q:\n%s", want, args)
		}
	}
	if testutil.Git(t, remote, "rev-parse", "--verify", "refs/heads/"+onboardingBranch) == "" {
		t.Error("the branch must be pushed to origin")
	}
	if got := testutil.Git(t, dir, "branch", "--show-current"); got != onboardingBranch {
		t.Errorf("current branch %q, want %s", got, onboardingBranch)
	}
	if subject := testutil.Git(t, dir, "log", "-1", "--format=%s%n%an"); subject != "chore: add fleetlint configuration\nt" {
		t.Errorf("commit subject and author: %q", subject)
	}
	if testutil.Git(t, dir, "ls-files", ".fleetlint.yaml") != ".fleetlint.yaml" {
		t.Error("the configuration must be committed")
	}
}

func TestInitPRFallsBackWithoutAForgeCLI(t *testing.T) { // not parallel: toolbox sets the environment
	cases := []struct {
		name  string
		fakes map[string]int
		want  string
	}{
		{"tea is used when gh is missing", map[string]int{"tea": 0}, ""},
		{"a failing gh leaves the branch", map[string]int{"gh": 1}, "branch fleetlint/onboarding is pushed to origin and checked out here, but the pull request could not be opened (gh: exit status 1: boom)"},
		{"no forge cli", nil, "is pushed to origin and checked out here, but the pull request could not be opened (no token for the forge (--token, FLEETLINT_TOKEN, GITHUB_TOKEN, GITEA_TOKEN, GITLAB_TOKEN) and neither gh, tea nor glab is installed)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin := toolbox(t, c.fakes)
			dir, remote := onboardingRepo(t)
			code, out, errOut := runCLI(t, dir, "init", "--pr")
			switch {
			case c.want == "" && (code != cli.ExitOK || !strings.Contains(out, "opened a pull request")):
				t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
			case c.want != "" && (code != cli.ExitInternal || !strings.Contains(errOut, c.want)):
				t.Fatalf("a pull request that could not be opened fails the run: exit %d\n%s\n%s", code, out, errOut)
			}
			if testutil.Git(t, remote, "rev-parse", "--verify", "refs/heads/"+onboardingBranch) == "" {
				t.Error("the branch is pushed before the PR is attempted")
			}
			if _, ok := c.fakes["tea"]; ok {
				args, _ := os.ReadFile(filepath.Join(bin, "tea.log"))
				if !strings.Contains(string(args), "--description\n") {
					t.Errorf("tea takes --description:\n%s", args)
				}
			}
		})
	}
}

// An onboarding branch already on origin is fleetlint's earlier proposal,
// perhaps edited in its pull request: a later run, from a fresh clone as in
// CI, leaves it alone and succeeds.
func TestInitPRLeavesAnEarlierOnboardingAlone(t *testing.T) { // not parallel: toolbox sets the environment
	bin := toolbox(t, map[string]int{"gh": 0})
	dir, remote := onboardingRepo(t)
	testutil.Git(t, dir, "push", "-q", "origin", "HEAD:refs/heads/"+onboardingBranch)
	before := testutil.Git(t, remote, "rev-parse", onboardingBranch)

	code, out, errOut := runCLI(t, dir, "init", "--pr")
	if code != cli.ExitOK || !strings.Contains(out, "already on origin") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if testutil.Git(t, remote, "rev-parse", onboardingBranch) != before {
		t.Error("the branch on origin is not overwritten")
	}
	if _, err := os.Stat(filepath.Join(bin, "gh.log")); err == nil {
		t.Error("no pull request is opened")
	}
}

// A push that fails is named as such and fails the run, so it is clear the
// branch exists only locally and still has to be pushed.
func TestInitPRSaysWhenThePushFailed(t *testing.T) { // not parallel: toolbox sets the environment
	toolbox(t, map[string]int{"gh": 0})
	dir, remote := onboardingRepo(t)
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, dir, "init", "--pr")
	if code != cli.ExitInternal || !strings.Contains(errOut, "pushing it failed") || !strings.Contains(errOut, "git push -u origin "+onboardingBranch) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(out, "opened a pull request") {
		t.Error("no pull request without a pushed branch")
	}
}

func TestInitPRRefusesUnsafeStates(t *testing.T) {
	t.Parallel()
	t.Run("not a git repository", func(t *testing.T) {
		t.Parallel()
		dir := testutil.Fixture(t, map[string]string{"go.mod": "module x\n"}).Root
		if code, _, errOut := runCLI(t, dir, "init", "--pr"); code != cli.ExitConfig || !strings.Contains(errOut, "needs a git repository") {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	})
	t.Run("dirty tree", func(t *testing.T) {
		t.Parallel()
		dir, _ := onboardingRepo(t)
		testutil.WriteFiles(t, dir, map[string]string{"README.md": "changed\n"})
		if code, _, errOut := runCLI(t, dir, "init", "--pr"); code != cli.ExitConfig || !strings.Contains(errOut, "clean working tree") {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	})
	t.Run("branch exists", func(t *testing.T) {
		t.Parallel()
		dir, _ := onboardingRepo(t)
		testutil.Git(t, dir, "branch", onboardingBranch)
		if code, _, errOut := runCLI(t, dir, "init", "--pr"); code != cli.ExitConfig || !strings.Contains(errOut, "already exists") {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	})
}

// publishedRepo is onboardingRepo with main pushed, origin/HEAD set and
// a tracked junk file to fix.
func publishedRepo(t *testing.T) (dir, remote string) {
	t.Helper()
	dir, remote = onboardingRepo(t)
	testutil.WriteFiles(t, dir, map[string]string{".DS_Store": "x", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n"})
	testutil.Git(t, dir, "add", "-A")
	testutil.Git(t, dir, "commit", "-q", "-m", "junk")
	testutil.Git(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")
	testutil.Git(t, dir, "fetch", "-q", "origin")
	testutil.Git(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return dir, remote
}

// Without a token the forge's CLI opens the pull request; glab takes
// GitLab's flags.
func TestFixPROpensAMergeRequestWithGlab(t *testing.T) { // not parallel: toolbox sets the environment
	bin := toolbox(t, map[string]int{"glab": 0})
	dir, remote := publishedRepo(t)
	code, out, errOut := runCLI(t, dir, "fix", "--apply", "--pr")
	if code != cli.ExitOK || !strings.Contains(out, "change(s) on fleetlint/fixes; opened pull request") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "glab.log"))
	for _, want := range []string{"mr\ncreate\n--yes\n", "--source-branch\nfleetlint/fixes\n", "--target-branch\nmain\n", "--description\n## Changes\n"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("glab was not called with %q:\n%s", want, args)
		}
	}
	if testutil.Git(t, remote, "rev-parse", "--verify", "refs/heads/fleetlint/fixes") == "" {
		t.Error("the fixes branch is pushed")
	}
	if got := testutil.Git(t, dir, "branch", "--show-current"); got != "main" && got != "master" {
		t.Errorf("the checkout stays on its branch: %q", got)
	}
	if code, _, _ := runCLI(t, dir, "fix", "--pr"); code != cli.ExitConfig {
		t.Errorf("--pr without --apply is a usage error: %d", code)
	}
}

// fleet --fix --apply --pr proposes per repository and notes it in the report.
func TestFleetFixPR(t *testing.T) { // not parallel: toolbox sets the environment
	toolbox(t, map[string]int{"gh": 0})
	dir, remote := publishedRepo(t)
	out := filepath.Join(t.TempDir(), "report")
	code, stdout, errOut := runCLI(t, t.TempDir(), "fleet", "--fix", "--apply", "--pr", "--out", out, dir)
	if code != cli.ExitOK || !strings.Contains(stdout, "fix(es) proposed on fleetlint/fixes: opened pull request") || !strings.Contains(stdout, "in 1 pull requests") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, errOut)
	}
	if testutil.Git(t, remote, "rev-parse", "--verify", "refs/heads/fleetlint/fixes") == "" {
		t.Error("the fixes branch is pushed")
	}
	if md, _ := os.ReadFile(filepath.Join(out, "fleet.md")); !strings.Contains(string(md), "proposed on fleetlint/fixes") {
		t.Errorf("the report notes the pull request:\n%s", md)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
		t.Error("no working tree is written")
	}
	if code, _, _ := runCLI(t, t.TempDir(), "fleet", "--fix", "--pr", dir); code != cli.ExitConfig {
		t.Errorf("--pr without --apply is a usage error: %d", code)
	}
}
