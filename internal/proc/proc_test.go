package proc_test

import (
	"bytes"
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/proc"
)

// A timeout ends the run even when the command left a background process
// that holds its output open.
func TestTimeoutKillsTheGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are unix")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	cmd := proc.Command(ctx, "sh", "-c", "sleep 30 & sleep 30")
	var out bytes.Buffer
	cmd.Stdout = &out
	start := time.Now()
	_ = cmd.Run()
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("run took %s", took)
	}
}

func TestSafeEnvDropsCredentials(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "x")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "x")
	t.Setenv("GOPRIVATE_TOKEN", "x")
	t.Setenv("GOFLAGS", "-mod=mod")
	env := strings.Join(proc.SafeEnv("FLEETLINT=1"), "\n")
	for _, gone := range []string{"GITHUB_TOKEN", "AWS_SECRET", "GOPRIVATE_TOKEN"} {
		if strings.Contains(env, gone) {
			t.Errorf("%s is passed on", gone)
		}
	}
	for _, kept := range []string{"PATH=", "GOFLAGS=-mod=mod", "FLEETLINT=1"} {
		if !strings.Contains(env, kept) {
			t.Errorf("%s is missing", kept)
		}
	}
}

func TestGitEnvDropsRedirects(t *testing.T) {
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GIT_INDEX_FILE", "/elsewhere/index")
	t.Setenv("GIT_CONFIG_KEY_0", "core.fsmonitor")
	env := proc.GitEnv("A=1")
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_INDEX_FILE=") || strings.HasPrefix(kv, "GIT_CONFIG_KEY_0=") {
			t.Errorf("%s is passed on", kv)
		}
	}
	if !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") || !slices.Contains(env, "A=1") {
		t.Error("prompt off and extras are set")
	}
}
