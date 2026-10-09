package fleet_test

import (
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
