package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/model"
)

// snapshot is what a run says about each repository: whether it could be
// checked and which rules failed there, at their worst severity.
type snapshot struct {
	At    time.Time
	Repos map[string]repoState
}

type repoState struct {
	Checked bool
	Failing map[string]model.Severity
}

func (s repoState) count(sev model.Severity) int {
	n := 0
	for _, v := range s.Failing {
		if v == sev {
			n++
		}
	}
	return n
}

func snapshotOf(rep *Report) *snapshot {
	s := &snapshot{At: rep.GeneratedAt, Repos: map[string]repoState{}}
	for _, r := range rep.Repos {
		st := repoState{Checked: r.Err == "", Failing: map[string]model.Severity{}}
		for _, res := range r.Results {
			if res.Status == model.StatusFail && (!hasKey(st.Failing, res.Rule.ID) || st.Failing[res.Rule.ID] < res.Severity) {
				st.Failing[res.Rule.ID] = res.Severity
			}
		}
		s.Repos[r.Name] = st
	}
	return s
}

func hasKey[K comparable, V any](m map[K]V, k K) bool { _, ok := m[k]; return ok }

// historyFile is the part of a fleet.json a comparison needs. Version 1
// wrote the whole rule in each result, versions 2 and 3 only its id.
type historyFile struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Repos       []struct {
		Name    string `json:"name"`
		Err     string `json:"error"`
		Results []struct {
			Rule     json.RawMessage `json:"rule"`
			Status   model.Status    `json:"status"`
			Severity model.Severity  `json:"severity"`
		} `json:"results"`
	} `json:"repos"`
}

// loadPrevious reads the latest readable run in dir/history from before
// now; nil when there is none. A file that does not parse, or comes from a
// newer fleetlint, is passed over for the one before it.
func loadPrevious(dir string, now time.Time) (*snapshot, error) {
	files, err := filepath.Glob(filepath.Join(dir, "history", "*.json"))
	if err != nil {
		return nil, err
	}
	current := now.UTC().Format(historyStamp)
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for _, f := range files {
		if strings.TrimSuffix(filepath.Base(f), ".json") >= current {
			continue
		}
		b, err := os.ReadFile(f) //nolint:gosec // the report's own history directory
		if err != nil {
			continue
		}
		var h historyFile
		if err := json.Unmarshal(b, &h); err != nil || h.Version < 1 || h.Version > jsonFormat {
			continue
		}
		return h.snapshot(), nil
	}
	return nil, nil
}

func (h historyFile) snapshot() *snapshot {
	s := &snapshot{At: h.GeneratedAt, Repos: map[string]repoState{}}
	for _, r := range h.Repos {
		st := repoState{Checked: r.Err == "", Failing: map[string]model.Severity{}}
		for _, res := range r.Results {
			id := ruleID(h.Version, res.Rule)
			if res.Status == model.StatusFail && id != "" && (!hasKey(st.Failing, id) || st.Failing[id] < res.Severity) {
				st.Failing[id] = res.Severity
			}
		}
		s.Repos[r.Name] = st
	}
	return s
}

// ruleID reads a result's rule: the whole definition in version 1, its id
// from version 2 on.
func ruleID(version int, raw json.RawMessage) string {
	if version >= 2 {
		var id string
		_ = json.Unmarshal(raw, &id)
		return id
	}
	var rule struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &rule)
	return rule.ID
}

// changes compares this run with the previous one: a line with the totals
// and a row per repository whose failing rules changed.
func (v *view) changes() table {
	t := table{
		Title: "Since the last run",
		Head:  []string{"Repository", "Errors", "Warnings", "Newly failing", "Fixed"},
	}
	prev := v.prev
	if prev == nil {
		return t
	}
	cur := snapshotOf(v.rep)
	var errsBefore, errsNow, newly, fixed int
	for _, name := range unionNames(prev, cur) {
		before, wasThere := prev.Repos[name]
		now, isThere := cur.Repos[name]
		errsBefore += before.count(model.SeverityError)
		errsNow += now.count(model.SeverityError)
		var row []tcell
		switch {
		case !wasThere:
			row = []tcell{txt(name), txt("new in the fleet"), txt(""), txt(""), txt("")}
		case !isThere:
			row = []tcell{txt(name), txt("no longer in the fleet"), txt(""), txt(""), txt("")}
		case before.Checked != now.Checked:
			row = []tcell{txt(name), checkedChange(now.Checked), txt(""), txt(""), txt("")}
		default:
			added, gone := diffRules(before.Failing, now.Failing)
			if len(added) == 0 && len(gone) == 0 {
				continue
			}
			newly += len(added)
			fixed += len(gone)
			row = []tcell{
				txt(name),
				delta(before.count(model.SeverityError), now.count(model.SeverityError)),
				delta(before.count(model.SeverityWarning), now.count(model.SeverityWarning)),
				{Text: strings.Join(added, ", "), Code: len(added) > 0, Class: classIf(len(added) > 0, "err")},
				{Text: strings.Join(gone, ", "), Code: len(gone) > 0, Class: classIf(len(gone) > 0, "ok")},
			}
		}
		row[0].Href = "actions/" + name + ".html"
		t.Rows = append(t.Rows, row)
	}
	t.Intro = fmt.Sprintf("Compared with the run of %s: rules failing at error severity %d -> %d; %d rule x repository pairs newly failing, %d fixed.",
		prev.At.Format("2006-01-02 15:04 MST"), errsBefore, errsNow, newly, fixed)
	if len(t.Rows) == 0 {
		t.Rows = [][]tcell{{txt("no changes"), txt(""), txt(""), txt(""), txt("")}}
	}
	return t
}

func unionNames(a, b *snapshot) []string {
	names := map[string]bool{}
	for n := range a.Repos {
		names[n] = true
	}
	for n := range b.Repos {
		names[n] = true
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	slices.SortFunc(out, func(x, y string) int { return strings.Compare(strings.ToLower(x), strings.ToLower(y)) })
	return out
}

// diffRules lists the rules failing now but not before, and the reverse;
// a rule whose severity changed counts as neither.
func diffRules(before, now map[string]model.Severity) (added, gone []string) {
	for id := range now {
		if !hasKey(before, id) {
			added = append(added, id)
		}
	}
	for id := range before {
		if !hasKey(now, id) {
			gone = append(gone, id)
		}
	}
	slices.Sort(added)
	slices.Sort(gone)
	return added, gone
}

func delta(before, now int) tcell {
	c := tcell{Text: fmt.Sprintf("%d -> %d", before, now), Sort: fmt.Sprint(now - before)}
	switch {
	case now > before:
		c.Class = "err"
	case now < before:
		c.Class = "ok"
	}
	return c
}

func checkedChange(now bool) tcell {
	if now {
		return tcell{Text: "checked again", Class: "ok"}
	}
	return tcell{Text: "could not be checked", Class: "err"}
}

func classIf(cond bool, class string) string {
	if cond {
		return class
	}
	return ""
}
