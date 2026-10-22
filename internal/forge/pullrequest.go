package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PullRequest is a pull (or merge) request from Head into Base.
type PullRequest struct {
	Head, Base, Title, Body string
}

// Opened is the pull request the forge has for the branch.
type Opened struct {
	URL string
	// Updated is true when one was open already and got the new title and body.
	Updated bool
}

// ErrNoAPI means the remote is on no forge fleetlint has an API client for,
// or names no owner and repository.
var ErrNoAPI = errors.New("no forge API for this remote")

// CanOpen reports whether OpenPullRequest can reach the remote's forge with
// a token, which writing needs.
func (c Client) CanOpen(kind, remote string) bool {
	host, _, _, ok := parseRemote(kind, remote)
	return hasAPI(kind) && ok && c.HasToken(kind, host)
}

// DefaultBranch is the repository's default branch as the forge reports it.
func (c Client) DefaultBranch(ctx context.Context, kind, remote string) (string, error) {
	host, owner, name, ok := parseRemote(kind, remote)
	if !hasAPI(kind) || !ok {
		return "", ErrNoAPI
	}
	r, err := c.repository(ctx, kind, host, owner, name)
	if err == nil && r.DefaultBranch == "" {
		err = errors.New("the forge names no default branch")
	}
	return r.DefaultBranch, err
}

// OpenPullRequest updates the open pull request from pr.Head, or opens one.
// The body is cut to what every forge accepts.
func (c Client) OpenPullRequest(ctx context.Context, kind, remote string, pr PullRequest) (Opened, error) {
	host, owner, name, ok := parseRemote(kind, remote)
	if !hasAPI(kind) || !ok {
		return Opened{}, ErrNoAPI
	}
	if len(pr.Body) > maxPRBody {
		pr.Body = pr.Body[:maxPRBody] + "\n\n(cut short)\n"
	}
	if kind == "gitlab" {
		return c.openMergeRequest(ctx, host, owner+"/"+name, pr)
	}
	base := "/repos/" + owner + "/" + name + "/pulls"
	type gh struct {
		Number int    `json:"number"`
		URL    string `json:"html_url"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	var open []gh
	query := "?state=open&per_page=100&limit=50"
	if kind == "github" {
		query += "&head=" + url.QueryEscape(owner+":"+pr.Head)
	}
	if _, err := c.get(ctx, kind, host, base+query, &open); err != nil {
		return Opened{}, err
	}
	for _, o := range open {
		if o.Head.Ref == pr.Head {
			var got gh
			if _, err := c.do(ctx, http.MethodPatch, kind, host, fmt.Sprintf("%s/%d", base, o.Number), map[string]string{"title": pr.Title, "body": pr.Body}, &got); err != nil {
				return Opened{}, err
			}
			return Opened{URL: firstNonEmpty(got.URL, o.URL), Updated: true}, nil
		}
	}
	var got gh
	if _, err := c.do(ctx, http.MethodPost, kind, host, base, map[string]string{"title": pr.Title, "head": pr.Head, "base": pr.Base, "body": pr.Body}, &got); err != nil {
		return Opened{}, err
	}
	return Opened{URL: got.URL}, nil
}

func (c Client) openMergeRequest(ctx context.Context, host, project string, pr PullRequest) (Opened, error) {
	base := "/projects/" + url.PathEscape(project) + "/merge_requests"
	type mr struct {
		IID int    `json:"iid"`
		URL string `json:"web_url"`
	}
	var open []mr
	if _, err := c.get(ctx, "gitlab", host, base+"?state=opened&source_branch="+url.QueryEscape(pr.Head), &open); err != nil {
		return Opened{}, err
	}
	var got mr
	if len(open) > 0 {
		_, err := c.do(ctx, http.MethodPut, "gitlab", host, fmt.Sprintf("%s/%d", base, open[0].IID), map[string]string{"title": pr.Title, "description": pr.Body}, &got)
		return Opened{URL: firstNonEmpty(got.URL, open[0].URL), Updated: true}, err
	}
	_, err := c.do(ctx, http.MethodPost, "gitlab", host, base, map[string]string{"source_branch": pr.Head, "target_branch": pr.Base, "title": pr.Title, "description": pr.Body}, &got)
	return Opened{URL: got.URL}, err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
