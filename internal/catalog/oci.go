package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
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
// org.opencontainers.image.title annotation. Files ending in .yaml at the
// root are catalogs, files under templates/ are fix templates that overlay
// the preset ones. A cosign signature in the sigstore bundle format
// (`cosign sign --new-bundle-format`) is read from the artifact's referrers.
const (
	ociPrefix       = "oci://"
	ociFetchTimeout = 2 * time.Minute
	ociMaxFile      = 8 << 20
	ociMaxFiles     = 512
	titleAnnotation = "org.opencontainers.image.title"
	// bundleArtifactTypes are the media types cosign has used for bundle referrers.
	bundleArtifactType     = "application/vnd.dev.sigstore.bundle.v0.3+json"
	bundleArtifactTypeOld  = "application/vnd.dev.sigstore.bundle+json;version=0.3"
	bundleArtifactTypeV1   = "application/vnd.dev.sigstore.bundle.v0.4+json"
	templatesDir           = "templates/"
	ociErrSignatureMissing = "no sigstore bundle is attached to the artifact (sign it with `cosign sign --new-bundle-format`)"
)

// ErrOCIPinRequired is returned for a tag reference without signers to verify it.
var ErrOCIPinRequired = errors.New("oci catalogs at a tag need signers in the sources file; a @sha256:<digest> reference needs none")

// OCIRef is a parsed `oci://<registry>/<repository>[:<tag>][@sha256:<digest>]`
// reference, the argument of Loader.FetchOCI.
type OCIRef struct {
	Repository, Tag, Digest string
}

func (o OCIRef) reference() string {
	if o.Digest != "" {
		return "sha256:" + o.Digest
	}
	return o.Tag
}

// parseOCIRef splits an OCI catalog reference. A digest pins the content;
// a tag alone is accepted only when signers can vouch for it.
func parseOCIRef(ref string) (OCIRef, error) {
	rest := strings.TrimPrefix(ref, ociPrefix)
	var o OCIRef
	if at := strings.Index(rest, "@"); at >= 0 {
		digest := rest[at+1:]
		rest = rest[:at]
		if !strings.HasPrefix(digest, "sha256:") || !isHexDigest(strings.TrimPrefix(digest, "sha256:")) {
			return OCIRef{}, fmt.Errorf("%s: want @sha256:<64 hex digits>", ref)
		}
		o.Digest = strings.TrimPrefix(digest, "sha256:")
	}
	// The registry host may carry a port; a tag follows the last colon after the last slash.
	if slash := strings.LastIndex(rest, "/"); slash >= 0 {
		if colon := strings.LastIndex(rest[slash:], ":"); colon > 0 {
			o.Tag = rest[slash+colon+1:]
			rest = rest[:slash+colon]
		}
	}
	o.Repository = rest
	switch {
	case !strings.Contains(o.Repository, "/") || strings.ContainsAny(o.Repository, " \t\n"):
		return OCIRef{}, fmt.Errorf("%s: want oci://<registry>/<repository>:<tag> or @sha256:<digest>", ref)
	case o.Tag == "" && o.Digest == "":
		return OCIRef{}, fmt.Errorf("%s: a tag or a digest is required", ref)
	case o.Tag != "" && !tagRe.MatchString(o.Tag):
		return OCIRef{}, fmt.Errorf("%s: tag %q is not valid", ref, o.Tag)
	}
	return o, nil
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
		return fetchArtifact(ctx, repo, o)
	}
}

func fetchArtifact(ctx context.Context, repo *remote.Repository, o OCIRef) (*OCIArtifact, error) {
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
	if len(manifest.Layers) > ociMaxFiles {
		return nil, fmt.Errorf("manifest %s has %d layers; at most %d files are read", desc.Digest, len(manifest.Layers), ociMaxFiles)
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

// loadOCI fetches an artifact, verifies it, and returns its catalogs in
// file-name order, each carrying the artifact's templates.
func (l Loader) loadOCI(ref string) ([]*Catalog, error) {
	o, err := parseOCIRef(ref)
	if err != nil {
		return nil, err
	}
	if l.FetchOCI == nil {
		return nil, fmt.Errorf("%s: oci catalogs are disabled in this mode", ref)
	}
	if o.Digest == "" && len(l.Signers) == 0 {
		return nil, fmt.Errorf("%s: %w", ref, ErrOCIPinRequired)
	}
	art, err := l.FetchOCI(o)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", ref, err)
	}
	if o.Digest != "" && art.Digest != o.Digest {
		return nil, fmt.Errorf("%s: the registry returned digest %s", ref, art.Digest)
	}
	if len(l.Signers) > 0 {
		if err := l.verifyArtifact(art); err != nil {
			return nil, fmt.Errorf("%s: %w", ref, err)
		}
	}
	cats, err := catalogsFromArtifact(ref, art)
	if err != nil {
		return nil, err
	}
	for _, c := range cats {
		if err := l.expand(c); err != nil {
			return nil, fmt.Errorf("%s: %w", ref, err)
		}
	}
	return cats, nil
}

func catalogsFromArtifact(ref string, art *OCIArtifact) ([]*Catalog, error) {
	templates := memFS{}
	var names []string
	for name, b := range art.Files {
		switch {
		case strings.HasPrefix(name, templatesDir):
			templates[name] = b
		case !strings.Contains(name, "/") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")):
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s: the artifact has no catalog file (a *.yaml at its root)", ref)
	}
	sort.Strings(names)
	out := make([]*Catalog, 0, len(names))
	for _, name := range names {
		c, err := Parse(art.Files[name])
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", ref, name, err)
		}
		c.Ref, c.Digest = ref, art.Digest
		if len(templates) > 0 {
			c.Templates = templates
		}
		out = append(out, c)
	}
	return out, nil
}

// memFS serves fetched files as a file system, for the template overlay.
type memFS map[string][]byte

func (m memFS) Open(name string) (fs.File, error) {
	if b, ok := m[name]; ok {
		return &memFile{name: name, data: b}, nil
	}
	for k := range m {
		if strings.HasPrefix(k, name+"/") {
			return &memFile{name: name, dir: true}, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

type memFile struct {
	name string
	data []byte
	off  int
	dir  bool
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f, nil }
func (f *memFile) Read(p []byte) (int, error) {
	if f.dir {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: errors.New("is a directory")}
	}
	if f.off >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.off:])
	f.off += n
	return n, nil
}
func (f *memFile) Close() error               { return nil }
func (f *memFile) Name() string               { return f.name[strings.LastIndex(f.name, "/")+1:] }
func (f *memFile) Size() int64                { return int64(len(f.data)) }
func (f *memFile) Mode() fs.FileMode          { return modeOf(f.dir) }
func (f *memFile) ModTime() time.Time         { return time.Time{} }
func (f *memFile) IsDir() bool                { return f.dir }
func (f *memFile) Sys() any                   { return nil }
func (f *memFile) Info() (fs.FileInfo, error) { return f, nil }
func (f *memFile) Type() fs.FileMode          { return modeOf(f.dir).Type() }

func modeOf(dir bool) fs.FileMode {
	if dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

func isHexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
