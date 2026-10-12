package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fleetlint/fleetlint/internal/catalog/fetch"
)

const gitPrefix = "git+"

// ErrGitPinRequired is returned for a git reference whose revision can move.
var ErrGitPinRequired = errors.New("git catalogs at a tag must be pinned with #sha256-<digest>; a full commit SHA needs no digest")

// gitRef is a parsed `git+<url>//<path>@<rev>[#sha256-<digest>]` reference.
type gitRef struct {
	URL, Path, Rev, Digest string
}

func (g gitRef) isCommit() bool { return fetch.IsCommit(g.Rev) }

// parseGitRef splits a git catalog reference. The repository URL must use
// https or ssh, the revision is a tag or a full commit SHA, and the path is
// relative to the repository root.
func parseGitRef(ref string) (gitRef, error) {
	rest, digest, pinned := strings.Cut(strings.TrimPrefix(ref, gitPrefix), "#")
	if pinned && !strings.HasPrefix(digest, "sha256-") {
		return gitRef{}, fmt.Errorf("%s: the fragment must be sha256-<digest>", ref)
	}
	scheme, tail, ok := strings.Cut(rest, "://")
	if !ok || (scheme != "https" && scheme != "ssh") {
		return gitRef{}, fmt.Errorf("%s: git catalogs are read over https or ssh", ref)
	}
	repo, pathRev, ok := strings.Cut(tail, "//")
	if !ok || repo == "" {
		return gitRef{}, fmt.Errorf("%s: want git+<url>//<path>@<tag or commit>", ref)
	}
	at := strings.LastIndex(pathRev, "@")
	if at <= 0 || at == len(pathRev)-1 {
		return gitRef{}, fmt.Errorf("%s: want git+<url>//<path>@<tag or commit>", ref)
	}
	g := gitRef{URL: scheme + "://" + repo, Path: pathRev[:at], Rev: pathRev[at+1:], Digest: strings.TrimPrefix(digest, "sha256-")}
	if err := g.validate(); err != nil {
		return gitRef{}, fmt.Errorf("%s: %w", ref, err)
	}
	return g, nil
}

func (g gitRef) validate() error {
	if !fetch.ValidRev(g.Rev) {
		return fmt.Errorf("revision %q is not a tag or a full commit SHA", g.Rev)
	}
	for _, seg := range strings.Split(g.Path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("path %q must be relative to the repository root", g.Path)
		}
	}
	if !g.isCommit() && g.Digest == "" {
		return ErrGitPinRequired
	}
	return nil
}
