package catalog

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/catalog/fetch"
)

// An OCI catalog's .yaml files at the root are catalogs; files under
// templates/ are fix templates that overlay the preset ones.
const (
	ociPrefix              = "oci://"
	templatesDir           = fetch.TemplatesDir
	ociMaxFiles            = fetch.MaxFiles
	ociErrSignatureMissing = "no sigstore bundle is attached to the artifact (sign it with `cosign sign --new-bundle-format`)"
)

// ErrOCIPinRequired is returned for a tag reference without signers to verify it.
var ErrOCIPinRequired = errors.New("oci catalogs at a tag need signers in the sources file; a @sha256:<digest> reference needs none")

// parseOCIRef splits an OCI catalog reference. A digest pins the content;
// a tag alone is accepted only when signers can vouch for it.
func parseOCIRef(ref string) (fetch.OCIRef, error) {
	rest := strings.TrimPrefix(ref, ociPrefix)
	var o fetch.OCIRef
	if at := strings.Index(rest, "@"); at >= 0 {
		digest := rest[at+1:]
		rest = rest[:at]
		if !strings.HasPrefix(digest, "sha256:") || !isHexDigest(strings.TrimPrefix(digest, "sha256:")) {
			return fetch.OCIRef{}, fmt.Errorf("%s: want @sha256:<64 hex digits>", ref)
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
		return fetch.OCIRef{}, fmt.Errorf("%s: want oci://<registry>/<repository>:<tag> or @sha256:<digest>", ref)
	case o.Tag == "" && o.Digest == "":
		return fetch.OCIRef{}, fmt.Errorf("%s: a tag or a digest is required", ref)
	case o.Tag != "" && !fetch.ValidRev(o.Tag):
		return fetch.OCIRef{}, fmt.Errorf("%s: tag %q is not valid", ref, o.Tag)
	}
	return o, nil
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

func catalogsFromArtifact(ref string, art *fetch.OCIArtifact) ([]*Catalog, error) {
	templates, library := shippedFiles(art.Files)
	var names []string
	for name := range art.Files {
		if !strings.Contains(name, "/") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
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
		// Each file keeps its own digest, so the files of one artifact are
		// distinct catalogs; Artifact names the manifest they came in.
		c.Ref, c.Artifact, c.Templates, c.Library = ref, art.Digest, templates, library
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

// ReadDir lists the immediate children of a directory, so fs.WalkDir works.
func (m memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	prefix := name + "/"
	if name == "." {
		prefix = ""
	}
	seen := map[string]bool{}
	var out []fs.DirEntry
	for k, b := range m {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		child, _, isDir := strings.Cut(rest, "/")
		if seen[child] {
			continue
		}
		seen[child] = true
		entry := &memFile{name: prefix + child, dir: isDir}
		if !isDir {
			entry.data = b
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
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

func (f *memFile) Close() error { return nil }

func (f *memFile) Name() string { return f.name[strings.LastIndex(f.name, "/")+1:] }

func (f *memFile) Size() int64 { return int64(len(f.data)) }

func (f *memFile) Mode() fs.FileMode { return modeOf(f.dir) }

func (f *memFile) ModTime() time.Time { return time.Time{} }

func (f *memFile) IsDir() bool { return f.dir }

func (f *memFile) Sys() any { return nil }

func (f *memFile) Info() (fs.FileInfo, error) { return f, nil }

func (f *memFile) Type() fs.FileMode { return modeOf(f.dir).Type() }

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
