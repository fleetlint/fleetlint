//go:build unix

package proc_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/proc"
)

// A timeout ends the run even when the command left a background process
// that holds its output open.
func TestTimeoutKillsTheGroup(t *testing.T) {
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
