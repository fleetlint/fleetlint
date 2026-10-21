// Package testutil builds fixture repositories for tests. A fixture is a
// temp directory with files and, optionally, a real git repository so that
// tracked-file rules see what git sees.
package testutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	Git(t, dir, "init", "-q", "-b", "main")
	for i := 0; i+1 < len(gitConfig); i += 2 {
		Git(t, dir, "config", gitConfig[i], gitConfig[i+1])
	}
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", "chore: fixture", "--no-verify")
	r, err := repo.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Git runs git in dir with GitEnv and returns its trimmed stdout; an error
// fails the test.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test helper; the test names the arguments
	cmd.Env = GitEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// GitEnv is the environment for fixture git commands: a fixed identity, no
// user or system configuration, so settings such as tag.gpgsign cannot
// change what a test does, and no background maintenance, so nothing writes
// into a repository after the test that owns it has ended.
func GitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=gc.auto", "GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=gc.autoDetach", "GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=maintenance.auto", "GIT_CONFIG_VALUE_2=false")
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
