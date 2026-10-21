package config_test

import (
	"io/fs"
	"strings"
	"testing"

	data "github.com/fleetlint/catalog"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/catalog/fetch"
	"github.com/fleetlint/fleetlint/internal/config"
)

// catalogTree is the catalog module packed the way the publish workflow
// packs it: every file of presets/, rules/, stacks/, templates/ and
// aliases.yaml, titled with its path.
func catalogTree(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{"aliases.yaml": data.Aliases}
	for _, fsys := range []fs.FS{data.Presets, data.Rules, data.Stacks, data.Artifacts, data.Templates} {
		err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(fsys, p)
			files[p] = b
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func ociLoader(t *testing.T, served map[string]*fetch.OCIArtifact) catalog.Loader {
	t.Helper()
	return catalog.Loader{FetchOCI: func(o fetch.OCIRef) (*fetch.OCIArtifact, error) {
		if art, ok := served[o.Repository]; ok {
			return art, nil
		}
		t.Fatalf("unexpected fetch of %s", o.Repository)
		return nil, nil
	}}
}

func TestCatalogVersionFromAnArtifact(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("d", 64)
	art := &fetch.OCIArtifact{Digest: digest, Files: catalogTree(t)}
	l := ociLoader(t, map[string]*fetch.OCIArtifact{"registry.example/fleetlint/catalog-a": art})
	dir := writeAll(t, map[string]string{config.FileName: "version: 1\ncatalog: {oci: \"registry.example/fleetlint/catalog-a:v0.1.0@sha256:" + digest + "\"}\nextends: [fleetlint:recommended]\noverrides:\n  lint/go-config: {severity: error}\n"})
	eff, err := config.Load(dir, config.Options{Loader: l})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(eff.Source.Version, "v0.1.0 (sha256:dddddddddddd)") || !eff.Source.Pinned {
		t.Errorf("the run names the artifact's version: %q", eff.Source.Version)
	}
	r, ok := ruleByID(eff, "lint/configured/go")
	if !ok || r.SelectedBy != "fleetlint-recommended@"+eff.Source.Version {
		t.Errorf("presets, library and aliases come from the artifact: %+v %v", r.SelectedBy, ok)
	}
	if len(eff.Stacks) < 10 {
		t.Errorf("stacks come from the artifact: %d", len(eff.Stacks))
	}
}

func TestArtifactPinsAreVerified(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("e", 64)
	art := &fetch.OCIArtifact{Digest: digest, Files: catalogTree(t)}
	served := map[string]*fetch.OCIArtifact{"registry.example/fleetlint/catalog-b": art, catalog.DefaultOCI: art}
	for name, tc := range map[string]struct{ pin, want string }{
		"a tag of another registry needs signers":   {"registry.example/fleetlint/catalog-b:v1", "needs signers"},
		"a moved digest is refused":                 {"registry.example/fleetlint/catalog-b@sha256:" + strings.Repeat("f", 64), "returned digest"},
		"the published catalog needs its signature": {catalog.DefaultOCI + ":v0.1.0", "no sigstore bundle"},
		"oci names the version alone":               {"x\", version: v1, y: \"", ""},
	} {
		cfg := "version: 1\ncatalog: {oci: \"" + tc.pin + "\"}\n"
		if tc.want == "" {
			cfg = "version: 1\ncatalog: {oci: registry.example/a/b:v1, version: v1}\n"
			tc.want = "drop version and repo"
		}
		dir := writeAll(t, map[string]string{config.FileName: cfg})
		if _, err := config.Load(dir, config.Options{Loader: ociLoader(t, served)}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestExtendingACatalogVersionArtifactExplainsThePin(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	art := &fetch.OCIArtifact{Digest: digest, Files: catalogTree(t)}
	l := ociLoader(t, map[string]*fetch.OCIArtifact{"registry.example/fleetlint/catalog-c": art})
	dir := writeAll(t, map[string]string{config.FileName: "version: 1\nextends: [\"oci://registry.example/fleetlint/catalog-c@sha256:" + digest + "\"]\n"})
	if _, err := config.Load(dir, config.Options{Loader: l}); err == nil || !strings.Contains(err.Error(), "pin it with `catalog: {oci:") {
		t.Errorf("extending a catalog version says how to pin it: %v", err)
	}
}

func TestPublishedTagIsTheGitTag(t *testing.T) {
	t.Parallel()
	git := &catalog.Pin{Version: "v0.1.0"}
	for pin, same := range map[string]bool{
		catalog.DefaultOCI + ":v0.1.0":                                   true,
		catalog.DefaultOCI + ":v0.1.0@sha256:" + strings.Repeat("b", 64): true,
		catalog.DefaultOCI + ":v0.2.0":                                   false,
		"registry.example/mirror/catalog:v0.1.0":                         false,
	} {
		if got := (&catalog.Pin{OCI: pin}).Same(git); got != same {
			t.Errorf("%s same as the git tag v0.1.0: %v, want %v", pin, got, same)
		}
	}
}

// A sources-file entry carries its own signers and a version floor: the
// alias's signers verify the artifact, and a catalog older than the floor
// fails the run.
func TestAliasSignersAndVersionFloor(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("c", 64)
	body := strings.Replace(layerCatalog("acme", "org/x", "error"), "version: 1.0.0", "version: 1.2.0", 1)
	art := &fetch.OCIArtifact{Digest: digest, Files: map[string][]byte{"catalog.yaml": []byte(body)}}
	l := ociLoader(t, map[string]*fetch.OCIArtifact{"registry.example/acme/catalog": art})
	tag, pinned := "oci://registry.example/acme/catalog:v1", "oci://registry.example/acme/catalog@sha256:"+digest
	sources := "version: 1\ncatalogs:\n" +
		"  signed: {ref: \"" + tag + "\", signers: [{issuer: https://token.actions.githubusercontent.com, subject: x}]}\n" +
		"  bare: \"" + tag + "\"\n" +
		"  floored: {ref: \"" + pinned + "\", min_version: v1.2.0}\n" +
		"  toolow: {ref: \"" + pinned + "\", min_version: 2.0.0}\n"
	for alias, want := range map[string]string{
		"signed":  "no sigstore bundle",
		"bare":    "need signers in the sources file",
		"floored": "",
		"toolow":  "catalog toolow (" + pinned + ") is version 1.2.0, an older version than the sources file allows (min_version 2.0.0); a tag may have been moved back",
	} {
		dir := writeAll(t, map[string]string{"s.yaml": sources, config.FileName: "version: 1\nsources: s.yaml\nextends: [" + alias + "]\n"})
		eff, err := config.Load(dir, config.Options{Loader: l})
		switch {
		case want == "" && err != nil:
			t.Errorf("%s: %v", alias, err)
		case want == "" && (len(eff.Catalogs) == 0 || eff.Catalogs[0].Alias != alias):
			t.Errorf("%s: the catalog is loaded under its alias: %+v", alias, eff.Hierarchy())
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: want %q, got %v", alias, want, err)
		}
	}
}
