// Package baseline grandfathers existing findings so a repository can adopt
// rules today and be held to them for new code only. A baseline is a
// multiset of finding fingerprints: an entry listed n times covers n
// findings with that fingerprint, so a new copy of a grandfathered finding
// still fails. Matching findings are reported as baselined, not as
// failures. The baseline may only shrink: writing a baseline never adds entries to
// an existing one unless the caller asks to reset it.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/fleetlint/fleetlint/internal/model"
)

// DefaultFile is the baseline file name at the repository root.
const DefaultFile = ".fleetlint-baseline.json"

// File is the on-disk format.
type File struct {
	Version   int      `json:"version"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Entries   []string `json:"entries"`
}

// Baseline is the loaded multiset: fingerprint to how many findings it covers.
type Baseline struct {
	Path    string
	entries map[string]int
	file    File
}

// Key identifies a finding independent of line numbers, run order and
// where a report points (its anchor): the hex SHA-256 of rule id, scope,
// path and message, and of the artifact's name when the finding is about
// a named artifact, so each artifact is a finding of its own. SARIF uses
// all of it; a finding without an artifact keeps the key it always had.
func Key(ruleID, scope string, f model.Finding) string {
	key := ruleID + "\x00" + scope + "\x00" + f.Path + "\x00" + f.Message
	if f.Artifact != "" {
		key += "\x00" + f.Artifact
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Fingerprint is the baseline's entry for a finding: the first 16
// characters of its Key.
func Fingerprint(rule, scope string, f model.Finding) string {
	return Key(rule, scope, f)[:16]
}

// Files reads and writes the baseline inside the repository.
type Files interface {
	ReadFile(rel string) ([]byte, error)
	WriteFile(rel string, data []byte, create bool) error
}

// Load reads a baseline; a missing file yields an empty baseline and ok=false.
func Load(fsys Files, path string) (*Baseline, bool, error) {
	b := &Baseline{Path: path, entries: map[string]int{}}
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return b, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(data, &b.file); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	if b.file.Version != 1 {
		return nil, false, fmt.Errorf("%s: version must be 1", path)
	}
	for _, e := range b.file.Entries {
		b.entries[e]++
	}
	return b, true, nil
}

// Has reports whether a fingerprint is grandfathered.
func (b *Baseline) Has(fp string) bool { return b.entries[fp] > 0 }

// Len is the number of grandfathered findings.
func (b *Baseline) Len() int {
	n := 0
	for _, c := range b.entries {
		n += c
	}
	return n
}

// Apply marks findings present in the baseline and downgrades results whose
// findings are all covered (baselined or excepted). It returns how many
// entries matched, so the caller can report paid-off debt.
func (b *Baseline) Apply(results []model.Result) (used int) {
	left := make(map[string]int, len(b.entries))
	for fp, c := range b.entries {
		left[fp] = c
	}
	for i := range results {
		res := &results[i]
		if res.Status != model.StatusFail {
			continue
		}
		covered := 0
		for j := range res.Findings {
			f := &res.Findings[j]
			if f.Exception != nil {
				covered++
				continue
			}
			if fp := Fingerprint(res.Rule.ID, res.Scope, *f); left[fp] > 0 {
				f.Baselined = true
				left[fp]--
				used++
				covered++
			}
		}
		if len(res.Findings) > 0 && covered == len(res.Findings) {
			res.Status = model.StatusBaselined
		}
	}
	return used
}

// Build computes the entries for the current failing findings, one per
// finding, so duplicates are counted.
func Build(results []model.Result) []string {
	var out []string
	for _, res := range results {
		if res.Status != model.StatusFail && res.Status != model.StatusBaselined {
			continue
		}
		for _, f := range res.Findings {
			if f.Exception == nil {
				out = append(out, Fingerprint(res.Rule.ID, res.Scope, f))
			}
		}
	}
	sort.Strings(out)
	return out
}

// Write stores entries. Without reset, entries beyond what a non-empty
// existing baseline holds (a new fingerprint, or one more copy of one) are
// refused, so the baseline can only shrink.
func Write(fsys Files, path string, entries []string, existing *Baseline, reset bool, now time.Time) (added, removed int, err error) {
	fresh := reset || existing == nil
	left := map[string]int{}
	if existing != nil {
		for fp, c := range existing.entries {
			left[fp] = c
		}
	}
	keep := make([]string, 0, len(entries))
	for _, e := range entries {
		switch {
		case left[e] > 0:
			left[e]--
			keep = append(keep, e)
		case fresh:
			keep = append(keep, e)
			added++
		}
	}
	if existing != nil {
		removed = existing.Len() - (len(keep) - added)
	}
	f := File{Version: 1, UpdatedAt: now.UTC().Format(time.RFC3339), Entries: keep}
	f.CreatedAt = f.UpdatedAt
	if existing != nil && existing.file.CreatedAt != "" {
		f.CreatedAt = existing.file.CreatedAt
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return 0, 0, err
	}
	return added, removed, fsys.WriteFile(path, append(data, '\n'), false)
}
