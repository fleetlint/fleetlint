package fleet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Discovered repositories are cloned from the forge's own host only, with
// the token on https, and their names are sanitised like fleet-file names.
func TestDiscoverTakesCloneURLsOnTheSourceOnly(t *testing.T) { // not parallel: GITHUB_API_URL
	var repos []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/repos" {
			http.NotFound(w, r)
			return
		}
		list := repos
		if r.URL.Query().Get("page") != "1" {
			list = nil
		}
		_ = json.NewEncoder(w).Encode(list)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("GH_TOKEN", "")
	api := "https://" + strings.TrimPrefix(srv.URL, "http://")
	repos = []map[string]any{
		{"name": "we#ird? name", "clone_url": "https://github.com/acme/weird.git"},
		{"name": "ssh", "clone_url": "git@github.com:acme/ssh.git"},
		{"name": "api", "clone_url": api + "/acme/api.git"},
	}
	entries, err := discover(context.Background(), "github:acme")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"we-ird-name": "tok", "ssh": "", "api": "tok"}
	if len(entries) != len(want) {
		t.Fatalf("entries: %+v", entries)
	}
	for _, e := range entries {
		if tok, ok := want[e.Name]; !ok || e.Token != tok {
			t.Errorf("entry %q with token %q; want one of %v (names sanitised like fleet-file names, the token on https only)", e.Name, e.Token, want)
		}
	}
	for _, bad := range []string{"/srv/git/acme/x", "https://evil.example/acme/x.git", "git@evil.example:acme/x.git", "ssh://git@evil.example/acme/x", "ftp://github.com/acme/x"} {
		repos = []map[string]any{{"name": "x", "clone_url": bad}}
		_, err := discover(context.Background(), "github:acme")
		if err == nil || !strings.Contains(err.Error(), "repository x: clone URL "+`"`+bad+`"`+" is not on github.com") {
			t.Errorf("clone URL %q: %v; want a refusal naming the repository", bad, err)
		}
	}
}
