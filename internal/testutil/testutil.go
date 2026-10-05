// Package testutil builds fixture repositories for tests. A fixture is a
// temp directory with files and, optionally, a real git repository so that
// tracked-file rules see what git sees.
package testutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// Fixture writes files into a temp dir and opens it as a Repo.
func Fixture(t *testing.T, files map[string]string) *repo.Repo {
	t.Helper()
	dir := t.TempDir()
	WriteFiles(t, dir, files)
	r, err := repo.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// GitFixture is Fixture plus `git init` and one commit of every file, so
// Tracked() reflects the files. Extra git config pairs may be given.
func GitFixture(t *testing.T, files map[string]string, gitConfig ...string) *repo.Repo {
	t.Helper()
	dir := t.TempDir()
	WriteFiles(t, dir, files)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test fixture; args are literals
		cmd.Env = GitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	for i := 0; i+1 < len(gitConfig); i += 2 {
		run("config", gitConfig[i], gitConfig[i+1])
	}
	run("add", "-A")
	run("commit", "-q", "-m", "chore: fixture", "--no-verify")
	r, err := repo.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// GitEnv is the environment for fixture git commands: a fixed identity and
// no user or system configuration, so settings such as tag.gpgsign cannot
// change what a test does.
func GitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
}

// WriteFiles creates the given files under dir.
func WriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
