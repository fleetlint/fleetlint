package forge_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fleetlint/fleetlint/internal/forge"
)

// fakeForge serves 120 repositories for the user "solo" (an unknown
// organization) and one private repository visible only with a token.
func fakeForge(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/solo/repos", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	mux.HandleFunc("/users/solo/repos", func(w http.ResponseWriter, r *http.Request) {
		var page int
		_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
		var out []forge.Repo
		for i := (page - 1) * 50; i < page*50 && i < 120; i++ {
			out = append(out, forge.Repo{Name: fmt.Sprintf("r%03d", i), CloneURL: fmt.Sprintf("https://forge.example/solo/r%03d.git", i), Archived: i == 7})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/repos/solo/open", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(forge.Repo{Name: "open"})
	})
	mux.HandleFunc("/repos/solo/secret", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t0ken" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(forge.Repo{Name: "secret", Private: true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestListPagesAndFallsBackToUser(t *testing.T) {
	t.Parallel()
	c := forge.Client{BaseURL: fakeForge(t).URL, Getenv: func(string) string { return "" }}
	src, err := forge.ParseSource("github:solo")
	if err != nil {
		t.Fatal(err)
	}
	repos, err := c.List(context.Background(), src)
	if err != nil || len(repos) != 120 || repos[119].Name != "r119" || !repos[7].Archived {
		t.Fatalf("got %d repositories, err=%v", len(repos), err)
	}
	if _, err := c.List(context.Background(), forge.Source{Kind: "github", Host: "github.com", Owner: "nobody"}); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("an unknown owner must be ErrNotFound, got %v", err)
	}
}

func TestPublicUsesTheTokenFromTheEnvironment(t *testing.T) {
	t.Parallel()
	url := fakeForge(t).URL
	anon := forge.Client{BaseURL: url, Getenv: func(string) string { return "" }}
	if pub, err := anon.Public(context.Background(), "github", "github.com", "solo", "open"); err != nil || !pub {
		t.Fatalf("open: public=%v err=%v", pub, err)
	}
	if _, err := anon.Public(context.Background(), "github", "github.com", "solo", "secret"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("a private repository without a token must be ErrNotFound, got %v", err)
	}
	authed := forge.Client{BaseURL: url, Getenv: func(k string) string {
		if k == "GITEA_TOKEN" {
			return "t0ken"
		}
		return ""
	}}
	if pub, err := authed.Public(context.Background(), "gitea", "git.example.com", "solo", "secret"); err != nil || pub {
		t.Fatalf("secret with token: public=%v err=%v", pub, err)
	}
	if _, err := anon.Public(context.Background(), "github", "github.com", "solo/..", "x"); err == nil {
		t.Fatal("names with path separators must be rejected before any request")
	}
}

func TestParseSourceAndRemote(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "github", "github:", "github:a/b", "gitea:owner", "gitea:host/a/b", "gitea:ho st/x", "gitlab:x", "gitea:host/path/../x"} {
		if _, err := forge.ParseSource(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if s, err := forge.ParseSource("gitea:git.example.com/platform"); err != nil || s.Host != "git.example.com" || s.Owner != "platform" {
		t.Errorf("gitea source: %+v %v", s, err)
	}
	remotes := map[string][3]string{
		"https://github.com/fleetlint/fleetlint.git":       {"github.com", "fleetlint", "fleetlint"},
		"git@github.com:fleetlint/fleetlint.git":           {"github.com", "fleetlint", "fleetlint"},
		"ssh://git@git.example.com:2222/platform/tool.git": {"git.example.com", "platform", "tool"},
		"https://git.example.com/platform/tool":            {"git.example.com", "platform", "tool"},
	}
	for remote, want := range remotes {
		host, owner, name, ok := forge.ParseRemote(remote)
		if !ok || [3]string{host, owner, name} != want {
			t.Errorf("%s: got %s %s %s ok=%v", remote, host, owner, name, ok)
		}
	}
	for _, bad := range []string{"", "/local/path", "https://github.com/only-owner", "https://gitlab.com/group/sub/repo.git"} {
		if _, _, _, ok := forge.ParseRemote(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}
