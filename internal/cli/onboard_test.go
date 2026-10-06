package cli_test

import (
	"context"
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
	gitIn(t, remote, "init", "-q", "--bare")
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", "README.md": "# x\n"},
		"remote.origin.url", remote, "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	return r.Root, remote
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
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
	if code != cli.ExitOK || !strings.Contains(out, "opened a pull request from "+onboardingBranch) {
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
	if gitIn(t, remote, "rev-parse", "--verify", "refs/heads/"+onboardingBranch) == "" {
		t.Error("the branch must be pushed to origin")
	}
	if got := gitIn(t, dir, "branch", "--show-current"); got != onboardingBranch {
		t.Errorf("current branch %q, want %s", got, onboardingBranch)
	}
	if subject := gitIn(t, dir, "log", "-1", "--format=%s%n%an"); subject != "chore: add fleetlint configuration\nt" {
		t.Errorf("commit subject and author: %q", subject)
	}
	if gitIn(t, dir, "ls-files", ".fleetlint.yaml") != ".fleetlint.yaml" {
		t.Error("the configuration must be committed")
	}
}

func TestInitPRFallsBackWithoutAForgeCLI(t *testing.T) { // not parallel: toolbox sets the environment
	cases := []struct {
		name  string
		fakes map[string]int
		want  string
	}{
		{"tea is used when gh is missing", map[string]int{"tea": 0}, "opened a pull request"},
		{"a failing gh leaves the branch", map[string]int{"gh": 1}, "could not open the pull request automatically (gh: exit status 1: boom)"},
		{"no forge cli", nil, "could not open the pull request automatically (neither gh nor tea is installed)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin := toolbox(t, c.fakes)
			dir, remote := onboardingRepo(t)
			code, out, errOut := runCLI(t, dir, "init", "--pr")
			if code != cli.ExitOK || !strings.Contains(out, c.want) {
				t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
			}
			if gitIn(t, remote, "rev-parse", "--verify", "refs/heads/"+onboardingBranch) == "" {
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
		gitIn(t, dir, "branch", onboardingBranch)
		if code, _, errOut := runCLI(t, dir, "init", "--pr"); code != cli.ExitConfig || !strings.Contains(errOut, "already exists") {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	})
}
