package catalog

import (
	"strings"
	"testing"
)

// loadSourcesBody parses a sources file served as a pinned https reference.
func loadSourcesBody(t *testing.T, body string) (*Sources, error) {
	t.Helper()
	l := Loader{Fetch: func(string) ([]byte, error) { return []byte(body), nil }}
	return l.LoadSources("https://acme.example/sources.yaml#sha256-" + Digest([]byte(body)))
}

const sourcesSigner = "{issuer: https://token.actions.githubusercontent.com, subject: "

func TestSourcesEntryForms(t *testing.T) {
	t.Parallel()
	const oci = "oci://registry.example/acme/catalog:3"
	body := "version: 1\nsigners:\n  - " + sourcesSigner + "top}\ncatalogs:\n" +
		"  plain: fleetlint:minimal\n" +
		"  signed:\n    ref: " + oci + "\n    signers:\n      - " + sourcesSigner + "team}\n    min_version: 3.1.0\n" +
		"  floored: {ref: " + oci + ", min_version: v3}\n"
	src, err := loadSourcesBody(t, body)
	if err != nil {
		t.Fatal(err)
	}
	if got := src.Catalogs["plain"]; got.Ref != "fleetlint:minimal" || got.Signers != nil || got.MinVersion != "" {
		t.Errorf("a string entry is the reference alone: %+v", got)
	}
	signed := src.Catalogs["signed"]
	if signed.Ref != oci || len(signed.Signers) != 1 || signed.Signers[0].Subject != "team" || signed.MinVersion != "3.1.0" {
		t.Errorf("a map entry carries signers and the floor: %+v", signed)
	}
	if got := src.Catalogs["floored"]; got.Ref != oci || got.MinVersion != "v3" || got.Signers != nil {
		t.Errorf("a map entry without signers: %+v", got)
	}
	base := Loader{Signers: src.Signers}
	if got := base.ForEntry(src.Catalogs["plain"]).Signers; len(got) != 1 || got[0].Subject != "top" {
		t.Errorf("an entry without signers uses the file's: %+v", got)
	}
	if got := base.ForEntry(signed).Signers; len(got) != 1 || got[0].Subject != "team" {
		t.Errorf("an entry's signers replace the file's: %+v", got)
	}
}

func TestSourcesEntryValidation(t *testing.T) {
	t.Parallel()
	const oci = "oci://registry.example/acme/catalog:3"
	for name, tc := range map[string]struct{ entry, want string }{
		"no ref":              {"{signers: [" + sourcesSigner + "x}]}", "catalogs.org: ref is required"},
		"floor not semver":    {"{ref: " + oci + ", min_version: latest}", `catalogs.org: min_version "latest" is not a semantic version`},
		"bad signer":          {"{ref: " + oci + ", signers: [{issuer: http://x, subject: y}]}", "catalogs.org: signers[0]"},
		"signers off oci":     {"{ref: fleetlint:minimal, signers: [" + sourcesSigner + "x}]}", "signers apply to oci:// references"},
		"unknown key":         {"{ref: fleetlint:minimal, pin: x}", "pin"},
		"empty string":        {`""`, "catalogs.org: ref is required"},
		"not a reference":     {"{ref: ftp://x/y}", "is not a catalog reference"},
		"remote names a path": {"{ref: org.yaml, min_version: 1.0.0}", "cannot name the local path"},
	} {
		_, err := loadSourcesBody(t, "version: 1\ncatalogs:\n  org: "+tc.entry+"\n")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want %q, got %v", name, tc.want, err)
		}
	}
}

func TestEntryCheckVersion(t *testing.T) {
	t.Parallel()
	floor := Entry{Ref: "oci://registry.example/acme/catalog:1", MinVersion: "1.2.0"}
	for version, want := range map[string]string{
		"1.2.0":       "",
		"v1.10.0":     "",
		"2.0.0":       "",
		"1.1.9":       "catalog org (oci://registry.example/acme/catalog:1) is version 1.1.9, an older version than the sources file allows (min_version 1.2.0); a tag may have been moved back",
		"v1.2.0-rc.1": "older version than the sources file allows",
		"2024.1a":     "not a semantic version",
		"":            "not a semantic version",
	} {
		c := &Catalog{Ref: floor.Ref}
		c.Metadata.Version = version
		err := floor.CheckVersion("org", c)
		switch {
		case want == "" && err != nil:
			t.Errorf("%q: %v", version, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%q: want %q, got %v", version, want, err)
		}
	}
	if err := (Entry{Ref: "x"}).CheckVersion("org", &Catalog{}); err != nil {
		t.Errorf("no floor, no check: %v", err)
	}
}
