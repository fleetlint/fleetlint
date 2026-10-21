package fleet

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
)

// jsonFormat versions fleet.json. 2: results name their rule by id and the
// definitions are stored once, under `definitions`. 3: summary fields are
// snake_case, its rule-error count is rule_errors. 4: each repository
// lists its catalogs in the order they applied, with their pins. 5: a rule's
// refs are objects {url, kind, section}.
const jsonFormat = 6

type reportOut struct {
	Version     int                   `json:"version"`
	GeneratedAt time.Time             `json:"generated_at"`
	Tool        string                `json:"tool,omitempty"`
	Catalog     string                `json:"catalog,omitempty"`
	Rules       []string              `json:"rules"`
	Definitions map[string]model.Rule `json:"definitions"`
	Repos       []repoOut             `json:"repos"`
	Teams       []TeamSummary         `json:"teams,omitempty"`
	Groups      []TeamSummary         `json:"groups,omitempty"`
	Unversioned []string              `json:"unversioned,omitempty"`
}

type repoOut struct {
	Name       string            `json:"name"`
	Team       string            `json:"team,omitempty"`
	Group      string            `json:"group,omitempty"`
	Source     string            `json:"source"`
	Git        *GitState         `json:"git,omitempty"`
	Facts      facts.Facts       `json:"facts"`
	Compliance float64           `json:"compliance"`
	Fixable    int               `json:"fixable"`
	Summary    model.Counts      `json:"summary"`
	Results    []resultOut       `json:"results"`
	Disabled   []config.Disabled `json:"disabled,omitempty"`
	Weakened   []config.Disabled `json:"weakened,omitempty"`
	Exceptions []model.Exception `json:"exceptions,omitempty"`
	// Catalogs are the repository's catalogs in the order they applied,
	// with their pins.
	Catalogs []config.CatalogInfo `json:"catalogs,omitempty"`
	Err      string               `json:"error,omitempty"`
}

// resultOut is a result without its rule: Rule is the id, and Definition
// the key in `definitions` when this repository's definition differs from
// the one stored under the id (a repository adjusted its parameters).
type resultOut struct {
	Rule       string          `json:"rule"`
	Definition string          `json:"definition,omitempty"`
	Scope      string          `json:"scope,omitempty"`
	Status     model.Status    `json:"status"`
	Severity   model.Severity  `json:"severity"`
	Findings   []model.Finding `json:"findings,omitempty"`
	Evidence   string          `json:"evidence,omitempty"`
	Err        string          `json:"error,omitempty"`
	Fixable    bool            `json:"fixable,omitempty"`
}

// MarshalJSON writes each rule definition once instead of once per result:
// a fleet of fifty repositories otherwise repeats the catalog fifty times.
func (rep *Report) MarshalJSON() ([]byte, error) {
	out := reportOut{
		Version: jsonFormat, GeneratedAt: rep.GeneratedAt, Tool: rep.Tool, Catalog: rep.Catalog, Rules: rep.Rules,
		Definitions: map[string]model.Rule{}, Teams: rep.Teams, Groups: rep.Groups, Unversioned: rep.Unversioned,
	}
	variants := map[string][]string{} // id -> keys in definitions, first is the id itself
	for _, r := range rep.Repos {
		ro := repoOut{
			Name: r.Name, Team: r.Team, Group: r.Group, Source: r.Source, Git: r.Git, Facts: r.Facts,
			Compliance: r.Compliance(), Fixable: r.Fixable(), Summary: r.Summary,
			Disabled: r.Disabled, Weakened: r.Weakened, Exceptions: r.Exceptions, Catalogs: r.Catalogs, Err: r.Err,
			Results: make([]resultOut, 0, len(r.Results)),
		}
		for _, res := range r.Results {
			ro.Results = append(ro.Results, resultOut{
				Rule: res.Rule.ID, Definition: definitionKey(out.Definitions, variants, res.Rule), Scope: res.Scope,
				Status: res.Status, Severity: res.Severity, Findings: res.Findings, Evidence: res.Evidence, Err: res.Err, Fixable: res.Fixable,
			})
		}
		out.Repos = append(out.Repos, ro)
	}
	return json.Marshal(out)
}

// definitionKey stores the rule if no equal definition is stored yet and
// returns its key, or "" when it is the one stored under its id.
func definitionKey(defs map[string]model.Rule, variants map[string][]string, rule model.Rule) string {
	for _, key := range variants[rule.ID] {
		if reflect.DeepEqual(defs[key], rule) {
			if key == rule.ID {
				return ""
			}
			return key
		}
	}
	key := rule.ID
	if n := len(variants[rule.ID]); n > 0 {
		key = fmt.Sprintf("%s#%d", rule.ID, n+1)
	}
	variants[rule.ID] = append(variants[rule.ID], key)
	defs[key] = rule
	if key == rule.ID {
		return ""
	}
	return key
}
