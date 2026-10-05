// Package baseline grandfathers existing findings so a repository can adopt
// rules today and be held to them for new code only. A baseline is a set of
// finding fingerprints; matching findings are reported as baselined, not as
// failures. The set may only shrink: writing a baseline never adds entries to
// an existing one unless the caller asks to reset it.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// Baseline is the loaded set.
type Baseline struct {
	Path    string
	entries map[string]bool
	file    File
}

// Fingerprint identifies a finding independent of line numbers and run order:
// rule id, scope, path and message.
func Fingerprint(rule, scope string, f model.Finding) string {
	sum := sha256.Sum256([]byte(rule + "\x00" + scope + "\x00" + f.Path + "\x00" + f.Message))
	return hex.EncodeToString(sum[:8])
}

// Load reads a baseline; a missing file yields an empty baseline and ok=false.
func Load(path string) (*Baseline, bool, error) {
	b := &Baseline{Path: path, entries: map[string]bool{}}
	data, err := os.ReadFile(path) //nolint:gosec // the path is the baseline file named in config or the default
	if errors.Is(err, os.ErrNotExist) {
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
		b.entries[e] = true
	}
	return b, true, nil
}

// Has reports whether a fingerprint is grandfathered.
func (b *Baseline) Has(fp string) bool { return b.entries[fp] }

// Len is the number of grandfathered findings.
func (b *Baseline) Len() int { return len(b.entries) }

// Apply marks findings present in the baseline and downgrades results whose
// findings are all covered (baselined or excepted). It returns how many
// entries matched, so the caller can report paid-off debt.
func (b *Baseline) Apply(results []model.Result) (used int) {
	seen := map[string]bool{}
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
			if fp := Fingerprint(res.Rule.ID, res.Scope, *f); b.entries[fp] {
				f.Baselined = true
				seen[fp] = true
				covered++
			}
		}
		if len(res.Findings) > 0 && covered == len(res.Findings) {
			res.Status = model.StatusBaselined
		}
	}
	return len(seen)
}

// Build computes the entries for the current failing findings.
func Build(results []model.Result) []string {
	set := map[string]bool{}
	for _, res := range results {
		if res.Status != model.StatusFail && res.Status != model.StatusBaselined {
			continue
		}
		for _, f := range res.Findings {
			if f.Exception == nil {
				set[Fingerprint(res.Rule.ID, res.Scope, f)] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for fp := range set {
		out = append(out, fp)
	}
	sort.Strings(out)
	return out
}

// Write stores entries. Without reset, entries not already in a non-empty
// existing baseline are refused, so the baseline can only shrink.
func Write(path string, entries []string, existing *Baseline, reset bool, now time.Time) (added, removed int, err error) {
	fresh := reset || existing == nil
	keep := make([]string, 0, len(entries))
	for _, e := range entries {
		switch {
		case existing != nil && existing.entries[e]:
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
	return added, removed, os.WriteFile(path, append(data, '\n'), 0o644) //nolint:gosec // a committed project file
}
