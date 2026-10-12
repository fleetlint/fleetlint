package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// An OCI catalog is an artifact pushed with `oras push <ref> catalog.yaml
// templates/...`: every layer is one file, named by its
// org.opencontainers.image.title annotation. A cosign signature in the
// sigstore bundle format (`cosign sign --new-bundle-format`) is read from
// the artifact's referrers.
const (
	ociFetchTimeout = 2 * time.Minute
	ociMaxFile      = 8 << 20
	titleAnnotation = "org.opencontainers.image.title"
	// bundleArtifactTypes are the media types cosign has used for bundle referrers.
	bundleArtifactType    = "application/vnd.dev.sigstore.bundle.v0.3+json"
	bundleArtifactTypeOld = "application/vnd.dev.sigstore.bundle+json;version=0.3"
	bundleArtifactTypeV1  = "application/vnd.dev.sigstore.bundle.v0.4+json"
)

// OCIRef is a parsed `oci://<registry>/<repository>[:<tag>][@sha256:<digest>]`
// reference, the argument of the catalog loader's FetchOCI.
type OCIRef struct {
	Repository, Tag, Digest string
}

func (o OCIRef) reference() string {
	if o.Digest != "" {
		return "sha256:" + o.Digest
	}
	return o.Tag
}

// OCIArtifact is a fetched catalog artifact: the manifest digest the
// signature covers, its files by title, and every sigstore bundle attached
// to it.
type OCIArtifact struct {
	Digest  string
	Files   map[string][]byte
	Bundles [][]byte
}

// OCIFetch returns a FetchOCI function for Loader that reads from a registry
// with the credentials docker or oras already stored. Registries on
// localhost are spoken to over plain http, like oras does.
func OCIFetch(ctx context.Context) func(OCIRef) (*OCIArtifact, error) {
	return func(o OCIRef) (*OCIArtifact, error) {
		ctx, cancel := context.WithTimeout(ctx, ociFetchTimeout)
		defer cancel()
		repo, err := remote.NewRepository(o.Repository)
		if err != nil {
			return nil, err
		}
		host := repo.Reference.Registry
		repo.PlainHTTP = strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1")
		store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
		if err == nil {
			repo.Client = &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache(), Credential: credentials.Credential(store)}
		}
		return Artifact(ctx, repo, o)
	}
}

// Artifact reads the manifest, its titled layers and the signature
// bundles attached to it from an opened repository.
func Artifact(ctx context.Context, repo *remote.Repository, o OCIRef) (*OCIArtifact, error) {
	desc, rc, err := repo.FetchReference(ctx, o.reference())
	if err != nil {
		return nil, err
	}
	raw, err := content.ReadAll(rc, desc)
	if cerr := rc.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("manifest %s: %w", desc.Digest, err)
	}
	if len(manifest.Layers) > MaxFiles {
		return nil, fmt.Errorf("manifest %s has %d layers; at most %d files are read", desc.Digest, len(manifest.Layers), MaxFiles)
	}
	art := &OCIArtifact{Digest: strings.TrimPrefix(desc.Digest.String(), "sha256:"), Files: map[string][]byte{}}
	for _, layer := range manifest.Layers {
		title := layer.Annotations[titleAnnotation]
		if title == "" {
			continue // an untitled layer is not a file
		}
		if layer.Size > ociMaxFile {
			return nil, fmt.Errorf("layer %s (%s) is %d bytes; the limit is %d", title, layer.Digest, layer.Size, ociMaxFile)
		}
		b, err := content.FetchAll(ctx, repo.Blobs(), layer)
		if err != nil {
			return nil, fmt.Errorf("layer %s: %w", title, err)
		}
		art.Files[title] = b
	}
	art.Bundles, err = fetchBundles(ctx, repo, desc)
	return art, err
}

// fetchBundles collects the sigstore bundles attached to the manifest as
// referrers, whatever media type cosign used for them.
func fetchBundles(ctx context.Context, repo *remote.Repository, subject ocispec.Descriptor) ([][]byte, error) {
	var bundles [][]byte
	err := repo.Referrers(ctx, subject, "", func(referrers []ocispec.Descriptor) error {
		for _, ref := range referrers {
			if !isBundleType(ref.ArtifactType) {
				continue
			}
			found, err := bundleLayers(ctx, repo, ref)
			if err != nil {
				return err
			}
			bundles = append(bundles, found...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("referrers of %s: %w", subject.Digest, err)
	}
	return bundles, nil
}

// bundleLayers reads the bundle blobs of one referrer manifest.
func bundleLayers(ctx context.Context, repo *remote.Repository, ref ocispec.Descriptor) ([][]byte, error) {
	raw, err := content.FetchAll(ctx, repo.Manifests(), ref)
	if err != nil {
		return nil, err
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	var out [][]byte
	for _, layer := range m.Layers {
		if layer.Size > ociMaxFile || !isBundleType(layer.MediaType) {
			continue
		}
		b, err := content.FetchAll(ctx, repo.Blobs(), layer)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func isBundleType(mediaType string) bool {
	switch mediaType {
	case bundleArtifactType, bundleArtifactTypeOld, bundleArtifactTypeV1:
		return true
	}
	return strings.HasPrefix(mediaType, "application/vnd.dev.sigstore.bundle")
}
