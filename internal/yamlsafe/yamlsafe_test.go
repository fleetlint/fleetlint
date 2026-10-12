package yamlsafe_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/yamlsafe"
)

// bomb is the classic billion-laughs shape: each level lists the previous
// one ten times.
func bomb(levels int) string {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i <= levels; i++ {
		fmt.Fprintf(&b, "a%d: &a%d [", i, i)
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "*a%d", i-1)
		}
		b.WriteString("]\n")
	}
	return b.String()
}

func TestAliasBombIsRefusedQuickly(t *testing.T) {
	t.Parallel()
	start := time.Now()
	var v any
	if err := yamlsafe.Unmarshal([]byte(bomb(9)), &v); err == nil {
		t.Fatal("a document expanding to 10^10 nodes was decoded")
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("took %s", took)
	}
}

func TestAnchorsStayUsable(t *testing.T) {
	t.Parallel()
	doc := "defaults: &d {runs-on: ubuntu-latest, timeout: 5}\njobs:\n  a: *d\n  b:\n    <<: *d\n    timeout: 9\n" + bomb(2)
	var v map[string]any
	if err := yamlsafe.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatal(err)
	}
	if v["jobs"] == nil {
		t.Fatal("not decoded")
	}
}
