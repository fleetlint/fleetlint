package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// runCommandRule evaluates one command rule backed by the given script under --deep.
func runCommandRule(t *testing.T, script string) model.Result {
	t.Helper()
	cfg := "version: 1\nextends: [fleetlint:minimal]\nrules:\n  - id: repo/custom\n    kind: command\n    severity: warning\n    run: ./scripts/check\n    message: x\n    fix: {human: h}\n"
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", "scripts/check": "#!/bin/sh\n" + script, config.FileName: cfg})
	if err := os.Chmod(filepath.Join(r.Root, "scripts", "check"), 0o755); err != nil {
		t.Fatal(err)
	}
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.EvaluateWith(context.Background(), r, eff, engine.Options{Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	return status(t, out, "repo/custom")
}

func TestCommandRuleOutputHandling(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 120)
	cases := []struct {
		name, script string
		wantStatus   model.Status
		wantErr      []string
	}{
		{
			name: "stdout over the limit is a rule error, not a pass",
			// 1 MiB of valid findings plus one more line; nothing of it may be parsed.
			script:     "yes '{\"message\":\"m\"}' | head -c 1100000\nexit 1\n",
			wantStatus: model.StatusError, wantErr: []string{"more than 1048576 bytes"},
		},
		{
			name:       "non-JSON output names the offending line, shortened",
			script:     "echo " + long + "\nexit 1\n",
			wantStatus: model.StatusError, wantErr: []string{"JSON lines", `got "` + long[:80] + `..."`},
		},
		{
			name:       "a short non-JSON line is quoted whole",
			script:     "echo notjson\nexit 1\n",
			wantStatus: model.StatusError, wantErr: []string{`got "notjson"`},
		},
		{
			name:       "an unexpected exit code reports the last stderr line",
			script:     "echo 'first line' >&2\necho 'tool crashed' >&2\nexit 3\n",
			wantStatus: model.StatusError, wantErr: []string{"exit status 3", "tool crashed"},
		},
		{
			name:       "stderr over the limit is dropped silently",
			script:     "yes 'noise' | head -c 1100000 >&2\nexit 0\n",
			wantStatus: model.StatusPass,
		},
		{
			name:       "exit 1 with no findings passes",
			script:     "exit 1\n",
			wantStatus: model.StatusPass,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := runCommandRule(t, c.script)
			if s.Status != c.wantStatus {
				t.Fatalf("status %s with %d findings, want %s: %s", s.Status, len(s.Findings), c.wantStatus, s.Err)
			}
			for _, want := range c.wantErr {
				if !strings.Contains(s.Err, want) {
					t.Errorf("error %q lacks %q", s.Err, want)
				}
			}
			if s.Status == model.StatusError && strings.Contains(s.Err, "first line") {
				t.Errorf("only the last stderr line is reported: %q", s.Err)
			}
		})
	}
}
