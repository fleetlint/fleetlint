package artifacts_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fleetlint/fleetlint/internal/artifacts"
	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

const imageDef = "id: image\ntitle: Container image\nsummary: a push\ndetect: >\n  tagged_workflows().filter(w, steps(w).exists(s, s.run.contains(\"docker push\")))\n"

func TestLoadReadsAndRefusesDefinitions(t *testing.T) {
	t.Parallel()
	defs, err := artifacts.Load(fstest.MapFS{
		"artifacts/image.yaml": {Data: []byte(imageDef)},
		"artifacts/npm.yaml":   {Data: []byte("id: npm\ntitle: npm package\ndetect: >\n  tagged_workflows().filter(w, steps(w).exists(s, s.run.contains(\"npm publish\")))\n")},
	})
	if err != nil || len(defs) != 2 || defs[0].ID != "image" || defs[1].Summary != "" {
		t.Fatalf("load: %v %+v", err, defs)
	}
	if ids := artifacts.IDs(defs); strings.Join(ids, ",") != "image,npm" {
		t.Errorf("ids: %v", ids)
	}
	if _, ok := artifacts.Find(defs, "npm"); !ok {
		t.Error("find npm")
	}
	if defs, err := artifacts.Load(fstest.MapFS{"stacks/go.yaml": {Data: []byte("id: go\n")}}); err != nil || defs != nil {
		t.Errorf("no artifacts/ is no definitions: %v %v", defs, err)
	}
	for name, body := range map[string]string{
		"id not the file name": "id: docker\ntitle: t\ndetect: tagged_workflows()\n",
		"no title":             "id: image\ndetect: tagged_workflows()\n",
		"no detect":            "id: image\ntitle: t\n",
		"detect not a list":    "id: image\ntitle: t\ndetect: \"true\"\n",
		"unknown key":          "id: image\ntitle: t\ndetect: tagged_workflows()\nmarkers: []\n",
	} {
		if _, err := artifacts.Load(fstest.MapFS{"artifacts/image.yaml": {Data: []byte(body)}}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDetectFindsPublishingWorkflowsOrTakesThePin(t *testing.T) {
	t.Parallel()
	defs, err := artifacts.Load(fstest.MapFS{"artifacts/image.yaml": {Data: []byte(imageDef)}})
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.Fixture(t, map[string]string{
		".github/workflows/release.yml": "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    steps:\n      - run: docker push ghcr.io/x/y\n",
		".github/workflows/ci.yml":      "on: push\njobs:\n  r:\n    steps:\n      - run: docker push ghcr.io/x/y\n",
	})
	env, err := celenv.New(context.Background(), r, facts.Facts{})
	if err != nil {
		t.Fatal(err)
	}
	found, err := artifacts.Detect(env, defs, nil)
	if err != nil || len(found) != 1 || found[0].Kind != "image" || strings.Join(found[0].Workflows, ",") != ".github/workflows/release.yml" {
		t.Fatalf("detect: %v %+v", err, found)
	}
	pinned := []facts.Artifact{{Kind: "npm"}, {Kind: "image", Name: "ghcr.io/x/y", Workflows: []string{".github/workflows/ci.yml"}}}
	found, err = artifacts.Detect(env, defs, &pinned)
	if err != nil || len(found) != 2 || found[0].Kind != "npm" || found[0].Workflows != nil || found[1].Name != "ghcr.io/x/y" || len(found[1].Workflows) != 1 {
		t.Fatalf("pinned: %v %+v", err, found)
	}
	none := []facts.Artifact{}
	if found, err = artifacts.Detect(env, defs, &none); err != nil || len(found) != 0 {
		t.Fatalf("empty pin: %v %+v", err, found)
	}
}

// A kind that names its instances yields one artifact per name, with every
// file that publishes it; a file that names none publishes one named after
// itself.
func TestDetectNamesInstances(t *testing.T) {
	t.Parallel()
	def := imageDef + "instances: >\n  findall(text(item), \"docker push ([^\\\\s:@]+)\")\n"
	defs, err := artifacts.Load(fstest.MapFS{"artifacts/image.yaml": {Data: []byte(def)}})
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.Fixture(t, map[string]string{
		".github/workflows/release.yml": "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    steps:\n      - run: docker push ghcr.io/x/y:1\n      - run: docker push ghcr.io/x/z@sha256:ab\n",
		".github/workflows/nightly.yml": "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    steps:\n      - run: docker push ghcr.io/x/y:nightly\n",
		".github/workflows/other.yml":   "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    steps:\n      - run: docker push\n",
	})
	env, err := celenv.New(context.Background(), r, facts.Facts{})
	if err != nil {
		t.Fatal(err)
	}
	found, err := artifacts.Detect(env, defs, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(found))
	for _, a := range found {
		got = append(got, a.String()+" <- "+strings.Join(a.Workflows, ","))
	}
	want := []string{
		"image ghcr.io/x/y <- .github/workflows/nightly.yml,.github/workflows/release.yml",
		"image .github/workflows/other.yml <- .github/workflows/other.yml",
		"image ghcr.io/x/z <- .github/workflows/release.yml",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("instances:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := artifacts.Load(fstest.MapFS{"artifacts/image.yaml": {Data: []byte(imageDef + "instances: \"true\"\n")}}); err == nil {
		t.Error("instances must be a list expression")
	}
}
