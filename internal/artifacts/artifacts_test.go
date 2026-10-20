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
	pinned := []string{"npm"}
	found, err = artifacts.Detect(env, defs, &pinned)
	if err != nil || len(found) != 1 || found[0].Kind != "npm" || found[0].Workflows != nil {
		t.Fatalf("pinned: %v %+v", err, found)
	}
	none := []string{}
	if found, err = artifacts.Detect(env, defs, &none); err != nil || len(found) != 0 {
		t.Fatalf("empty pin: %v %+v", err, found)
	}
}
