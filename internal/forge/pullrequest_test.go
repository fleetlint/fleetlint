package forge_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/forge"
)

// GitLab: a group with a subgroup, paged by X-Next-Page, nested remotes.
func TestGitLabListsGroupsAndNestedProjects(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/groups/acme%2Fplatform/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_subgroups") != "true" {
			t.Error("subgroups are listed")
		}
		page := r.URL.Query().Get("page")
		next := map[string]string{"1": "2", "2": ""}[page]
		w.Header().Set("X-Next-Page", next)
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"path": "api" + page, "path_with_namespace": "acme/platform/tools/api" + page,
			"http_url_to_repo": "https://gitlab.example/acme/platform/tools/api" + page + ".git",
			"visibility":       map[string]string{"1": "public", "2": "internal"}[page], "default_branch": "main",
		}})
	})
	mux.HandleFunc("/projects/acme%2Fplatform%2Ftools%2Fapi1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "api1", "visibility": "public", "default_branch": "trunk"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := forge.Client{BaseURL: srv.URL, Getenv: func(k string) string { return map[string]string{"GITLAB_HOST": "gitlab.example"}[k] }}
	src, err := forge.ParseSource("gitlab:gitlab.example/acme/platform")
	if err != nil || src.Owner != "acme/platform" {
		t.Fatalf("source %+v %v", src, err)
	}
	repos, err := c.List(context.Background(), src)
	if err != nil || len(repos) != 2 || repos[0].Name != "tools/api1" || repos[0].Private || !repos[1].Private {
		t.Fatalf("repos %+v %v", repos, err)
	}
	public, asked, _, err := c.RemoteVisibility(context.Background(), "gitlab", "git@gitlab.example:acme/platform/tools/api1.git")
	if err != nil || !asked || !public {
		t.Errorf("a nested remote is looked up: %v %v %v", public, asked, err)
	}
	if b, err := c.DefaultBranch(context.Background(), "gitlab", "https://gitlab.example/acme/platform/tools/api1.git"); err != nil || b != "trunk" {
		t.Errorf("default branch %q %v", b, err)
	}
	if _, _, _, err := (forge.Client{BaseURL: srv.URL, Getenv: func(string) string { return "" }}).RemoteVisibility(context.Background(), "gitlab", "https://gitlab.other/a/b.git"); !errors.Is(err, forge.ErrUnknownHost) {
		t.Errorf("an unnamed GitLab host is not contacted: %v", err)
	}
}

// A fake forge that keeps pull requests in memory for each API shape.
func prForge(t *testing.T, kind string) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	open := map[string]bool{}
	mux := http.NewServeMux()
	record := func(r *http.Request) map[string]string {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+body["title"])
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("%s: authorization %q", r.URL.Path, got)
		}
		return body
	}
	switch kind {
	case "gitlab":
		mux.HandleFunc("/projects/acme%2Fapp/merge_requests", func(w http.ResponseWriter, r *http.Request) {
			body := record(r)
			if r.Method == http.MethodGet {
				var out []map[string]any
				if open[r.URL.Query().Get("source_branch")] {
					out = append(out, map[string]any{"iid": 7, "web_url": "https://gitlab.example/acme/app/-/merge_requests/7"})
				}
				_ = json.NewEncoder(w).Encode(out)
				return
			}
			open[body["source_branch"]] = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"iid": 7, "web_url": "https://gitlab.example/acme/app/-/merge_requests/7"})
		})
		mux.HandleFunc("/projects/acme%2Fapp/merge_requests/7", func(w http.ResponseWriter, r *http.Request) {
			record(r)
			_ = json.NewEncoder(w).Encode(map[string]any{"iid": 7, "web_url": "https://gitlab.example/acme/app/-/merge_requests/7"})
		})
	default:
		mux.HandleFunc("/repos/acme/app/pulls", func(w http.ResponseWriter, r *http.Request) {
			body := record(r)
			if r.Method == http.MethodGet {
				out := make([]map[string]any, 0, len(open))
				for head := range open {
					out = append(out, map[string]any{"number": 3, "html_url": "https://forge.example/acme/app/pulls/3", "head": map[string]string{"ref": head}})
				}
				_ = json.NewEncoder(w).Encode(out)
				return
			}
			if body["base"] != "main" {
				t.Errorf("base %q", body["base"])
			}
			open[body["head"]] = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 3, "html_url": "https://forge.example/acme/app/pulls/3"})
		})
		mux.HandleFunc("/repos/acme/app/pulls/3", func(w http.ResponseWriter, r *http.Request) {
			record(r)
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 3, "html_url": "https://forge.example/acme/app/pulls/3"})
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

// The first call opens the pull request, the second updates the open one.
func TestOpenPullRequestOpensThenUpdates(t *testing.T) {
	t.Parallel()
	for kind, remote := range map[string]string{
		"github": "https://github.com/acme/app.git",
		"gitea":  "https://git.example/acme/app.git",
		"gitlab": "git@gitlab.example:acme/app.git",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, calls := prForge(t, kind)
			env := map[string]string{"GITHUB_TOKEN": "tok", "GITEA_TOKEN": "tok", "GITLAB_TOKEN": "tok", "GITEA_HOST": "git.example", "GITLAB_HOST": "gitlab.example"}
			c := forge.Client{BaseURL: srv.URL, Getenv: func(k string) string { return env[k] }}
			if !c.CanOpen(kind, remote) {
				t.Fatal("a token for the host is enough to open")
			}
			pr := forge.PullRequest{Head: "fleetlint/fixes", Base: "main", Title: "Apply fleetlint fixes", Body: strings.Repeat("x", 70000)}
			first, err := c.OpenPullRequest(context.Background(), kind, remote, pr)
			if err != nil || first.Updated || first.URL == "" {
				t.Fatalf("open: %+v %v", first, err)
			}
			second, err := c.OpenPullRequest(context.Background(), kind, remote, pr)
			if err != nil || !second.Updated || second.URL != first.URL {
				t.Fatalf("update: %+v %v", second, err)
			}
			if got := fmt.Sprint(*calls); strings.Count(got, "GET") != 2 || !strings.Contains(got, "POST") || (!strings.Contains(got, "PATCH") && !strings.Contains(got, "PUT")) {
				t.Errorf("calls: %v", got)
			}
		})
	}
	if (forge.Client{Getenv: func(string) string { return "" }}).CanOpen("github", "https://github.com/acme/app.git") {
		t.Error("no token, no API write")
	}
}
