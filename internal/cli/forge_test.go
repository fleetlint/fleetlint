package cli_test

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
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
	mux.HandleFunc("/orgs/acme/repos", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(repos)
	})
	mux.HandleFunc("/repos/acme/open", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "open", "private": false})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
}

// gitOverHTTPS serves the repositories moved into its root over https on
// the forge's host (the loopback address), as a forge would, and returns
// the base URL of the clones; git trusts the server's certificate through
// GIT_SSL_CAINFO.
func gitOverHTTPS(t *testing.T, repos map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, dir := range repos {
		if err := os.Rename(dir, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	srv := httptest.NewTLSServer(&cgi.Handler{
		Path: gitBin, Root: "/git", Args: []string{"http-backend"},
		Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "HOME=" + t.TempDir()},
	})
	t.Cleanup(srv.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSL_CAINFO", ca)
	return srv.URL + "/git/"
}

func TestFleetFromForge(t *testing.T) {
	pub := testutil.GitFixture(t, map[string]string{"go.mod": "module pub\n", "CLAUDE.md": "notes\n"}).Root
	priv := testutil.GitFixture(t, map[string]string{"go.mod": "module priv\n", "CLAUDE.md": "notes\n"}).Root
	clones := gitOverHTTPS(t, map[string]string{"pub": pub, "priv": priv})
	fakeGitHub(t, []map[string]any{
		{"name": "pub", "clone_url": clones + "pub", "private": false},
		{"name": "priv", "clone_url": clones + "priv", "private": true},
		{"name": "old", "clone_url": clones + "pub", "archived": true},
		{"name": "theirs", "clone_url": clones + "pub", "fork": true},
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
			Err   string `json:"error"`
			Facts struct {
				Visibility string `json:"visibility"`
			} `json:"facts"`
			Results []struct {
				Rule   string `json:"rule"`
				Status string `json:"status"`
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
			if res.Rule == "public/license" {
				status = res.Status
			}
		}
		if got := [2]string{r.Facts.Visibility, status}; got != want[r.Name] {
			t.Errorf("%s: visibility and public/license = %v, want %v (error %q)", r.Name, got, want[r.Name], r.Err)
		}
	}
	if code, _, stderr := runCLI(t, t.TempDir(), "fleet", "--from", "github:nobody", "--out", t.TempDir()); code != cli.ExitConfig || !strings.Contains(stderr, "not found") {
		t.Fatalf("an unknown owner must be a configuration error: exit %d %s", code, stderr)
	}
}

// A forge that hands out a clone URL off its own host (a local path, say)
// gets nothing cloned: the run refuses with the repository's name.
func TestFleetFromForgeRefusesCloneURLsElsewhere(t *testing.T) {
	local := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"}).Root
	fakeGitHub(t, []map[string]any{{"name": "pub", "clone_url": local}})
	cache := t.TempDir()
	code, _, stderr := runCLI(t, t.TempDir(), "fleet", "--from", "github:acme", "--out", t.TempDir(), "--cache", cache)
	if code != cli.ExitConfig || !strings.Contains(stderr, `repository pub: clone URL "`+local+`" is not on github.com`) {
		t.Fatalf("exit %d %s", code, stderr)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Fatal("nothing may be cloned from a host the forge merely named")
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
