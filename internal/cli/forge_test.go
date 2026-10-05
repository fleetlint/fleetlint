package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/cli"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// fakeGitHub answers the two API calls fleetlint makes and is installed
// through GITHUB_API_URL, the variable GitHub itself defines.
func fakeGitHub(t *testing.T, repos []map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(repos) })
	mux.HandleFunc("/repos/acme/open", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "open", "private": false})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
}

func TestFleetFromForge(t *testing.T) {
	pub := testutil.GitFixture(t, map[string]string{"go.mod": "module pub\n", "CLAUDE.md": "notes\n"}).Root
	priv := testutil.GitFixture(t, map[string]string{"go.mod": "module priv\n", "CLAUDE.md": "notes\n"}).Root
	fakeGitHub(t, []map[string]any{
		{"name": "pub", "clone_url": pub, "private": false},
		{"name": "priv", "clone_url": priv, "private": true},
		{"name": "old", "clone_url": pub, "archived": true},
		{"name": "theirs", "clone_url": pub, "fork": true},
	})
	out := t.TempDir()
	code, stdout, stderr := runCLI(t, t.TempDir(), "fleet", "--from", "github:acme", "--out", out, "--cache", t.TempDir())
	if code != cli.ExitOK || !strings.Contains(stdout, "2 repositories checked") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "fleet.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Repos []struct {
			Name  string `json:"name"`
			Facts struct {
				Visibility string `json:"visibility"`
			} `json:"facts"`
			Results []struct {
				Rule   struct{ ID string } `json:"rule"`
				Status string              `json:"status"`
			} `json:"results"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	// The forge's answer decides whether the public-only rule applies.
	want := map[string][2]string{"pub": {"public", "fail"}, "priv": {"private", "n/a"}}
	for _, r := range rep.Repos {
		status := ""
		for _, res := range r.Results {
			if res.Rule.ID == "public/no-agent-files" {
				status = res.Status
			}
		}
		if got := [2]string{r.Facts.Visibility, status}; got != want[r.Name] {
			t.Errorf("%s: visibility and public/no-agent-files = %v, want %v", r.Name, got, want[r.Name])
		}
	}
	if code, _, stderr := runCLI(t, t.TempDir(), "fleet", "--from", "github:nobody", "--out", t.TempDir()); code != cli.ExitConfig || !strings.Contains(stderr, "not found") {
		t.Fatalf("an unknown owner must be a configuration error: exit %d %s", code, stderr)
	}
}

func TestInitLooksUpVisibility(t *testing.T) {
	fakeGitHub(t, nil)
	dir := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"}, "remote.origin.url", "https://github.com/acme/open.git").Root
	code, stdout, stderr := runCLI(t, dir, "init")
	if code != cli.ExitOK || !strings.Contains(stdout, "visibility: public") || !strings.Contains(stdout, "visibility=public tier=1") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	// Rewriting the file must look the visibility up again, not drop it.
	if code, stdout, _ = runCLI(t, dir, "init", "--force"); code != cli.ExitOK || !strings.Contains(stdout, "\n  visibility: public") {
		t.Fatalf("init --force lost the visibility: exit %d\n%s", code, stdout)
	}
	hidden := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"}, "remote.origin.url", "git@github.com:acme/hidden.git").Root
	code, stdout, stderr = runCLI(t, hidden, "init")
	if code != cli.ExitOK || strings.Contains(stdout, "\nfacts:\n") || !strings.Contains(stderr, "left undetected") {
		t.Fatalf("a repository the forge hides stays undetected, with a note: exit %d\n%s\n%s", code, stdout, stderr)
	}
}
