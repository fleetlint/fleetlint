// Package forge talks to GitHub, Gitea (and Forgejo) and GitLab over their
// REST APIs: which repositories an owner has, whether one is public, and,
// the only write, opening or updating a pull request from a pushed branch.
// A Gitea or self-hosted GitLab host is contacted only when the user named
// it (GITEA_HOST, GITLAB_HOST or a --from source), since a repository's
// remote, not the user, says which host that is; tokens go to those hosts
// only. github.com and gitlab.com are always known.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	timeout  = 20 * time.Second
	maxBody  = 8 << 20
	pageSize = 50
	maxPages = 100

	githubHost = "github.com"
	gitlabHost = "gitlab.com"
	maxPRBody  = 60000
)

// ErrNotFound means the forge has no such owner or repository, or hides it
// from this caller.
var ErrNotFound = errors.New("not found on the forge (or not visible without a token)")

// Repo is one repository as the forge lists it.
type Repo struct {
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	DefaultBranch string `json:"default_branch"`
}

// gitlabProject is a project as GitLab's API returns it.
type gitlabProject struct {
	Path              string           `json:"path"`
	PathWithNamespace string           `json:"path_with_namespace"`
	HTTPURL           string           `json:"http_url_to_repo"`
	Visibility        string           `json:"visibility"`
	Archived          bool             `json:"archived"`
	ForkedFrom        *json.RawMessage `json:"forked_from_project"`
	DefaultBranch     string           `json:"default_branch"`
}

// repo converts the project, named relative to the listed group.
func (p gitlabProject) repo(group string) Repo {
	name := strings.TrimPrefix(p.PathWithNamespace, group+"/")
	if name == "" {
		name = p.Path
	}
	return Repo{Name: name, CloneURL: p.HTTPURL, Private: p.Visibility != "public", Archived: p.Archived, Fork: p.ForkedFrom != nil, DefaultBranch: p.DefaultBranch}
}

// Client talks to the forges. The zero value is ready to use.
type Client struct {
	// HTTP is the client to use; nil means a default with a timeout.
	HTTP *http.Client
	// Getenv reads the token variables; nil means os.Getenv.
	Getenv func(string) string
	// BaseURL overrides the API root for every forge and host; tests point
	// it at a local server.
	BaseURL string
	// Hosts are Gitea or GitLab hosts the user named besides GITEA_HOST and
	// GITLAB_HOST, such as the host of a --from source.
	Hosts []string
}

// ErrUnknownHost means a Gitea or GitLab host the user did not name:
// fleetlint does not contact it, and never sends it a token.
var ErrUnknownHost = errors.New("not contacted: name the host in GITEA_HOST (or GITLAB_HOST) to let fleetlint ask it")

// ErrRateLimited means the forge refused the request for its rate limit.
var ErrRateLimited = errors.New("rate limited by the forge; a token raises the limit")

// Trusted reports whether a Gitea or GitLab host was named by the user;
// gitlab.com always is.
func (c Client) Trusted(host string) bool {
	if strings.EqualFold(host, gitlabHost) {
		return true
	}
	named := append(strings.Split(c.getenv("GITEA_HOST"), ","), strings.Split(c.getenv("GITLAB_HOST"), ",")...)
	for _, h := range append(named, c.Hosts...) {
		if h = strings.TrimSpace(h); h != "" && strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

var (
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// groupRe is a GitLab namespace: a group with its subgroups.
	groupRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)
)

// Source is a parsed `github:<owner>`, `gitea:<host>/<owner>` or
// `gitlab:<host>/<group>` reference; a GitLab group may name subgroups.
type Source struct {
	Kind, Host, Owner string
}

// ParseSource reads a discovery source.
func ParseSource(s string) (Source, error) {
	kind, rest, ok := strings.Cut(s, ":")
	switch {
	case ok && kind == "github" && nameRe.MatchString(rest):
		return Source{Kind: "github", Host: githubHost, Owner: rest}, nil
	case ok && kind == "gitea":
		host, owner, ok := strings.Cut(rest, "/")
		if ok && validHost(host) && nameRe.MatchString(owner) {
			return Source{Kind: "gitea", Host: host, Owner: owner}, nil
		}
	case ok && kind == "gitlab":
		host, group, ok := strings.Cut(rest, "/")
		if ok && validHost(host) && groupRe.MatchString(group) {
			return Source{Kind: "gitlab", Host: host, Owner: group}, nil
		}
	}
	return Source{}, fmt.Errorf("source %q: want github:<owner>, gitea:<host>/<owner> or gitlab:<host>/<group>", s)
}

func validHost(h string) bool {
	u, err := url.Parse("https://" + h)
	return err == nil && h != "" && u.Host == h && u.Path == ""
}

// List returns the owner's repositories, organization first and user as the
// fallback. Private repositories appear only when the token may see them.
func (c Client) List(ctx context.Context, src Source) ([]Repo, error) {
	if src.Kind == "gitlab" {
		return c.listGitLab(ctx, src)
	}
	repos, err := c.listPath(ctx, src, "orgs")
	if errors.Is(err, ErrNotFound) {
		repos, err = c.listPath(ctx, src, "users")
	}
	if err != nil {
		return nil, fmt.Errorf("%s:%s: %w", src.Kind, src.Owner, err)
	}
	return repos, nil
}

func (c Client) listPath(ctx context.Context, src Source, kind string) ([]Repo, error) {
	var all []Repo
	for page := 1; page <= maxPages; page++ {
		q := fmt.Sprintf("per_page=%d&limit=%d&page=%d", pageSize, pageSize, page)
		var batch []Repo
		h, err := c.get(ctx, src.Kind, src.Host, "/"+kind+"/"+src.Owner+"/repos?"+q, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if !morePages(h, len(all), len(batch)) {
			return all, nil
		}
	}
	return nil, fmt.Errorf("more than %d repositories; narrow the source", maxPages*pageSize)
}

// listGitLab lists a group's projects with its subgroups', and a user's
// projects when no group has the name.
func (c Client) listGitLab(ctx context.Context, src Source) ([]Repo, error) {
	owner := url.PathEscape(src.Owner)
	repos, err := c.listGitLabPath(ctx, src, "/groups/"+owner+"/projects?include_subgroups=true&")
	if errors.Is(err, ErrNotFound) {
		repos, err = c.listGitLabPath(ctx, src, "/users/"+owner+"/projects?")
	}
	if err != nil {
		return nil, fmt.Errorf("%s:%s: %w", src.Kind, src.Owner, err)
	}
	return repos, nil
}

func (c Client) listGitLabPath(ctx context.Context, src Source, path string) ([]Repo, error) {
	var all []Repo
	for page := 1; page <= maxPages; page++ {
		var batch []gitlabProject
		h, err := c.get(ctx, src.Kind, src.Host, fmt.Sprintf("%sper_page=%d&page=%d", path, pageSize, page), &batch)
		if err != nil {
			return nil, err
		}
		for _, p := range batch {
			all = append(all, p.repo(src.Owner))
		}
		if !morePages(h, len(all), len(batch)) {
			return all, nil
		}
	}
	return nil, fmt.Errorf("more than %d repositories; narrow the source", maxPages*pageSize)
}

// morePages reads GitHub's Link header, GitLab's X-Next-Page and Gitea's
// X-Total-Count; without any, only an empty page ends the list, since a
// server may cap the page size below what was asked.
func morePages(h http.Header, have, got int) bool {
	if link := h.Get("Link"); link != "" {
		return strings.Contains(link, `rel="next"`)
	}
	if next, ok := h["X-Next-Page"]; ok {
		return len(next) > 0 && next[0] != ""
	}
	if total, err := strconv.Atoi(h.Get("X-Total-Count")); err == nil {
		return have < total && got > 0
	}
	return got > 0
}

// Public reports whether a repository is public. A repository the caller
// cannot see yields ErrNotFound: it is private or does not exist.
func (c Client) Public(ctx context.Context, kind, host, owner, name string) (bool, error) {
	r, err := c.repository(ctx, kind, host, owner, name)
	return err == nil && !r.Private, err
}

// repository looks one repository up; owner is a GitLab namespace there.
func (c Client) repository(ctx context.Context, kind, host, owner, name string) (Repo, error) {
	if (kind == "gitlab" && !groupRe.MatchString(owner)) || (kind != "gitlab" && !nameRe.MatchString(owner)) || !nameRe.MatchString(name) {
		return Repo{}, fmt.Errorf("%s/%s is not a repository name", owner, name)
	}
	if kind == "gitlab" {
		var p gitlabProject
		if _, err := c.get(ctx, kind, host, "/projects/"+url.PathEscape(owner+"/"+name), &p); err != nil {
			return Repo{}, err
		}
		return p.repo(owner), nil
	}
	var r Repo
	_, err := c.get(ctx, kind, host, "/repos/"+owner+"/"+name, &r)
	return r, err
}

// RemoteVisibility asks the forge of a git remote whether the repository
// is public. asked is false when there is nothing to ask: no forge API for
// the kind, or a remote that names no owner and repository.
func (c Client) RemoteVisibility(ctx context.Context, kind, remote string) (public, asked bool, host string, err error) {
	if !hasAPI(kind) {
		return false, false, "", nil
	}
	host, owner, name, ok := parseRemote(kind, remote)
	if !ok {
		return false, false, "", nil
	}
	public, err = c.Public(ctx, kind, host, owner, name)
	return public, true, host, err
}

func hasAPI(kind string) bool { return kind == "github" || kind == "gitea" || kind == "gitlab" }

func (c Client) get(ctx context.Context, kind, host, path string, into any) (http.Header, error) {
	return c.do(ctx, http.MethodGet, kind, host, path, nil, into)
}

// do sends one API request, with send as its JSON body, and decodes the
// answer into into.
func (c Client) do(ctx context.Context, method, kind, host, path string, send, into any) (http.Header, error) {
	if (kind == "gitea" || kind == "gitlab") && !c.Trusted(host) {
		return nil, fmt.Errorf("%s: %w", host, ErrUnknownHost)
	}
	base, err := c.apiBase(kind, host)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := jsonBody(send)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	if send != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "fleetlint")
	if tok := c.token(kind, host); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // read side; nothing to do on close failure
	endpoint := base + strings.SplitN(path, "?", 2)[0]
	if err := checkStatus(resp, endpoint); err != nil {
		return nil, err
	}
	return resp.Header, decode(resp.Body, endpoint, into)
}

// jsonBody is the request body for send; nil sends none.
func jsonBody(send any) (io.Reader, error) {
	if send == nil {
		return nil, nil
	}
	b, err := json.Marshal(send)
	return bytes.NewReader(b), err
}

// decode reads an answer into into, which may be nil.
func decode(r io.Reader, endpoint string, into any) error {
	answer, err := io.ReadAll(io.LimitReader(r, maxBody))
	if err != nil || into == nil {
		return err
	}
	if err := json.Unmarshal(answer, into); err != nil {
		return fmt.Errorf("%s: %w", endpoint, err)
	}
	return nil
}

func (c Client) apiBase(kind, host string) (string, error) {
	if c.BaseURL != "" {
		return c.BaseURL, nil
	}
	switch kind {
	case "github":
		// GITHUB_API_URL is GitHub's own variable: set in Actions and
		// pointed at the instance on GitHub Enterprise Server.
		if base := c.getenv("GITHUB_API_URL"); base != "" {
			return base, nil
		}
		return "https://api.github.com", nil
	case "gitea":
		return "https://" + host + "/api/v1", nil
	case "gitlab":
		return "https://" + host + "/api/v4", nil
	}
	return "", fmt.Errorf("forge %q has no API client", kind)
}

func checkStatus(resp *http.Response, endpoint string) error {
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case rateLimited(resp):
		reset := ""
		if t, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			reset = ", resets " + time.Unix(t, 0).Format(time.Kitchen)
		}
		return fmt.Errorf("%s: %w%s", endpoint, ErrRateLimited, reset)
	}
	return fmt.Errorf("%s: HTTP %d", endpoint, resp.StatusCode)
}

func rateLimited(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
	}
	return false
}

// Token returns the token the client would use for the source, so clones
// of what it listed can authenticate the same way; "" when none is set.
func (c Client) Token(src Source) string { return c.token(src.Kind, src.Host) }

// HasToken reports whether a token is set for the forge of a host.
func (c Client) HasToken(kind, host string) bool { return c.token(kind, host) != "" }

// token returns the forge's token from the environment: GITHUB_TOKEN or
// GH_TOKEN for GitHub (sent to its API only), GITEA_TOKEN for a Gitea host
// and GITLAB_TOKEN for a GitLab host the user named.
func (c Client) token(kind, host string) string {
	switch {
	case kind == "github":
		if t := c.getenv("GITHUB_TOKEN"); t != "" {
			return t
		}
		return c.getenv("GH_TOKEN")
	case kind == "gitea" && c.Trusted(host):
		return c.getenv("GITEA_TOKEN")
	case kind == "gitlab" && c.Trusted(host):
		return c.getenv("GITLAB_TOKEN")
	}
	return ""
}

func (c Client) getenv(key string) string {
	if c.Getenv != nil {
		return c.Getenv(key)
	}
	return os.Getenv(key)
}

var scpRemoteRe = regexp.MustCompile(`^[^@/]+@([^:/]+):(.+)$`)

// ParseRemote splits a git remote URL (https, ssh or scp-like) into host,
// owner and repository name.
func ParseRemote(remote string) (host, owner, name string, ok bool) {
	return parseRemote("", remote)
}

// parseRemote is ParseRemote; for GitLab the owner is the namespace, which
// may hold subgroups.
func parseRemote(kind, remote string) (host, owner, name string, ok bool) {
	var path string
	if m := scpRemoteRe.FindStringSubmatch(remote); m != nil {
		host, path = m[1], m[2]
	} else if u, err := url.Parse(remote); err == nil && u.Host != "" {
		host, path = u.Hostname(), u.Path
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(path, ".git"), "/"), "/")
	if kind == "gitlab" && len(parts) > 2 {
		parts = []string{strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]}
		if host == "" || !groupRe.MatchString(parts[0]) || !nameRe.MatchString(parts[1]) {
			return "", "", "", false
		}
		return host, parts[0], parts[1], true
	}
	if host == "" || len(parts) != 2 || !nameRe.MatchString(parts[0]) || !nameRe.MatchString(parts[1]) {
		return "", "", "", false
	}
	return host, parts[0], parts[1], true
}
