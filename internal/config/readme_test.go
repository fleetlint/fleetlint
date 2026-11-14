package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
)

// yamlBlock is a fenced yaml block of a Markdown file.
var yamlBlock = regexp.MustCompile("(?ms)^```yaml\n(.*?)^```$")

// Every configuration example in the README loads, offline.
func TestREADMEConfigurationExamplesLoad(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range yamlBlock.FindAllStringSubmatch(string(readme), -1) {
		if !strings.HasPrefix(m[1], "version: 1\n") {
			continue
		}
		n++
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(m[1]), 0o600); err != nil {
			t.Fatal(err)
		}
		eff, err := config.Load(dir, config.Options{Now: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Loader: catalog.Loader{}})
		if err != nil {
			t.Errorf("README example %d does not load: %v\n%s", n, err, m[1])
			continue
		}
		if len(eff.Deprecations) > 0 {
			t.Errorf("README example %d uses deprecated forms: %v", n, eff.Deprecations)
		}
	}
	if n == 0 {
		t.Fatal("no configuration example found in README.md")
	}
}
