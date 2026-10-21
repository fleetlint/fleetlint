package fleet

import (
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// staleAfter is how long without a commit makes a repository stale.
const staleAfter = 365 * 24 * time.Hour

// GitState is what was checked: the commit, the branch, and how far the
// working tree is from it (rules read the working tree, not the commit).
type GitState struct {
	Commit     string    `json:"commit,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Dirty      int       `json:"dirty"`
	LastCommit time.Time `json:"last_commit,omitzero"`
}

// Stale reports whether the last commit is more than a year before now.
func (g *GitState) Stale(now time.Time) bool {
	return g != nil && !g.LastCommit.IsZero() && now.Sub(g.LastCommit) > staleAfter
}

// gitState reads the checkout; nil without git. A repository without
// commits keeps the fields git could not answer empty.
func gitState(r *repo.Repo) *GitState {
	if !r.HasGit() {
		return nil
	}
	g := &GitState{}
	g.Commit, _ = r.Git("rev-parse", "--short=12", "HEAD")
	if b, err := r.Git("rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "HEAD" {
		g.Branch = b
	}
	g.Dirty = DirtyCount(r)
	if ts, err := r.Git("log", "-1", "--format=%cI"); err == nil {
		g.LastCommit, _ = time.Parse(time.RFC3339, ts)
	}
	return g
}

// DirtyCount is the number of uncommitted changes in the working tree; a
// repository without git has none.
func DirtyCount(r *repo.Repo) int {
	if !r.HasGit() {
		return 0
	}
	if st, err := r.Git("status", "--porcelain"); err == nil && st != "" {
		return len(strings.Split(st, "\n"))
	}
	return 0
}
