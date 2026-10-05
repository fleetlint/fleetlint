package fix

import (
	"regexp"

	"github.com/google/licensecheck"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// minLicenseCoverage is how much of the file must be one license's text
// before its identifier is written anywhere.
const minLicenseCoverage = 90

// ambiguousGNU matches the GNU identifiers without -only or -or-later. The
// license text is the same for both, so which one applies is the author's
// statement and cannot be read from the file.
var ambiguousGNU = regexp.MustCompile(`^(A|L)?GPL-[0-9.]+$`)

// DetectLicense returns the SPDX identifier of the repository's LICENSE or
// COPYING file, or "" when there is none, it is not one recognised license,
// or the identifier would be a guess (GNU licenses, see ambiguousGNU).
func DetectLicense(r *repo.Repo) string {
	for _, pattern := range []string{"LICENSE*", "COPYING*"} {
		for _, path := range r.Glob(pattern) {
			text, ok := r.Text(path)
			if !ok {
				continue
			}
			cov := licensecheck.Scan([]byte(text))
			if len(cov.Match) == 1 && cov.Percent >= minLicenseCoverage && !ambiguousGNU.MatchString(cov.Match[0].ID) {
				return cov.Match[0].ID
			}
		}
	}
	return ""
}
