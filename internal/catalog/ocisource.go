package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/fleetlint/fleetlint/internal/catalog/fetch"
)

// A catalog version can come from a registry instead of git: the publish
// workflow of the catalog repository pushes its whole tree (presets/,
// rules/, stacks/, templates/, aliases.yaml) as one artifact per tag. A
// repository pins it with `catalog: {oci: <registry>/<repository>:<tag>}`
// and extends `fleetlint:<preset>` as with a git pin.

// DefaultOCI is where the fleetlint catalog repository publishes its tags.
const DefaultOCI = "ghcr.io/fleetlint/catalog"

// SourceArtifactType marks an artifact that is a catalog version, as
// opposed to a catalog to extend.
const SourceArtifactType = "application/vnd.fleetlint.catalog.source.v1"

// fleetlintSigner is the identity that signs DefaultOCI: the catalog
// repository's publish workflow, on a version tag.
var fleetlintSigner = Signer{
	Issuer:       "https://token.actions.githubusercontent.com",
	SubjectRegex: `^https://github\.com/fleetlint/catalog/\.github/workflows/publish\.yml@refs/tags/v`,
}

// ociSources caches the catalog versions fetched in this process, so a
// fleet run over many repositories reads each artifact once.
var ociSources sync.Map // reference -> *Source

// PinOCI makes the catalog version in an artifact the source of presets,
// library, stacks and templates. A @sha256 digest pins the content; a tag
// alone is verified against the sources file's signers, or for DefaultOCI
// against the catalog repository's publish workflow.
func (l *Loader) PinOCI(ref string) (*Source, error) {
	if cached, ok := ociSources.Load(ref); ok {
		l.Source = cached.(*Source)
		return l.Source, nil
	}
	o, err := parseOCIRef(ociPrefix + strings.TrimPrefix(ref, ociPrefix))
	if err != nil {
		return nil, err
	}
	if l.FetchOCI == nil {
		return nil, fmt.Errorf("catalog %s: oci catalogs are disabled in this mode", ref)
	}
	signers := l.Signers
	if len(signers) == 0 && o.Repository == DefaultOCI {
		signers = []Signer{fleetlintSigner}
	}
	if o.Digest == "" && len(signers) == 0 {
		return nil, fmt.Errorf("catalog %s: a tag needs signers in the sources file; pin a @sha256:<digest> instead", ref)
	}
	art, err := l.FetchOCI(o)
	if err != nil {
		return nil, fmt.Errorf("catalog %s: %w", ref, err)
	}
	if o.Digest != "" && art.Digest != o.Digest {
		return nil, fmt.Errorf("catalog %s: the registry returned digest %s", ref, art.Digest)
	}
	if o.Digest == "" {
		verifier := *l
		verifier.Signers = signers
		if err := verifier.verifyArtifact(art); err != nil {
			return nil, fmt.Errorf("catalog %s: %w", ref, err)
		}
	}
	src, err := sourceFromArtifact(ref, o, art)
	if err != nil {
		return nil, err
	}
	ociSources.Store(ref, src)
	l.Source = src
	return src, nil
}

// sourceFromArtifact serves an artifact's files as a catalog version.
func sourceFromArtifact(ref string, o fetch.OCIRef, art *fetch.OCIArtifact) (*Source, error) {
	files := memFS(art.Files)
	name := o.Tag
	if name == "" {
		name = "sha256:" + art.Digest[:12]
	} else {
		name += " (sha256:" + art.Digest[:12] + ")"
	}
	src := &Source{Presets: files, Templates: files, Rules: files, Stacks: files, Version: name, Pinned: true}
	if len(src.PresetNames()) == 0 {
		return nil, fmt.Errorf("catalog %s: the artifact has no presets/ directory; is it a catalog version?", ref)
	}
	return src, nil
}

// isSourceArtifact reports an artifact that is a whole catalog version,
// which a repository pins with `catalog: {oci: ...}` instead of extending.
func isSourceArtifact(files map[string][]byte) bool {
	for name := range files {
		if strings.HasPrefix(name, "presets/") {
			return true
		}
	}
	return false
}

// errSourceArtifact explains a catalog version written where a catalog to
// extend belongs.
func errSourceArtifact(ref string) error {
	return fmt.Errorf("%s is a catalog version (presets, rules, stacks): pin it with `catalog: {oci: %s}` and extend fleetlint:<preset>", ref, strings.TrimPrefix(ref, ociPrefix))
}

// ociKey is what two pins are compared by: the artifact repository, and
// the tag and digest where given. A git pin of the default repository at a
// tag is the same version as DefaultOCI at that tag.
func (p *Pin) ociKey() (repo, tag, digest string, err error) {
	if p.OCI == "" {
		if repoOrDefault(p.Repo) != DefaultRepo {
			return "", "", "", errors.New("not the published catalog")
		}
		return DefaultOCI, p.Version, "", nil
	}
	o, err := parseOCIRef(ociPrefix + strings.TrimPrefix(p.OCI, ociPrefix))
	return o.Repository, o.Tag, o.Digest, err
}

func (p *Pin) sameOCI(o *Pin) bool {
	r1, t1, d1, err1 := p.ociKey()
	r2, t2, d2, err2 := o.ociKey()
	if err1 != nil || err2 != nil || r1 != r2 {
		return false
	}
	if t1 != "" && t2 != "" && t1 != t2 {
		return false
	}
	if d1 != "" && d2 != "" {
		return d1 == d2
	}
	return t1 != "" && t1 == t2
}

var _ fs.FS = memFS(nil)
