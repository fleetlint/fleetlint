package catalog_test

import (
	"os"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/fix"
)

// This repository's own dev container is the Go template, unedited.
func TestOwnDevcontainerIsTheTemplate(t *testing.T) {
	t.Parallel()
	want, _, err := catalog.EmbeddedTemplates{}.Compose("go/devcontainer.json", fix.Project{Container: "devcontainer"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../.devcontainer/devcontainer.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf(".devcontainer/devcontainer.json differs from the go template; regenerate it:\n%s", want)
	}
}
