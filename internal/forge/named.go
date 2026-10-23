package forge

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

// Named is a forge the user names with --forge or FLEETLINT_FORGE: its
// kind, its host and its web root. Naming it trusts the host, and a
// --token goes to it alone.
type Named struct {
	Kind, Host, Root string
}

// ParseNamed reads `github`, `github:<host>`, `gitea:<host>`,
// `gitlab:<host>` (a host or an https URL with a path prefix), or a bare
// https URL whose kind the repository's remote decides.
func ParseNamed(s string) (Named, error) {
	kind, rest, ok := strings.Cut(s, ":")
	switch {
	case s == "github":
		return Named{Kind: "github", Host: githubHost, Root: "https://" + githubHost}, nil
	case strings.HasPrefix(s, "https://"):
		return namedURL("", s)
	case ok && hasAPI(kind) && strings.HasPrefix(rest, "https://"):
		return namedURL(kind, rest)
	case ok && hasAPI(kind) && validHost(rest):
		return Named{Kind: kind, Host: rest, Root: "https://" + rest}, nil
	}
	return Named{}, fmt.Errorf("forge %q: want github, github:<host>, gitea:<host>, gitlab:<host> or an https URL", s)
}

func namedURL(kind, raw string) (Named, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return Named{}, fmt.Errorf("forge %q: want an https URL without credentials or a query", raw)
	}
	return Named{Kind: kind, Host: u.Hostname(), Root: strings.TrimSuffix("https://"+u.Host+u.Path, "/")}, nil
}

// api is the API root of the named forge for its kind.
func (n Named) api(kind string) string {
	switch {
	case kind == "github" && strings.EqualFold(n.Host, githubHost):
		return "https://api.github.com"
	case kind == "github":
		return n.Root + "/api/v3"
	case kind == "gitea":
		return n.Root + "/api/v1"
	case kind == "gitlab":
		return n.Root + "/api/v4"
	}
	return ""
}

// actionsServer is the host of the Gitea or Forgejo instance whose Actions
// runner this is; empty outside such a run.
func (c Client) actionsServer() string {
	if c.getenv("GITEA_ACTIONS") != "true" && c.getenv("FORGEJO_ACTIONS") != "true" {
		return ""
	}
	return urlHost(c.getenv("GITHUB_SERVER_URL"))
}

// gitlabCIServer is the host of the GitLab instance running this CI job.
func (c Client) gitlabCIServer() string {
	if c.getenv("GITLAB_CI") != "true" {
		return ""
	}
	return c.getenv("CI_SERVER_HOST")
}

func urlHost(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Hostname()
	}
	return ""
}

// KindOf is the forge a remote is on: the named forge's kind for its host,
// the CI system's when the job runs on that host, else detected.
func (c Client) KindOf(remote, detected string) string {
	host, _, _, ok := parseRemote("", remote)
	if !ok {
		host, _, _, _ = parseRemote("gitlab", remote)
	}
	switch {
	case host == "":
		return detected
	case c.Forge.Kind != "" && strings.EqualFold(host, c.Forge.Host):
		return c.Forge.Kind
	case strings.EqualFold(host, c.actionsServer()):
		return "gitea"
	case strings.EqualFold(host, c.gitlabCIServer()):
		return "gitlab"
	}
	return detected
}

// ForRemote is the client for work on the remote's own repository, which
// the user asked for (a pull request): the remote's host counts as named.
func (c Client) ForRemote(kind, remote string) Client {
	if host, _, _, ok := parseRemote(kind, remote); ok {
		c.Hosts = append(append([]string{}, c.Hosts...), host)
	}
	return c
}

// tokenGoesTo reports whether the --token belongs to the host: the named
// forge and the named hosts, or every trusted host when none is named.
func (c Client) tokenGoesTo(kind, host string) bool {
	if c.Forge.Host == "" && len(c.Hosts) == 0 {
		return kind == "github" || c.Trusted(host)
	}
	if strings.EqualFold(host, c.Forge.Host) {
		return true
	}
	for _, h := range c.Hosts {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

// GitAuth is the git environment that pushes to an https remote with the
// forge's token, as an Authorization header scoped to its host; nil when
// there is no token or the remote is not https (git's own credentials
// apply then).
func (c Client) GitAuth(kind, remote string) []string {
	host, _, _, ok := parseRemote(kind, remote)
	if !ok || !strings.HasPrefix(remote, "https://") {
		return nil
	}
	user := "x-access-token"
	if kind == "gitlab" {
		user = "oauth2"
	}
	return AuthEnv(remote, user, c.token(kind, host))
}

// AuthEnv sends user and token as basic auth to the URL's host only. The
// empty first value resets the headers a checkout stored for the host
// (actions/checkout does), so only this one is sent.
func AuthEnv(raw, user, token string) []string {
	u, err := url.Parse(raw)
	if token == "" || err != nil || u.Host == "" || u.Scheme != "https" {
		return nil
	}
	key := "http." + u.Scheme + "://" + u.Host + "/.extraheader"
	basic := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
	return []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=" + key, "GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=" + key, "GIT_CONFIG_VALUE_1=AUTHORIZATION: basic " + basic,
	}
}
