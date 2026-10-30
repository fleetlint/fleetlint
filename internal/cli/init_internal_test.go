package cli

import "testing"

// A directory name with a newline or a quote would otherwise end the YAML
// value and write a live key into .fleetlint.yaml; a plain path stays
// readable.
func TestYAMLScalarKeepsOneValue(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"api":                              "api",
		".github/workflows/release.yml":    ".github/workflows/release.yml",
		"x\nextends: [evil]":               `"x\nextends: [evil]"`,
		`a"b`:                              `"a\"b"`,
		"-x":                               `"-x"`,
		"yes":                              `"yes"`,
		"ghcr.io/${{ github.repository }}": `"ghcr.io/${{ github.repository }}"`,
	} {
		if got := yamlScalar(in); got != want {
			t.Errorf("yamlScalar(%q) = %s, want %s", in, got, want)
		}
	}
}
