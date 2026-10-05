package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// Command rules are the escape hatch for logic the catalog cannot express.
// They run only from the repository's own configuration (the loader rejects
// them from untrusted catalogs) and under the constraints below, so a check
// run can never be hijacked by a file that merely appears in the tree.

const (
	commandTimeout = 60 * time.Second
	maxOutput      = 1 << 20
)

// limitedBuffer keeps at most limit bytes and records that more arrived.
type limitedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	room := l.limit - l.Len()
	if room <= 0 {
		l.truncated = true
		return len(p), nil
	}
	if len(p) > room {
		l.truncated = true
		p = p[:room]
	}
	_, _ = l.Buffer.Write(p) // bytes.Buffer.Write never returns an error
	return len(p), nil
}

// commandInput is what the executable receives on stdin.
type commandInput struct {
	Rule   string         `json:"rule"`
	Facts  facts.Facts    `json:"facts"`
	Scope  string         `json:"scope"`
	Root   string         `json:"root"`
	Params map[string]any `json:"params,omitempty"`
}

// commandFinding is one JSON line on stdout.
type commandFinding struct {
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
}

func evalCommand(ctx context.Context, r *repo.Repo, f facts.Facts, rule model.Rule) ([]model.Finding, error) {
	path, err := commandPath(r, rule.Run) //nolint:contextcheck // git lookups run under the repository's own bounded context
	if err != nil {
		return nil, err
	}
	input, err := json.Marshal(commandInput{Rule: rule.ID, Facts: f, Scope: r.Scope, Root: r.ScopeRoot(), Params: rule.Params})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path) //nolint:gosec // path is a tracked, executable, user-owned file inside the repository, verified by commandPath
	cmd.Dir = r.ScopeRoot()
	cmd.Env = scrubbedEnv()
	cmd.Stdin = bytes.NewReader(input)
	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	if stdout.truncated {
		return nil, fmt.Errorf("%s: produced more than %d bytes of output", rule.Run, maxOutput)
	}
	findings, parseErr := parseFindings(stdout.Bytes())
	if parseErr != nil {
		return nil, parseErr
	}
	var exit *exec.ExitError
	switch {
	case runErr == nil, errors.As(runErr, &exit) && exit.ExitCode() == 1:
		return findings, nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, fmt.Errorf("%s: timed out after %s", rule.Run, commandTimeout)
	default:
		return nil, fmt.Errorf("%s: %w: %s", rule.Run, runErr, lastLine(stderr.String()))
	}
}

// commandPath validates the executable: relative, inside the repository,
// git-tracked, executable, a regular file, and not writable by others.
func commandPath(r *repo.Repo, run string) (string, error) {
	if strings.ContainsAny(run, " \t\n") || filepath.IsAbs(run) || !strings.HasPrefix(run, "./") {
		return "", fmt.Errorf("run %q must be a relative path to a tracked executable (./scripts/check), without arguments", run)
	}
	rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(run)), "./")
	tracked := false
	for _, t := range r.TrackedInScope() {
		if t == rel {
			tracked = true
			break
		}
	}
	if !tracked {
		return "", fmt.Errorf("run %q is not a git-tracked file", run)
	}
	abs, err := r.Abs(rel)
	if err != nil {
		return "", err
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("run %q is a symlink", run)
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("run %q is not a regular file", run)
	}
	if st.Mode()&0o111 == 0 {
		return "", fmt.Errorf("run %q is not executable", run)
	}
	if st.Mode()&0o002 != 0 {
		return "", fmt.Errorf("run %q is world-writable", run)
	}
	if dir, derr := os.Stat(filepath.Dir(abs)); derr == nil && dir.Mode()&0o002 != 0 {
		return "", fmt.Errorf("run %q is in a world-writable directory", run)
	}
	return abs, nil
}

func scrubbedEnv() []string {
	keep := []string{"PATH", "HOME", "LANG", "LC_ALL", "TMPDIR", "TERM"}
	env := make([]string, 0, len(keep)+1)
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return append(env, "FLEETLINT=1")
}

func parseFindings(out []byte) ([]model.Finding, error) {
	var findings []model.Finding
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var cf commandFinding
		if err := json.Unmarshal([]byte(line), &cf); err != nil {
			return nil, fmt.Errorf("stdout must be JSON lines {message, path?, line?}; got %q", truncate(line, 80))
		}
		if cf.Message == "" {
			return nil, errors.New("finding without message")
		}
		findings = append(findings, model.Finding{Message: cf.Message, Path: cf.Path, Line: cf.Line, Evidence: "command"})
	}
	return findings, sc.Err()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
