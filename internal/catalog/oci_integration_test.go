package catalog

import (
	"context"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestOCIIntegration loads a real catalog version from the registry and
// verifies cosign's signature through the public sigstore root: the TUF
// fetch, the JSON bundle, SCT checking and the publish workflow's identity.
// It needs the network and runs weekly in CI (check.yml), not in make check:
//
//	FLEETLINT_OCI_TEST_REF=ghcr.io/fleetlint/catalog:v0.3.0 go test -run TestOCIIntegration ./internal/catalog/
func TestOCIIntegration(t *testing.T) {
	ref := os.Getenv("FLEETLINT_OCI_TEST_REF")
	if ref == "" || testing.Short() {
		t.Skip("set FLEETLINT_OCI_TEST_REF to a signed catalog version, e.g. ghcr.io/fleetlint/catalog:v0.3.0")
	}
	if !requireSCT {
		t.Fatal("the real path must demand a signed certificate timestamp")
	}
	ociSources.Delete(ref)
	ctx := context.Background()

	// A signature by another identity is refused before any file is read.
	other := RemoteLoader(ctx)
	other.Signers = []Signer{{Issuer: fleetlintSigner.Issuer, Subject: "https://github.com/evil/catalog/.github/workflows/publish.yml@refs/tags/v0.0.0"}}
	if _, err := other.PinOCI(ref); err == nil || !strings.Contains(err.Error(), "no attached signature is valid") {
		t.Fatalf("another identity is refused: %v", err)
	}

	// No signers: the default repository is verified against its publish workflow.
	l := RemoteLoader(ctx)
	src, err := l.PinOCI(ref)
	if err != nil {
		t.Fatalf("load %s: %v", ref, err)
	}
	if !src.Pinned || !strings.Contains(src.Version, "sha256:") {
		t.Errorf("a registry source is pinned to its digest: %+v", src.Version)
	}
	for _, p := range []string{"minimal", "recommended"} {
		if !slices.Contains(src.PresetNames(), p) {
			t.Errorf("preset %s missing from %v", p, src.PresetNames())
		}
	}
	for _, dir := range []string{"templates", "rules", "stacks", "artifacts"} {
		entries, err := fs.ReadDir(src.Templates, dir)
		if err != nil || len(entries) == 0 {
			t.Errorf("%s/ comes through the artifact: %d entries, %v", dir, len(entries), err)
		}
	}
	if defs, err := src.ArtifactDefs(); err != nil || len(defs) == 0 {
		t.Errorf("artifact kinds parse: %d, %v", len(defs), err)
	}
	if defs, err := src.StackDefs(); err != nil || len(defs) == 0 {
		t.Errorf("stacks parse: %d, %v", len(defs), err)
	}

	// The same reference, written where a catalog to extend belongs, is explained.
	l.Signers = []Signer{fleetlintSigner}
	if _, err := l.Load(ociPrefix + ref); err == nil || !strings.Contains(err.Error(), "is a catalog version") {
		t.Errorf("a catalog version is not a catalog to extend: %v", err)
	}
}
