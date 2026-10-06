package cli_test

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
)

// The files other repositories consume directly must at least be valid
// YAML: nothing else in this repository reads them.
func TestIntegrationFilesParse(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"../../action/action.yml", "../../.pre-commit-hooks.yaml", "../../.goreleaser.yaml",
		"../../.github/workflows/check.yml", "../../.github/workflows/release.yml", "../../.github/workflows/mutate.yml",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}
