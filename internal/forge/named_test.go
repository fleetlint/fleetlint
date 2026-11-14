package forge_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/forge"
)

func TestParseNamed(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]forge.Named{
		"github":                             {Kind: "github", Host: "github.com", Root: "https://github.com"},
		"gitea:codeberg.org":                 {Kind: "gitea", Host: "codeberg.org", Root: "https://codeberg.org"},
		"gitlab:https://git.example/gitlab/": {Kind: "gitlab", Host: "git.example", Root: "https://git.example/gitlab"},
		"https://git.example.com":            {Host: "git.example.com", Root: "https://git.example.com"},
	} {
		if got, err := forge.ParseNamed(in); err != nil || got != want {
			t.Errorf("%s: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"bitbucket:x.org", "http://git.example", "https://u:p@git.example", "gitea:"} {
		if _, err := forge.ParseNamed(bad); err == nil {
			t.Errorf("%s: expected an error", bad)
		}
	}
}

// --forge names a self-hosted GitLab without "gitlab" in its name: the
// kind, the API root under its path and the token go to it alone.
func TestNamedForgeKindAPIAndToken(t *testing.T) {
	t.Parallel()
	var auth, path string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(map[string]any{"visibility": "private", "default_branch": "trunk"})
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "https://")
	named, err := forge.ParseNamed("gitlab:" + srv.URL + "/scm")
	if err != nil {
		t.Fatal(err)
	}
	c := forge.Client{HTTP: srv.Client(), Forge: named, GivenToken: "given", Getenv: func(k string) string { return map[string]string{"GITHUB_TOKEN": "gh"}[k] }}
	remote := "https://" + host + "/scm/acme/platform/app.git"
	if kind := c.KindOf(remote, "gitea"); kind != "gitlab" {
		t.Fatalf("the named kind wins for its host: %s", kind)
	}
	if b, err := c.DefaultBranch(context.Background(), "gitlab", "https://"+host+"/acme/platform/app.git"); err != nil || b != "trunk" || auth != "Bearer given" || !strings.HasPrefix(path, "/scm/api/v4/projects/acme%2Fplatform%2Fapp") {
		t.Errorf("branch %q %v, auth %q, path %q", b, err, auth, path)
	}
	if env := c.GitAuth("github", "https://github.com/acme/app.git"); strings.Contains(strings.Join(env, " "), base64.StdEncoding.EncodeToString([]byte("x-access-token:given"))) {
		t.Errorf("the given token is not sent to another forge: %q", env)
	}
	env := c.GitAuth("gitlab", remote)
	if want := base64.StdEncoding.EncodeToString([]byte("oauth2:given")); len(env) != 5 || !strings.HasSuffix(env[4], want) || env[3] != "GIT_CONFIG_KEY_1=http.https://"+host+"/.extraheader" {
		t.Errorf("the push authenticates with the token, scoped to the host: %q", env)
	}
	if env[1] != env[3][:len("GIT_CONFIG_KEY_")]+"0"+env[3][len("GIT_CONFIG_KEY_1"):] || env[2] != "GIT_CONFIG_VALUE_0=" {
		t.Errorf("a header a checkout stored for the host is reset first: %q", env)
	}
	if env := c.GitAuth("gitlab", "git@"+host+":acme/app.git"); env != nil {
		t.Errorf("ssh remotes keep git's own credentials: %q", env)
	}
}

// In Gitea or Forgejo Actions and in GitLab CI the job's own server is
// known: its kind, its API from the CI variables, and the job's token.
func TestCIServersAreKnown(t *testing.T) {
	t.Parallel()
	gitea := forge.Client{Getenv: func(k string) string {
		return map[string]string{"GITEA_ACTIONS": "true", "GITHUB_SERVER_URL": "https://code.example", "GITHUB_API_URL": "https://code.example/api/v1", "GITHUB_TOKEN": "job"}[k]
	}}
	remote := "https://code.example/acme/app.git"
	if kind := gitea.KindOf(remote, "github"); kind != "gitea" || !gitea.Trusted("code.example") || !gitea.CanOpen("gitea", remote) {
		t.Errorf("Gitea Actions: kind %s, trusted %v, can open %v", kind, gitea.Trusted("code.example"), gitea.CanOpen("gitea", remote))
	}
	if env := gitea.GitAuth("gitea", remote); len(env) != 5 {
		t.Errorf("the job token pushes: %q", env)
	}
	gitlab := forge.Client{Getenv: func(k string) string {
		return map[string]string{"GITLAB_CI": "true", "CI_SERVER_HOST": "scm.example", "CI_API_V4_URL": "https://scm.example/api/v4", "GITLAB_TOKEN": "pat"}[k]
	}}
	if kind := gitlab.KindOf("git@scm.example:acme/sub/app.git", "gitea"); kind != "gitlab" || !gitlab.CanOpen("gitlab", "https://scm.example/acme/sub/app.git") {
		t.Errorf("GitLab CI: kind %s", kind)
	}
	outside := forge.Client{Getenv: func(string) string { return "" }}
	if outside.Trusted("code.example") || outside.KindOf(remote, "gitea") != "gitea" {
		t.Error("outside CI nothing is assumed")
	}
}

// A pull request on the remote's own forge needs no GITEA_HOST: asking for
// it names the host.
func TestForRemoteNamesTheHost(t *testing.T) {
	t.Parallel()
	c := forge.Client{Getenv: func(k string) string { return map[string]string{"GITEA_TOKEN": "tok"}[k] }}
	remote := "https://codeberg.org/acme/app.git"
	if c.CanOpen("gitea", remote) {
		t.Fatal("an unnamed host is not contacted")
	}
	if !c.ForRemote("gitea", remote).CanOpen("gitea", remote) {
		t.Error("the remote's own host is named by the request")
	}
}

// A GitHub Enterprise host is self-hosted: not contacted and sent no token
// until named, then asked at its own API; in its own Actions run it is known.
func TestGitHubEnterpriseHostIsContactedOnlyWhenNamed(t *testing.T) {
	t.Parallel()
	remote := "https://github.acme.example/acme/app.git"
	env := map[string]string{"GITHUB_TOKEN": "gh"}
	c := forge.Client{Getenv: func(k string) string { return env[k] }}
	if _, _, _, err := c.RemoteVisibility(context.Background(), "github", remote); !errors.Is(err, forge.ErrUnknownHost) {
		t.Errorf("an unnamed GitHub Enterprise host is not asked: %v", err)
	}
	if c.CanOpen("github", remote) || c.GitAuth("github", remote) != nil {
		t.Error("GITHUB_TOKEN does not go to an unnamed host")
	}
	if !c.CanOpen("github", "https://github.com/acme/app.git") {
		t.Error("github.com is always known")
	}
	if !c.ForRemote("github", remote).CanOpen("github", remote) {
		t.Error("a pull request on the remote names its host")
	}

	var path string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "app", "private": true})
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "https://")
	named, err := forge.ParseNamed("github:" + srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	nc := forge.Client{HTTP: srv.Client(), Forge: named, Getenv: c.Getenv}
	if public, asked, _, err := nc.RemoteVisibility(context.Background(), "github", "https://"+host+"/acme/app.git"); err != nil || !asked || public || path != "/api/v3/repos/acme/app" {
		t.Errorf("--forge github:<host> asks its API: public %v, asked %v, err %v, path %q", public, asked, err, path)
	}

	actions := forge.Client{Getenv: func(k string) string {
		return map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.acme.example", "GITHUB_TOKEN": "job"}[k]
	}}
	if !actions.Trusted("github.acme.example") || !actions.CanOpen("github", remote) {
		t.Error("in its own Actions run the GitHub Enterprise host is known")
	}
}
