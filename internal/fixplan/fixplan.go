// Package fixplan turns the failing results of a run into the changes
// `fleetlint fix` makes: it picks each rule's actions, the scope's stack and
// the templates of the run's catalogs, and leaves applying them to fix.
package fixplan

import (
	"fmt"
	"strings"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// Plan plans the failing rules of a run (all, or those in only); a rule
// that cannot be planned stops the plan.
func Plan(r *repo.Repo, run *engine.Run, only []string) ([]fix.Change, []model.Result, error) {
	return PlanEach(r, run, only, func(id string, err error) error { return fmt.Errorf("rule %s: %w", id, err) })
}

// PlanEach plans the failing rules and returns the changes and the
// results whose fix is not mechanical. onErr decides whether a rule that
// cannot be planned stops the plan (an error) or is passed over (nil).
func PlanEach(r *repo.Repo, run *engine.Run, only []string, onErr func(id string, err error) error) ([]fix.Change, []model.Result, error) {
	want := map[string]bool{}
	for _, id := range only {
		want[id] = true
	}
	var changes []fix.Change
	var manual []model.Result
	seen := map[string]bool{}
	for _, res := range run.Results {
		if res.Status != model.StatusFail || (len(want) > 0 && !want[res.Rule.ID]) {
			continue
		}
		planned, err := planRule(r, run, res)
		if err != nil {
			if err := onErr(res.Rule.ID, err); err != nil {
				return nil, nil, err
			}
			continue
		}
		if len(planned) == 0 {
			manual = append(manual, res)
			continue
		}
		changes = appendUnique(changes, seen, planned)
	}
	return changes, manual, nil
}

// MarkFixable flags the failing results whose fix is mechanical here: the
// rule has actions and they plan at least one change for this repository.
// A rule with a template is not fixable when the file already exists.
// The planned file edits go on the result for SARIF.
func MarkFixable(r *repo.Repo, run *engine.Run) {
	for i := range run.Results {
		res := &run.Results[i]
		if res.Status != model.StatusFail {
			continue
		}
		planned, err := planRule(r, run, *res)
		res.Fixable = err == nil && len(planned) > 0
		res.Edits = nil
		if res.Fixable {
			res.Edits = fileEdits(planned)
		}
	}
}

// fileEdits are the changes as text edits; none when the plan untracks a
// file or changes one file twice, which one edit of the original cannot say.
func fileEdits(changes []fix.Change) []model.FileEdit {
	seen := map[string]bool{}
	out := make([]model.FileEdit, 0, len(changes))
	for _, c := range changes {
		if c.Kind == "untrack" || seen[c.Path] {
			return nil
		}
		seen[c.Path] = true
		out = append(out, model.FileEdit{Path: c.Path, Old: c.Old, New: c.New})
	}
	return out
}

// appendUnique drops changes already planned: the root scope sees member
// files too, so one file can be planned from two scopes, and applying the
// same untrack or create twice fails. Different edits of one file stay;
// each applies to the file as the previous one left it.
func appendUnique(changes []fix.Change, seen map[string]bool, planned []fix.Change) []fix.Change {
	for _, ch := range planned {
		key := ch.Kind + " " + ch.Path
		if ch.Kind == "edit" {
			key += "\n" + ch.Diff
		}
		if ch.Kind != "append" && seen[key] {
			continue
		}
		seen[key] = true
		changes = append(changes, ch)
	}
	return changes
}

// planRule returns the changes for one failing rule in its scope, with paths
// made repo-relative; an empty result means the fix is not mechanical.
func planRule(r *repo.Repo, run *engine.Run, res model.Result) ([]fix.Change, error) {
	actions, err := fix.Decode(res.Rule.Fix.Actions)
	if err != nil || len(actions) == 0 {
		return nil, err
	}
	scope := r
	if res.Scope != "" {
		var err error
		if scope, err = r.Scoped(res.Scope); err != nil {
			return nil, err
		}
	}
	p := project(run, res.Scope)
	p.Vars = map[string]string{}
	if id := fix.DetectLicense(r); id != "" {
		p.Vars["license"] = id
	}
	tpl := templatesFor(run)
	planned, err := fix.PlanFor(scope, p, actions, tpl, res.Findings)
	if err != nil {
		return nil, err
	}
	if o, ok := tpl.(interface{ Origins() []string }); ok {
		from := strings.Join(o.Origins(), ", ")
		for i := range planned {
			planned[i].From = from
		}
	}
	if res.Scope != "" {
		for i := range planned {
			planned[i].Path = res.Scope + "/" + planned[i].Path
		}
	}
	return planned, nil
}

// templatesFor returns the templates of the run's configuration: files the
// extended catalogs ship, over the pinned or built-in catalog's.
func templatesFor(run *engine.Run) fix.Templates {
	if run.Config != nil {
		return run.Config.FixTemplates()
	}
	return catalog.EmbeddedTemplates{}
}

// project describes a scope for the templates: its stack and, for the root,
// the nested projects that share its hook configuration and workflow.
func project(run *engine.Run, scope string) fix.Project {
	p := fix.Project{Stack: primaryStack(run, scope), Runner: run.Facts.TaskRunner.Kind}
	if scope == "" {
		p.Container = run.Facts.Container
	}
	for _, s := range run.Scopes {
		// Further stacks in the same directory share its hooks and workflow.
		if s.Path == scope && len(s.Facts.Stacks) > 1 {
			for _, extra := range s.Facts.Stacks[1:] {
				p.Nested = append(p.Nested, fix.Nested{Stack: extra})
			}
		}
	}
	if scope != "" {
		return p
	}
	for _, s := range run.Scopes {
		if s.Path != "" && len(s.Facts.Stacks) > 0 {
			p.Nested = append(p.Nested, fix.Nested{Path: s.Path, Stack: s.Facts.Stacks[0]})
		}
	}
	return p
}

func primaryStack(run *engine.Run, scope string) string {
	for _, s := range run.Scopes {
		if s.Path == scope && len(s.Facts.Stacks) > 0 {
			return s.Facts.Stacks[0]
		}
	}
	return ""
}
