package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// fakeRegistry speaks the parts of the distribution API oras-go uses to
// read an artifact and its referrers (a copy of the catalog package's).
type fakeRegistry struct {
	mu        sync.Mutex
	blobs     map[string][]byte
	manifests map[string][]byte
	tags      map[string]string
	types     map[string]string
	referrers map[string][]ocispec.Descriptor
	// tagSchema serves referrers the way ghcr.io does: no referrers API,
	// a sha256-<hex> tag whose index records the empty config type.
	tagSchema bool
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

// push stores a manifest with the layers and returns its descriptor.
func (r *fakeRegistry) push(tag, artifactType string, layers []ocispec.Descriptor, subject *ocispec.Descriptor) ocispec.Descriptor {
	cfg := r.blob([]byte("{}"), ocispec.MediaTypeEmptyJSON, nil)
	m := ocispec.Manifest{MediaType: ocispec.MediaTypeImageManifest, ArtifactType: artifactType, Config: cfg, Layers: layers, Subject: subject}
	m.SchemaVersion = 2
	raw, _ := json.Marshal(m)
	return r.pushRaw(tag, artifactType, raw, subject)
}

// pushRaw stores manifest bytes as they are, valid or not.
func (r *fakeRegistry) pushRaw(tag, artifactType string, raw []byte, subject *ocispec.Descriptor) ocispec.Descriptor {
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
		if r.tagSchema {
			r.writeTagSchema(subject.Digest.String())
		}
	}
	return desc
}

// writeTagSchema stores the referrers of subject as cosign does without a
// referrers API. The caller holds the lock.
func (r *fakeRegistry) writeTagSchema(subject string) {
	idx := ocispec.Index{MediaType: ocispec.MediaTypeImageIndex}
	idx.SchemaVersion = 2
	for _, d := range r.referrers[subject] {
		d.ArtifactType = ocispec.MediaTypeEmptyJSON
		idx.Manifests = append(idx.Manifests, d)
	}
	raw, _ := json.Marshal(idx)
	d := digestOf(raw)
	r.manifests[d] = raw
	r.types[d] = ocispec.MediaTypeImageIndex
	r.tags[strings.Replace(subject, ":", "-", 1)] = d
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
		if r.tagSchema {
			http.NotFound(w, req)
			return
		}
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

const (
	catalogYAML = "apiVersion: fleetlint.org/v1\nkind: Catalog\n"
	bundleJSON  = `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`
)

// serve starts the registry and returns the repository name on it.
func serve(t *testing.T, reg *fakeRegistry) string {
	t.Helper()
	srv := httptest.NewServer(reg)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://") + "/acme/catalog"
}

func titled(name string) map[string]string { return map[string]string{titleAnnotation: name} }

// pushSigned stores a catalog with a template, an untitled layer and the
// referrers cosign and others leave: one bundle in each known media type,
// a referrer of another kind and a bundle referrer without a bundle layer.
func pushSigned(t *testing.T, reg *fakeRegistry) (string, ocispec.Descriptor) {
	t.Helper()
	layers := []ocispec.Descriptor{
		reg.blob([]byte(catalogYAML), "application/yaml", titled("catalog.yaml")),
		reg.blob([]byte("# policy\n"), "text/markdown", titled("templates/SECURITY.md")),
		reg.blob([]byte("ignored"), "application/octet-stream", nil),
		{MediaType: "application/octet-stream", Digest: godigest.Digest(digestOf([]byte("huge"))), Size: ociMaxFile + 1},
	}
	desc := reg.push("1.0.0", "application/vnd.fleetlint.catalog.v1", layers, nil)
	reg.push("", bundleArtifactType, []ocispec.Descriptor{reg.blob([]byte(bundleJSON), bundleArtifactType, nil)}, &desc)
	reg.push("", bundleArtifactTypeOld, []ocispec.Descriptor{reg.blob([]byte(`{"old":true}`), bundleArtifactTypeOld, nil)}, &desc)
	reg.push("", "application/vnd.example.sbom", []ocispec.Descriptor{reg.blob([]byte("sbom"), "application/spdx+json", nil)}, &desc)
	reg.push("", bundleArtifactTypeV1, []ocispec.Descriptor{
		reg.blob([]byte("not a bundle"), "text/plain", nil),
		{MediaType: bundleArtifactTypeV1, Digest: godigest.Digest(digestOf([]byte("big"))), Size: ociMaxFile + 1},
	}, &desc)
	return serve(t, reg), desc
}

func TestOCIFetch(t *testing.T) {
	t.Parallel()
	for _, tagSchema := range []bool{false, true} {
		reg := newFakeRegistry()
		reg.tagSchema = tagSchema
		name, desc := pushSigned(t, reg)
		fetch := OCIFetch(context.Background())
		for _, ref := range []OCIRef{{Repository: name, Tag: "1.0.0"}, {Repository: name, Digest: strings.TrimPrefix(desc.Digest.String(), "sha256:")}} {
			art, err := fetch(ref)
			if err != nil {
				t.Fatalf("tagSchema=%v %+v: %v", tagSchema, ref, err)
			}
			if art.Digest != strings.TrimPrefix(desc.Digest.String(), "sha256:") {
				t.Errorf("digest: %s", art.Digest)
			}
			if len(art.Files) != 2 || string(art.Files["catalog.yaml"]) != catalogYAML || string(art.Files["templates/SECURITY.md"]) != "# policy\n" {
				t.Errorf("files: %q", art.Files)
			}
			if len(art.Bundles) != 2 || string(art.Bundles[0]) != bundleJSON || string(art.Bundles[1]) != `{"old":true}` {
				t.Errorf("bundles: %q", art.Bundles)
			}
		}
		if _, err := fetch(OCIRef{Repository: name, Tag: "9.9.9"}); err == nil {
			t.Error("an unknown tag is an error")
		}
	}
}

func TestOCIReferenceAndTypes(t *testing.T) {
	t.Parallel()
	if (OCIRef{Tag: "v1"}).reference() != "v1" || (OCIRef{Tag: "v1", Digest: "abc"}).reference() != "sha256:abc" {
		t.Error("the digest wins over the tag")
	}
	for mt, want := range map[string]bool{bundleArtifactType: true, bundleArtifactTypeOld: true, bundleArtifactTypeV1: true, "application/vnd.dev.sigstore.bundle.v0.5+json": true, "text/plain": false, "": false, ocispec.MediaTypeEmptyJSON: false} {
		if got := isBundleType(mt); got != want {
			t.Errorf("isBundleType(%q) = %v", mt, got)
		}
	}
	for at, want := range map[string]bool{bundleArtifactType: true, "": true, ocispec.MediaTypeEmptyJSON: true, "application/vnd.example.sbom": false} {
		if got := mayBeBundle(at); got != want {
			t.Errorf("mayBeBundle(%q) = %v", at, got)
		}
	}
	if _, err := OCIFetch(context.Background())(OCIRef{Repository: "not a repository!"}); err == nil {
		t.Error("an invalid repository is an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OCIFetch(ctx)(OCIRef{Repository: "registry.invalid/acme/catalog", Tag: "1"}); err == nil {
		t.Error("a cancelled context fails before any request")
	}
}

func TestOCIArtifactLimits(t *testing.T) {
	t.Parallel()
	reg := newFakeRegistry()
	many := make([]ocispec.Descriptor, MaxFiles+1)
	for i := range many {
		many[i] = ocispec.Descriptor{MediaType: "text/plain", Digest: godigest.Digest(digestOf([]byte(fmt.Sprint(i)))), Size: 1}
	}
	reg.push("many", "", many, nil)
	reg.push("oversized", "", []ocispec.Descriptor{{MediaType: "text/plain", Digest: godigest.Digest(digestOf([]byte("x"))), Size: ociMaxFile + 1, Annotations: titled("x")}}, nil)
	reg.push("missing-blob", "", []ocispec.Descriptor{{MediaType: "text/plain", Digest: godigest.Digest(digestOf([]byte("gone"))), Size: 4, Annotations: titled("gone")}}, nil)
	reg.pushRaw("broken", "", []byte("not json"), nil)
	signed := reg.push("bad-referrer", "", []ocispec.Descriptor{reg.blob([]byte(catalogYAML), "application/yaml", titled("catalog.yaml"))}, nil)
	reg.pushRaw("", bundleArtifactType, []byte("not json either"), &signed)
	crowded := reg.push("crowded", "", nil, nil)
	for i := range maxReferrers + 1 {
		reg.push("", bundleArtifactType, []ocispec.Descriptor{reg.blob([]byte(fmt.Sprint(i)), bundleArtifactType, nil)}, &crowded)
	}
	name := serve(t, reg)
	fetch := OCIFetch(context.Background())
	for tag, wantErr := range map[string]string{
		"many":         "at most 512 files",
		"oversized":    "the limit is",
		"missing-blob": "layer gone",
		"broken":       "manifest",
		"bad-referrer": "referrers of",
		"crowded":      "more than 32 referrers",
	} {
		if _, err := fetch(OCIRef{Repository: name, Tag: tag}); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%s: want %q, got %v", tag, wantErr, err)
		}
	}
}
