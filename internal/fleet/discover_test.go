package fleet_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fleetlint/fleetlint/internal/fleet"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestDiscoverReposGroupsAndUnversioned(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, d := range []string{"solo/.git", "suite/core/.git", "suite/docs/.git", "other/docs/.git", "vendored/lib/.git", "solo/sub/.git", "app/node_modules/x/.git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	testutil.WriteFiles(t, root, map[string]string{
		"script/run.py": "print(1)\n", "script/lib/more.py": "x\n", "app/package.json": "{}\n", "notes/readme.txt": "x\n",
		"suite/README.md": "x\n",
	})
	found, err := fleet.Discover(root, fleet.DiscoverDepth, []string{"vendored/**"})
	if err != nil {
		t.Fatal(err)
	}
	type got struct{ name, group string }
	repos := make([]got, 0, len(found.Repos))
	for _, e := range found.Repos {
		repos = append(repos, got{e.Name, e.Group})
	}
	want := []got{{"other-docs", "other"}, {"solo", ""}, {"core", "suite"}, {"suite-docs", "suite"}}
	if !slices.Equal(repos, want) {
		t.Errorf("repos %v, want %v (a repository is not descended into; colliding names keep their parents)", repos, want)
	}
	if want := []string{"app", "script"}; !slices.Equal(found.Unversioned, want) {
		t.Errorf("unversioned %v, want %v (topmost project directories only, none that hold repositories)", found.Unversioned, want)
	}
	if _, err := fleet.Discover(root, 4, []string{"[bad"}); err == nil {
		t.Error("an invalid exclude glob is an error")
	}
}

func TestDiscoverRootRepository(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	found, err := fleet.Discover(r.Root, fleet.DiscoverDepth, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Repos) != 1 || found.Repos[0].Path != r.Root || found.Repos[0].Group != "" {
		t.Fatalf("a root without nested repositories is the repository: %+v", found.Repos)
	}
}

// A discovered or positional repository whose directory name is unsafe in
// a file name gets one safe name, the same in actions/ and sarif/.
func TestAssembledNamesAreSafeFileNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "we#ird? name")
	testutil.WriteFiles(t, dir, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n"})
	testutil.Git(t, dir, "init", "-q", "-b", "main")
	testutil.Git(t, dir, "add", "-A")
	testutil.Git(t, dir, "commit", "-q", "-m", "chore: fixture", "--no-verify")
	for name, src := range map[string]fleet.Sources{"discover": {Discover: []string{root}}, "path": {Paths: []string{dir}}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec, err := fleet.Assemble(context.Background(), src)
			if err != nil {
				t.Fatal(err)
			}
			if len(spec.Repos) != 1 || spec.Repos[0].Name != "we-ird-name" {
				t.Fatalf("entries %+v, want one named we-ird-name", spec.Repos)
			}
			rep, err := fleet.Run(context.Background(), spec, fleet.Options{})
			if err != nil || rep.Repos[0].Err != "" {
				t.Fatalf("run: %v %+v", err, rep.Repos)
			}
			out := t.TempDir()
			if err := fleet.Write(out, rep); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"actions/we-ird-name.md", "actions/we-ird-name.html", "sarif/we-ird-name.sarif"} {
				if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

// reportFile names the per-repository files of a report built from
// entries that were never normalised the same safe way.
func TestWriteMakesUnnormalisedNamesSafe(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	if err := fleet.Write(out, &fleet.Report{Repos: []fleet.RepoReport{{Name: "a/b c"}}}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"actions/a-b-c.md", "actions/a-b-c.html", "sarif/a-b-c.sarif"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
			t.Error(err)
		}
	}
}
