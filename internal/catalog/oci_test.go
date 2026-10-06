package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"oras.land/oras-go/v2/registry/remote"
)

func TestParseOCIRef(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("ab", 32)
	good := map[string]OCIRef{
		"oci://ghcr.io/acme/catalog:3.2.0":                      {Repository: "ghcr.io/acme/catalog", Tag: "3.2.0"},
		"oci://localhost:5000/acme/catalog:v1":                  {Repository: "localhost:5000/acme/catalog", Tag: "v1"},
		"oci://ghcr.io/acme/catalog@sha256:" + digest:           {Repository: "ghcr.io/acme/catalog", Digest: digest},
		"oci://ghcr.io/acme/catalog:3.2.0@sha256:" + digest:     {Repository: "ghcr.io/acme/catalog", Tag: "3.2.0", Digest: digest},
		"oci://registry.example.com:443/a/b/c@sha256:" + digest: {Repository: "registry.example.com:443/a/b/c", Digest: digest},
	}
	for ref, want := range good {
		got, err := parseOCIRef(ref)
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", ref, got, err, want)
		}
	}
	for _, bad := range []string{"oci://ghcr.io/acme/catalog", "oci://catalog:1", "oci://ghcr.io/acme/catalog@sha256:abc", "oci://ghcr.io/acme/catalog@md5:" + digest, "oci://ghcr.io/acme/catalog:bad tag"} {
		if _, err := parseOCIRef(bad); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

// fakeRegistry speaks the parts of the distribution API oras-go uses to
// read an artifact and its referrers.
type fakeRegistry struct {
	mu        sync.Mutex
	blobs     map[string][]byte // by digest
	manifests map[string][]byte // by digest
	tags      map[string]string // tag -> digest
	types     map[string]string // manifest digest -> media type
	referrers map[string][]ocispec.Descriptor
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}, tags: map[string]string{}, types: map[string]string{}, referrers: map[string][]ocispec.Descriptor{}}
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *fakeRegistry) blob(b []byte, mediaType string, annotations map[string]string) ocispec.Descriptor {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := digestOf(b)
	r.blobs[d] = b
	return ocispec.Descriptor{MediaType: mediaType, Digest: godigest.Digest(d), Size: int64(len(b)), Annotations: annotations}
}

// push stores an artifact manifest with the given layers and returns its descriptor.
func (r *fakeRegistry) push(tag, artifactType string, layers []ocispec.Descriptor, subject *ocispec.Descriptor) ocispec.Descriptor {
	cfg := r.blob([]byte("{}"), ocispec.MediaTypeEmptyJSON, nil)
	m := ocispec.Manifest{MediaType: ocispec.MediaTypeImageManifest, ArtifactType: artifactType, Config: cfg, Layers: layers, Subject: subject}
	m.SchemaVersion = 2
	raw, _ := json.Marshal(m)
	r.mu.Lock()
	defer r.mu.Unlock()
	d := digestOf(raw)
	r.manifests[d] = raw
	r.types[d] = ocispec.MediaTypeImageManifest
	if tag != "" {
		r.tags[tag] = d
	}
	desc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageManifest, Digest: godigest.Digest(d), Size: int64(len(raw)), ArtifactType: artifactType}
	if subject != nil {
		r.referrers[subject.Digest.String()] = append(r.referrers[subject.Digest.String()], desc)
	}
	return desc
}

func (r *fakeRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(req.URL.Path, "/v2/"), "/")
	if req.URL.Path == "/v2/" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if len(parts) < 3 {
		http.NotFound(w, req)
		return
	}
	kind, ref := parts[len(parts)-2], parts[len(parts)-1]
	switch kind {
	case "manifests":
		d := ref
		if !strings.HasPrefix(ref, "sha256:") {
			d = r.tags[ref]
		}
		raw, ok := r.manifests[d]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", r.types[d])
		w.Header().Set("Docker-Content-Digest", d)
		w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
		if req.Method != http.MethodHead {
			_, _ = w.Write(raw)
		}
	case "blobs":
		raw, ok := r.blobs[ref]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Docker-Content-Digest", ref)
		w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
		_, _ = w.Write(raw)
	case "referrers":
		idx := ocispec.Index{MediaType: ocispec.MediaTypeImageIndex, Manifests: r.referrers[ref]}
		idx.SchemaVersion = 2
		if idx.Manifests == nil {
			idx.Manifests = []ocispec.Descriptor{}
		}
		w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
		_ = json.NewEncoder(w).Encode(idx)
	default:
		http.NotFound(w, req)
	}
}

const ociCatalog = `apiVersion: fleetlint.org/v1
kind: Catalog
metadata: {name: acme, version: 1.0.0, includes: ["fleetlint:minimal"]}
rules: []
overrides:
  repo/no-tracked-env: {locked: true}
`

// pushCatalog stores a catalog with a template and returns the registry,
// the repository name and the manifest digest.
func pushCatalog(t *testing.T) (*fakeRegistry, string, ocispec.Descriptor) {
	t.Helper()
	reg := newFakeRegistry()
	srv := httptest.NewServer(reg)
	t.Cleanup(srv.Close)
	layers := []ocispec.Descriptor{
		reg.blob([]byte(ociCatalog), "application/yaml", map[string]string{titleAnnotation: "catalog.yaml"}),
		reg.blob([]byte("# acme security policy\n"), "text/markdown", map[string]string{titleAnnotation: "templates/SECURITY.md"}),
		reg.blob([]byte("ignored"), "application/octet-stream", nil),
	}
	desc := reg.push("1.0.0", "application/vnd.fleetlint.catalog.v1", layers, nil)
	return reg, strings.TrimPrefix(srv.URL, "http://") + "/acme/catalog", desc
}

func localFetch(ctx context.Context) func(OCIRef) (*OCIArtifact, error) {
	return func(o OCIRef) (*OCIArtifact, error) {
		repo, err := remote.NewRepository(o.Repository)
		if err != nil {
			return nil, err
		}
		repo.PlainHTTP = true
		return fetchArtifact(ctx, repo, o)
	}
}

func TestLoadOCIByDigest(t *testing.T) {
	t.Parallel()
	_, name, desc := pushCatalog(t)
	l := Loader{FetchOCI: localFetch(context.Background())}
	ref := "oci://" + name + "@" + desc.Digest.String()
	cats, err := l.Load(ref)
	if err != nil {
		t.Fatal(err)
	}
	own := cats[len(cats)-1]
	if own.Ref != ref || own.Metadata.Name != "acme" || own.Digest != strings.TrimPrefix(desc.Digest.String(), "sha256:") {
		t.Fatalf("catalog: %+v", own)
	}
	if cats[0].Ref != "fleetlint:minimal" {
		t.Errorf("the include is loaded first: %s", cats[0].Ref)
	}
	b, err := fs.ReadFile(own.Templates, "templates/SECURITY.md")
	if err != nil || !strings.Contains(string(b), "acme") {
		t.Errorf("the template travels with the catalog: %q %v", b, err)
	}
	if _, err := fs.ReadFile(own.Templates, "catalog.yaml"); err == nil {
		t.Error("only templates/ files are templates")
	}
	if _, err := fs.Stat(own.Templates, "templates"); err != nil {
		t.Errorf("the templates directory exists: %v", err)
	}

	wrong := "oci://" + name + "@sha256:" + strings.Repeat("0", 64)
	if _, err := l.Load(wrong); err == nil || !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "fetch") {
		t.Errorf("an unknown digest fails: %v", err)
	}
	if _, err := l.Load("oci://" + name + ":1.0.0"); !errors.Is(err, ErrOCIPinRequired) {
		t.Errorf("a tag without signers is refused: %v", err)
	}
	if _, err := (Loader{}).Load(ref); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("no fetcher, no oci: %v", err)
	}
}

// Signatures: the virtual sigstore signs the manifest; the loader accepts
// the artifact for a matching signer and refuses it for anyone else.
func TestLoadOCISigned(t *testing.T) {
	reg, name, desc := pushCatalog(t)
	virtual, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	const issuer, subject = "https://token.actions.githubusercontent.com", "https://github.com/acme/standards/.github/workflows/publish.yml@refs/tags/v1.0.0"
	entity, err := virtual.Sign(subject, issuer, reg.manifests[desc.Digest.String()])
	if err != nil {
		t.Fatal(err)
	}
	// The bundle layer's bytes are a key into the test's entities, not JSON.
	entities := map[string]verify.SignedEntity{"good": entity}
	swap, sct := loadBundle, requireSCT
	requireSCT = false
	loadBundle = func(raw []byte) (verify.SignedEntity, error) {
		if e, ok := entities[string(raw)]; ok {
			return e, nil
		}
		return nil, errors.New("not a bundle")
	}
	t.Cleanup(func() { loadBundle, requireSCT = swap, sct })
	subj := desc
	reg.push("", bundleArtifactType, []ocispec.Descriptor{reg.blob([]byte("junk"), bundleArtifactType, nil)}, &subj)
	reg.push("", bundleArtifactType, []ocispec.Descriptor{reg.blob([]byte("good"), bundleArtifactType, nil)}, &subj)

	trusted := func() (root.TrustedMaterial, error) { return virtual, nil }
	ref := "oci://" + name + ":1.0.0"
	ok := Loader{FetchOCI: localFetch(context.Background()), TrustedRoot: trusted, Signers: []Signer{{Issuer: issuer, Subject: subject}}}
	if _, err := ok.Load(ref); err != nil {
		t.Fatalf("a valid signature by a listed signer passes: %v", err)
	}
	byRegex := Loader{FetchOCI: localFetch(context.Background()), TrustedRoot: trusted, Signers: []Signer{{Issuer: issuer, SubjectRegex: `^https://github\.com/acme/standards/.*@refs/tags/v`}}}
	if _, err := byRegex.Load(ref); err != nil {
		t.Fatalf("a regex signer matches: %v", err)
	}
	other := Loader{FetchOCI: localFetch(context.Background()), TrustedRoot: trusted, Signers: []Signer{{Issuer: issuer, Subject: "https://github.com/evil/standards/.github/workflows/publish.yml@refs/tags/v1.0.0"}}}
	if _, err := other.Load(ref); err == nil || !strings.Contains(err.Error(), "no attached signature is valid") {
		t.Fatalf("another identity is refused: %v", err)
	}
	// With a digest and signers, the signature is still checked.
	if _, err := other.Load("oci://" + name + "@" + desc.Digest.String()); err == nil {
		t.Fatal("signers listed: the signature is verified even for a digest reference")
	}
	noRoot := Loader{FetchOCI: localFetch(context.Background()), Signers: ok.Signers}
	if _, err := noRoot.Load(ref); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("no trusted root, no verification: %v", err)
	}
	unsigned := pushUnsigned(t)
	if _, err := ok.Load(unsigned); err == nil || !strings.Contains(err.Error(), "no sigstore bundle") {
		t.Errorf("an unsigned artifact at a tag is refused: %v", err)
	}
}

func pushUnsigned(t *testing.T) string {
	t.Helper()
	_, name, _ := pushCatalog(t)
	return "oci://" + name + ":1.0.0"
}

func TestSignerValidation(t *testing.T) {
	t.Parallel()
	cases := map[Signer]bool{
		{Issuer: "https://token.actions.githubusercontent.com", Subject: "x"}:   true,
		{Issuer: "https://accounts.google.com", SubjectRegex: `^.*@acme\.com$`}: true,
		{Issuer: "http://insecure", Subject: "x"}:                               false,
		{Issuer: "https://ok", Subject: "x", SubjectRegex: "y"}:                 false,
		{Issuer: "https://ok"}:                    false,
		{Issuer: "https://ok", SubjectRegex: "("}: false,
	}
	for s, want := range cases {
		if err := s.validate(); (err == nil) != want {
			t.Errorf("%+v: err=%v want ok=%v", s, err, want)
		}
	}
	src := Sources{Version: 1, Signers: []Signer{{Issuer: "http://x", Subject: "y"}}, Catalogs: map[string]string{"org": "fleetlint:minimal"}}
	if err := src.validate(""); err == nil || !strings.Contains(err.Error(), "signers[0]") {
		t.Errorf("sources validation names the signer: %v", err)
	}
}

func TestOverlayAndMemFS(t *testing.T) {
	t.Parallel()
	over := memFS{"templates/SECURITY.md": []byte("org"), "templates/sub/x.txt": []byte("x")}
	tpl := Builtin().FixTemplatesOver(over)
	b, err := tpl.Template("SECURITY.md")
	if err != nil || string(b) != "org" {
		t.Errorf("the catalog's file wins: %q %v", b, err)
	}
	if b, err := tpl.Template("cliff.toml"); err != nil || len(b) == 0 {
		t.Errorf("the preset's file is still served: %v", err)
	}
	if _, err := tpl.Template("nope"); err == nil {
		t.Error("a missing file is missing in every layer")
	}
	if _, err := over.Open("templates/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("memFS: %v", err)
	}
	f, err := over.Open("templates/sub")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := f.Stat(); !st.IsDir() || st.Name() != "sub" {
		t.Errorf("directory entry: %+v", st)
	}
	if _, err := f.Read(make([]byte, 1)); err == nil {
		t.Error("reading a directory fails")
	}
}
