package testutil_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/testutil"
)

// Fixture repositories and test remotes start no background git: a commit
// and a push into a bare remote, run with git's defaults in the
// environment (as the code under test does), are traced and must not start
// `git maintenance` or `git gc`, which would keep writing after the test
// and break the removal of its temp directory.
func TestFixturesStartNoBackgroundGit(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"a.txt": "a\n"})
	remote := t.TempDir()
	testutil.Git(t, remote, "init", "-q", "--bare")
	testutil.NoBackgroundGit(t, remote)
	trace := filepath.Join(t.TempDir(), "trace.json")
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TRACE2_EVENT="+trace,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	testutil.WriteFiles(t, r.Root, map[string]string{"b.txt": "b\n"})
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "b", "--no-verify"}, {"push", "-q", remote, "main"}} {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", r.Root}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	events, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), "receive-pack") {
		t.Fatal("the trace does not cover the remote's side of the push")
	}
	for _, background := range []string{`"maintenance","run"`, `"gc","--auto"`} {
		if strings.Contains(string(events), background) {
			t.Errorf("a fixture started background git (%s):\n%s", background, events)
		}
	}
}
