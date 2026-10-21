package fix

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// ErrDirty is why CheckClean refuses a working tree with uncommitted
// changes to tracked files.
var ErrDirty = errors.New("the working tree has uncommitted changes to tracked files; commit or stash them, or pass --allow-dirty")

// CheckClean returns nil when fixes may be written into r: no tracked file
// has uncommitted changes, or r has no git. An untracked file, such as the
// configuration `init` just wrote, is not in danger. It fails closed: when
// git cannot tell, it returns that error.
func CheckClean(r *repo.Repo) error {
	if !r.HasGit() {
		return nil
	}
	st, err := r.Git("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("cannot tell whether the working tree is clean: %w; pass --allow-dirty to write anyway", err)
	}
	for line := range strings.SplitSeq(st, "\n") {
		if line != "" && !strings.HasPrefix(line, "??") {
			return ErrDirty
		}
	}
	return nil
}
