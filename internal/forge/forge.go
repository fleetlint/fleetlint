// Package forge asks GitHub and Gitea two questions over their REST APIs:
// which repositories an owner has, and whether one repository is public.
// It sends a token only when the environment provides one and never writes.
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	timeout  = 20 * time.Second
	maxBody  = 8 << 20
	pageSize = 50
	maxPages = 100

	githubHost = "github.com"
)

// ErrNotFound means the forge has no such owner or repository, or hides it
// from this caller.
var ErrNotFound = errors.New("not found on the forge (or not visible without a token)")

// Repo is one repository as the forge lists it.
type Repo struct {
	Name     string `json:"name"`
	CloneURL string `json:"clone_url"`
	Private  bool   `json:"private"`
	Archived bool   `json:"archived"`
	Fork     bool   `json:"fork"`
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
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Source is a parsed `github:<owner>` or `gitea:<host>/<owner>` reference.
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
	}
	return Source{}, fmt.Errorf("source %q: want github:<owner> or gitea:<host>/<owner>", s)
}

func validHost(h string) bool {
	u, err := url.Parse("https://" + h)
	return err == nil && h != "" && u.Host == h && u.Path == ""
}

// List returns the owner's repositories, organization first and user as the
// fallback. Private repositories appear only when the token may see them.
func (c Client) List(ctx context.Context, src Source) ([]Repo, error) {
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
		if err := c.get(ctx, src.Kind, src.Host, "/"+kind+"/"+src.Owner+"/repos?"+q, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			return all, nil
		}
	}
	return nil, fmt.Errorf("more than %d repositories; narrow the source", maxPages*pageSize)
}

// Public reports whether a repository is public. A repository the caller
// cannot see yields ErrNotFound: it is private or does not exist.
func (c Client) Public(ctx context.Context, kind, host, owner, name string) (bool, error) {
	if !nameRe.MatchString(owner) || !nameRe.MatchString(name) {
		return false, fmt.Errorf("%s/%s is not a repository name", owner, name)
	}
	var r Repo
	if err := c.get(ctx, kind, host, "/repos/"+owner+"/"+name, &r); err != nil {
		return false, err
	}
	return !r.Private, nil
}

func (c Client) get(ctx context.Context, kind, host, path string, into any) error {
	base := c.BaseURL
	if base == "" {
		switch kind {
		case "github":
			// GITHUB_API_URL is GitHub's own variable: set in Actions and
			// pointed at the instance on GitHub Enterprise Server.
			if base = c.getenv("GITHUB_API_URL"); base == "" {
				base = "https://api.github.com"
			}
		case "gitea":
			base = "https://" + host + "/api/v1"
		default:
			return fmt.Errorf("forge %q has no API client", kind)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "fleetlint")
	if tok := c.token(kind); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // read side; nothing to do on close failure
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s%s: HTTP %d", base, strings.SplitN(path, "?", 2)[0], resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%s%s: %w", base, strings.SplitN(path, "?", 2)[0], err)
	}
	return nil
}

// Token returns the token the client would use for the source, so clones
// of what it listed can authenticate the same way; "" when none is set.
func (c Client) Token(src Source) string { return c.token(src.Kind) }

// token returns the forge's token from the environment: GITHUB_TOKEN or
// GH_TOKEN for GitHub, GITEA_TOKEN for Gitea.
func (c Client) token(kind string) string {
	if kind == "github" {
		if t := c.getenv("GITHUB_TOKEN"); t != "" {
			return t
		}
		return c.getenv("GH_TOKEN")
	}
	return c.getenv("GITEA_TOKEN")
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
	var path string
	if m := scpRemoteRe.FindStringSubmatch(remote); m != nil {
		host, path = m[1], m[2]
	} else if u, err := url.Parse(remote); err == nil && u.Host != "" {
		host, path = u.Hostname(), u.Path
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(path, ".git"), "/"), "/")
	if host == "" || len(parts) != 2 || !nameRe.MatchString(parts[0]) || !nameRe.MatchString(parts[1]) {
		return "", "", "", false
	}
	return host, parts[0], parts[1], true
}
