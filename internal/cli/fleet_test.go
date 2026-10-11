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
		{"exclude without discover", []string{"--exclude", "x/*", work}, "--exclude needs --discover"},
		{"discover a missing directory", []string{"--discover", filepath.Join(work, "missing")}, "no such file"},
		{"apply without fix", []string{"--apply", work}, "--apply and --rule need --fix"},
		{"rule without fix", []string{"--rule", "repo/gitattributes", work}, "--apply and --rule need --fix"},
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

func TestFleetDiscover(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	repo := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	group := filepath.Join(work, "group")
	if err := os.MkdirAll(group, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repo.Root, filepath.Join(group, "svc")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFiles(t, work, map[string]string{"loose/main.go": "package main\n"})
	out := filepath.Join(work, "report")
	code, stdout, stderr := runCLI(t, work, "fleet", "--discover", work, "--exclude", "report", "--out", out)
	if code != cli.ExitOK || !strings.Contains(stdout, "1 repositories checked") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	md, err := os.ReadFile(filepath.Join(out, "fleet.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| svc | group |", "## Not under version control", filepath.Join(work, "loose")} {
		if !strings.Contains(string(md), want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
}

func TestFleetFix(t *testing.T) {
	t.Parallel()
	messy := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".DS_Store": "x", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n"}).Root
	work := t.TempDir()
	out := filepath.Join(work, "report")
	junk := func() string {
		b, err := os.ReadFile(filepath.Join(out, "fleet.json"))
		if err != nil {
			t.Fatal(err)
		}
		var rep struct {
			Repos []struct {
				Results []struct {
					Rule   string `json:"rule"`
					Status string `json:"status"`
				} `json:"results"`
			} `json:"repos"`
		}
		if err := json.Unmarshal(b, &rep); err != nil {
			t.Fatal(err)
		}
		for _, r := range rep.Repos[0].Results {
			if r.Rule == "repo/no-tracked-junk" {
				return r.Status
			}
		}
		return ""
	}

	code, stdout, stderr := runCLI(t, work, "fleet", "--fix", "--rule", "repo/no-tracked-junk", "--out", out, messy)
	if code != cli.ExitOK || !strings.Contains(stdout, "untrack .DS_Store") || !strings.Contains(stdout, "2 change(s) planned in 1 repositories") {
		t.Fatalf("dry run: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if got := junk(); got != "fail" {
		t.Fatalf("a dry run changes nothing: %s", got)
	}

	code, stdout, stderr = runCLI(t, work, "fleet", "--fix", "--apply", "--rule", "repo/no-tracked-junk", "--out", out, messy)
	if code != cli.ExitOK || !strings.Contains(stdout, "applied 2 change(s) in 1 repositories") {
		t.Fatalf("apply: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if got := junk(); got != "pass" {
		t.Fatalf("the report shows the state after the fix: %s", got)
	}
}

func TestFleetFixPassesOverWhatItCannotPlan(t *testing.T) {
	t.Parallel()
	commented := testutil.GitFixture(t, map[string]string{
		"package.json": `{"name": "x", "scripts": {"build": "tsc"}}`, "tsconfig.json": "{\n  // loose for now\n  \"compilerOptions\": {}\n}\n",
	}).Root
	work := t.TempDir()
	code, stdout, stderr := runCLI(t, work, "fleet", "--fix", "--out", filepath.Join(work, "report"), commented)
	if code != cli.ExitOK || !strings.Contains(stdout, "skipped lint/node-config") || !strings.Contains(stdout, "0 could not be checked") {
		t.Fatalf("a rule fix cannot plan is skipped, the repository still checked: exit %d\n%s\n%s", code, stdout, stderr)
	}
}

func TestFleetFileEntryAdjustsADiscoveredRepository(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	repo := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	svc := filepath.Join(work, "group", "svc")
	if err := os.MkdirAll(filepath.Dir(svc), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repo.Root, svc); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFiles(t, work, map[string]string{"fleet.yaml": "version: 1\nrepos:\n  - {path: group/svc, name: svc, tier: 3}\n"})
	out := filepath.Join(work, "report")
	code, stdout, stderr := runCLI(t, work, "fleet", "--repos", filepath.Join(work, "fleet.yaml"), "--discover", work, "--exclude", "report", "--out", out)
	if code != cli.ExitOK || !strings.Contains(stdout, "1 repositories checked") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	md, _ := os.ReadFile(filepath.Join(out, "fleet.md"))
	if !strings.Contains(string(md), "| svc | group |") || !strings.Contains(string(md), "| 3 |") {
		t.Errorf("the entry keeps its tier and takes the discovered group:\n%s", md)
	}
}
