package catalog

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// Signer is an identity allowed to sign a catalog: the OIDC issuer that
// vouched for it and the certificate subject, exact or as a regular
// expression. For GitHub Actions the subject is the workflow reference,
// e.g. https://github.com/acme/standards/.github/workflows/publish.yml@refs/tags/v1.2.0.
type Signer struct {
	Issuer       string `yaml:"issuer"`
	Subject      string `yaml:"subject,omitempty"`
	SubjectRegex string `yaml:"subject_regex,omitempty"`
}

func (s Signer) validate() error {
	if !strings.HasPrefix(s.Issuer, "https://") {
		return fmt.Errorf("issuer %q must be an https URL", s.Issuer)
	}
	if (s.Subject == "") == (s.SubjectRegex == "") {
		return errors.New("exactly one of subject and subject_regex is required")
	}
	_, err := verify.NewShortCertificateIdentity(s.Issuer, "", s.Subject, s.SubjectRegex)
	return err
}

// TrustedRootFunc provides the sigstore trust material: the public-good
// instance's TUF root by default, a virtual one in tests.
type TrustedRootFunc func() (root.TrustedMaterial, error)

// PublicTrustedRoot fetches the sigstore public-good trusted root through
// TUF once and caches it for the process. ctx is unused: the TUF client
// manages its own timeouts and on-disk cache.
func PublicTrustedRoot(_ context.Context) TrustedRootFunc {
	var (
		once sync.Once
		tr   *root.TrustedRoot
		err  error
	)
	return func() (root.TrustedMaterial, error) {
		once.Do(func() { tr, err = root.FetchTrustedRoot() })
		if err != nil {
			return nil, fmt.Errorf("sigstore trusted root: %w", err)
		}
		return tr, nil
	}
}

// verifyArtifact accepts the artifact when at least one attached bundle is
// a valid signature over its manifest digest by one of the loader's signers.
func (l Loader) verifyArtifact(art *OCIArtifact) error {
	if len(art.Bundles) == 0 {
		return errors.New(ociErrSignatureMissing)
	}
	if l.TrustedRoot == nil {
		return errors.New("signature verification is disabled in this mode")
	}
	trusted, err := l.TrustedRoot()
	if err != nil {
		return err
	}
	var errs []error
	for _, raw := range art.Bundles {
		entity, err := loadBundle(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("bundle: %w", err))
			continue
		}
		if err := verifyEntity(entity, art.Digest, l.Signers, trusted); err != nil {
			errs = append(errs, err)
			continue
		}
		return nil
	}
	return fmt.Errorf("no attached signature is valid for the listed signers: %w", errors.Join(errs...))
}

// requireSCT demands a signed certificate timestamp on the signing
// certificate, as Fulcio issues; the virtual sigstore in tests has none.
var requireSCT = true

// loadBundle parses a sigstore bundle; a variable so tests can hand in
// entities signed by a virtual sigstore, which has no JSON form.
var loadBundle = func(raw []byte) (verify.SignedEntity, error) {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	return &b, nil
}

// verifyEntity checks one signed entity against the digest and the signers:
// a Fulcio certificate chain, a transparency-log entry or a signed timestamp
// placing the signature inside the certificate's validity, and an identity
// from the list.
func verifyEntity(entity verify.SignedEntity, digestHex string, signers []Signer, trusted root.TrustedMaterial) error {
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return fmt.Errorf("digest %q: %w", digestHex, err)
	}
	ids := make([]verify.PolicyOption, 0, len(signers))
	for _, s := range signers {
		id, err := verify.NewShortCertificateIdentity(s.Issuer, "", s.Subject, s.SubjectRegex)
		if err != nil {
			return fmt.Errorf("signer %s: %w", s.Issuer, err)
		}
		ids = append(ids, verify.WithCertificateIdentity(id))
	}
	opts := []verify.VerifierOption{verify.WithObserverTimestamps(1), verify.WithTransparencyLog(1)}
	if requireSCT {
		opts = append(opts, verify.WithSignedCertificateTimestamps(1))
	}
	v, err := verify.NewVerifier(trusted, opts...)
	if err != nil {
		return err
	}
	_, err = v.Verify(entity, verify.NewPolicy(verify.WithArtifactDigest("sha256", digest), ids...))
	return err
}
