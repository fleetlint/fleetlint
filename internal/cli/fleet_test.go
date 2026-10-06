package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/cli"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestFleetSpecFromFileAndPaths(t *testing.T) {
	t.Parallel()
	listed := testutil.GitFixture(t, map[string]string{"go.mod": "module listed\n"}).Root
	given := testutil.GitFixture(t, map[string]string{"go.mod": "module given\n"}).Root
	work := t.TempDir()
	testutil.WriteFiles(t, work, map[string]string{"fleet.yaml": "version: 1\nrepos:\n  - path: " + listed + "\n    name: listed\n"})
	out := filepath.Join(work, "report")

	code, stdout, stderr := runCLI(t, work, "fleet", "--repos", filepath.Join(work, "fleet.yaml"), "--out", out, given)
	if code != cli.ExitOK || !strings.Contains(stdout, "2 repositories checked") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "fleet.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Repos []struct {
			Name string `json:"name"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(rep.Repos))
	for _, r := range rep.Repos {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	// A path argument is named after its directory, a fleet entry keeps its name.
	want := []string{"listed", filepath.Base(given)}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("repositories %v, want %v", names, want)
	}
}

func TestFleetSpecErrors(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	testutil.WriteFiles(t, work, map[string]string{"bad.yaml": "version: 2\nrepos: []\n"})
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"nothing to check", nil, "no repositories"},
		{"missing fleet file", []string{"--repos", filepath.Join(work, "missing.yaml")}, "no such file"},
		{"invalid fleet file", []string{"--repos", filepath.Join(work, "bad.yaml")}, "version must be 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			code, _, stderr := runCLI(t, work, append([]string{"fleet", "--out", t.TempDir()}, c.args...)...)
			if code != cli.ExitConfig || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d, stderr %q; want exit %d mentioning %q", code, stderr, cli.ExitConfig, c.want)
			}
		})
	}
}
