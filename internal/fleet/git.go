package fleet

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/proc"
)

const gitTimeout = 5 * time.Minute

// gitRun runs git in dir with extra environment entries (credentials go
// here, never in args, which error messages quote).
func gitRun(ctx context.Context, dir string, env []string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := proc.Command(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = proc.GitEnv(env...)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return nil
}
