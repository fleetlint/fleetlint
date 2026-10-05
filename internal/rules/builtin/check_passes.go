package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/rules"
)

// checkPasses runs the repository's own `check` and measures it against the
// baseline's budget. It is the one rule that executes repository code, so it
// only runs under --deep and only through the task runner the repo declares.
type checkPasses struct{}

func (checkPasses) ID() string { return "quality/check-passes" }

// Check is the non-deep path: report that deep mode is needed.
func (checkPasses) Check(rules.Context) (rules.Outcome, error) {
	return rules.Outcome{}, rules.ErrNeedsDeep
}

const (
	defaultCheckBudget = 10 * time.Minute
	checkTimeout       = 20 * time.Minute
)

func (c checkPasses) CheckDeep(ctx context.Context, rc rules.Context) ([]model.Finding, string, error) {
	if !rc.Deep {
		return nil, "", rules.ErrNeedsDeep
	}
	cmdline, ok := checkCommand(rc.Facts.TaskRunner.Kind)
	if !ok {
		return []model.Finding{{Message: "no task runner with a check target to run"}}, "", nil
	}
	budget, err := durationParam(rc.Params, "budget", defaultCheckBudget)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...) //nolint:gosec // fixed task-runner invocations, see checkCommand
	cmd.Dir = rc.Repo.ScopeRoot()
	cmd.Env = append(os.Environ(), "CI=true", "NO_COLOR=1")
	out := &tailBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	runErr := cmd.Run()
	took := time.Since(start).Round(time.Second)
	evidence := fmt.Sprintf("%s in %s", strings.Join(cmdline, " "), took)
	var findings []model.Finding
	if runErr != nil {
		findings = append(findings, model.Finding{
			Message:  fmt.Sprintf("`%s` failed after %s: %s", strings.Join(cmdline, " "), took, lastLines(out.String(), 5)),
			Evidence: "failed",
		})
	}
	if took > budget {
		findings = append(findings, model.Finding{
			Message:  fmt.Sprintf("`%s` took %s, over the %s budget", strings.Join(cmdline, " "), took, budget),
			Evidence: "slow",
		})
	}
	return findings, evidence, nil
}

// tailBuffer keeps only the last limit bytes of output: enough to explain a
// failure, never enough to exhaust memory on a chatty build.
type tailBuffer struct {
	bytes.Buffer
	limit int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	_, _ = t.Buffer.Write(p) // bytes.Buffer.Write never returns an error
	if t.Len() > t.limit {
		excess := t.Len() - t.limit
		rest := make([]byte, t.limit)
		copy(rest, t.Bytes()[excess:])
		t.Reset()
		_, _ = t.Buffer.Write(rest)
	}
	return len(p), nil
}

func checkCommand(kind string) ([]string, bool) {
	switch kind {
	case repo.RunnerMake, repo.RunnerJust, repo.RunnerTask:
		return []string{repo.RunnerCmd(kind), "check"}, true
	case repo.RunnerNPM:
		return []string{"npm", "run", "check"}, true
	case repo.RunnerGradle:
		return []string{"./gradlew", "check", "--quiet"}, true
	}
	return nil, false
}

func durationParam(params map[string]any, key string, def time.Duration) (time.Duration, error) {
	v, ok := params[key]
	if !ok {
		return def, nil
	}
	s, isStr := v.(string)
	if !isStr {
		return 0, fmt.Errorf("param %s must be a duration string like 10m", key)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("param %s: %w", key, err)
	}
	return d, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, " | "))
}
