// Package model holds the types shared by every layer of fleetlint: rule
// metadata, findings and results. It has no dependencies on the rest of the
// tool so that catalogs, the engine and reporters agree on one vocabulary.
package model

import (
	"fmt"
	"strings"
)

// Severity is how much a failed rule matters. Order matters: higher is worse.
type Severity int

const (
	// SeverityInfo is advisory and never affects the exit code.
	SeverityInfo Severity = iota
	// SeverityWarning is reported but passes unless --fail-on warning.
	SeverityWarning
	// SeverityError fails the run.
	SeverityError
)

var severityNames = map[Severity]string{
	SeverityInfo:    "info",
	SeverityWarning: "warning",
	SeverityError:   "error",
}

// String returns the lower-case name used in config files and reports.
func (s Severity) String() string {
	if n, ok := severityNames[s]; ok {
		return n
	}
	return fmt.Sprintf("severity(%d)", int(s))
}

// ParseSeverity converts a config value to a Severity.
func ParseSeverity(s string) (Severity, error) {
	for sev, name := range severityNames {
		if strings.EqualFold(name, s) {
			return sev, nil
		}
	}
	return SeverityInfo, fmt.Errorf("unknown severity %q (want error, warning or info)", s)
}

// MarshalText implements encoding.TextMarshaler so JSON and YAML show names.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (s *Severity) UnmarshalText(b []byte) error {
	v, err := ParseSeverity(string(b))
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// Kind is how a rule is implemented.
type Kind string

const (
	// KindExpr is a CEL predicate declared in a catalog.
	KindExpr Kind = "expr"
	// KindGo is a rule implemented in Go and registered by id.
	KindGo Kind = "go"
	// KindCommand is a local executable (only the repo's own config may declare one).
	KindCommand Kind = "command"
	// KindOutcome asks whether a result is achieved and accepts several
	// satisfiers as evidence; repositories may add satisfiers, never remove
	// the requirement.
	KindOutcome Kind = "outcome"
)

// Satisfier is one way an outcome can be achieved. The first satisfier whose
// `when` holds (if any) and whose `expr` is true makes the rule pass, and its
// name is the evidence.
type Satisfier struct {
	Name string `yaml:"name" json:"name"`
	When string `yaml:"when,omitempty" json:"when,omitempty"`
	Expr string `yaml:"expr" json:"expr"`
}

// Partial is a graded outcome: something is in place but not enough. When no
// satisfier passes, the first matching partial produces a finding at its own
// severity instead of the rule's.
type Partial struct {
	Name     string   `yaml:"name" json:"name"`
	Expr     string   `yaml:"expr" json:"expr"`
	Message  string   `yaml:"message" json:"message"`
	Severity Severity `yaml:"severity" json:"severity"`
}

// Scope says where in a repository a rule is evaluated.
type Scope string

const (
	// ScopeEach evaluates the rule in the root and in every scope (default).
	ScopeEach Scope = "each"
	// ScopeRoot evaluates the rule only at the repository root: hooks, CI,
	// licenses and other things that exist once per repository.
	ScopeRoot Scope = "root"
	// ScopeMembers evaluates the rule only inside workspace members, never at
	// a root that merely hosts them.
	ScopeMembers Scope = "members"
)

// Fix describes how a finding is resolved: text for a person, text for an agent.
type Fix struct {
	Human string `yaml:"human,omitempty" json:"human,omitempty"`
	Agent string `yaml:"agent,omitempty" json:"agent,omitempty"`
	// Actions are declarative, mechanical steps `fleetlint fix` can apply.
	// Keys: template, to, untrack, gitignore. Decoded by the fix package.
	Actions []map[string]any `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// Rule is the metadata every rule carries regardless of kind. The
// documentation is generated from it, so every field is user-facing.
type Rule struct {
	ID       string   `yaml:"id" json:"id"`
	Title    string   `yaml:"title" json:"title"`
	Kind     Kind     `yaml:"kind" json:"kind"`
	Severity Severity `yaml:"severity" json:"severity"`
	Tiers    []int    `yaml:"tiers,omitempty" json:"tiers,omitempty"`
	Stacks   []string `yaml:"stacks,omitempty" json:"stacks,omitempty"`
	Scope    Scope    `yaml:"scope,omitempty" json:"scope,omitempty"`
	When     string   `yaml:"when,omitempty" json:"when,omitempty"`
	// Foreach is a CEL expression yielding a list; Expr is then evaluated once
	// per element as `item`, and every false element is its own finding.
	Foreach     string         `yaml:"foreach,omitempty" json:"foreach,omitempty"`
	Expr        string         `yaml:"expr,omitempty" json:"expr,omitempty"`
	Satisfiers  []Satisfier    `yaml:"satisfiers,omitempty" json:"satisfiers,omitempty"`
	Partials    []Partial      `yaml:"partials,omitempty" json:"partials,omitempty"`
	Run         string         `yaml:"run,omitempty" json:"run,omitempty"`
	Message     string         `yaml:"message,omitempty" json:"message,omitempty"`
	Requirement string         `yaml:"requirement,omitempty" json:"requirement,omitempty"`
	Rationale   string         `yaml:"rationale,omitempty" json:"rationale,omitempty"`
	Fix         Fix            `yaml:"fix,omitempty" json:"fix,omitempty"`
	Params      map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
	Refs        []string       `yaml:"refs,omitempty" json:"refs,omitempty"`
	Since       string         `yaml:"since,omitempty" json:"since,omitempty"`
	// Policy fields for organization catalogs. Locked: lower layers cannot
	// disable the rule, lower its severity or redefine it. MinSeverity: a
	// floor below which no layer may set the severity. AllowExceptions
	// (`exceptions: false`) forbids per-finding exceptions entirely.
	Locked          bool   `yaml:"locked,omitempty" json:"locked,omitempty"`
	MinSeverity     string `yaml:"min_severity,omitempty" json:"min_severity,omitempty"`
	AllowExceptions *bool  `yaml:"exceptions,omitempty" json:"exceptions,omitempty"`
	// Source is the catalog the rule came from, filled by the loader.
	Source string `yaml:"-" json:"source,omitempty"`
	// Layer is who owns the definition: preset, org, team or repo.
	Layer string `yaml:"-" json:"layer,omitempty"`
}

// ExceptionsAllowed reports whether per-finding exceptions may target the rule.
func (r Rule) ExceptionsAllowed() bool { return r.AllowExceptions == nil || *r.AllowExceptions }

// Floor returns the lowest severity a lower layer may set: the min_severity
// when declared, the catalog severity when the rule is locked, info otherwise.
func (r Rule) Floor() Severity {
	if r.MinSeverity != "" {
		if s, err := ParseSeverity(r.MinSeverity); err == nil {
			return s
		}
	}
	if r.Locked {
		return r.Severity
	}
	return SeverityInfo
}

// AppliesToTier reports whether the rule is in scope for a repo tier.
// An empty Tiers list means every tier.
func (r Rule) AppliesToTier(tier int) bool {
	if len(r.Tiers) == 0 {
		return true
	}
	for _, t := range r.Tiers {
		if t == tier {
			return true
		}
	}
	return false
}

// AppliesInScope reports whether the rule runs at this scope path ("" = root).
func (r Rule) AppliesInScope(path string, rootHostsMembers bool) bool {
	switch r.Scope {
	case ScopeRoot:
		return path == ""
	case ScopeMembers:
		return path != ""
	case ScopeEach, "":
	}
	// A workspace root that only hosts members has no code of its own;
	// per-stack rules there would be noise.
	return path != "" || !rootHostsMembers || len(r.Stacks) == 0
}

// AppliesToStacks reports whether the rule is in scope for the detected stacks.
// An empty Stacks list, or "any", means every stack.
func (r Rule) AppliesToStacks(stacks []string) bool {
	if len(r.Stacks) == 0 {
		return true
	}
	for _, want := range r.Stacks {
		if want == "any" {
			return true
		}
		for _, have := range stacks {
			if want == have {
				return true
			}
		}
	}
	return false
}

// Status is the outcome of evaluating one rule against one scope.
type Status string

const (
	// StatusPass means the requirement is met.
	StatusPass Status = "pass"
	// StatusFail means at least one finding at the rule's severity.
	StatusFail Status = "fail"
	// StatusExcepted means every finding was covered by an exception.
	StatusExcepted Status = "excepted"
	// StatusBaselined means every finding is grandfathered by the baseline file.
	StatusBaselined Status = "baselined"
	// StatusNotApplicable means tier, stack or `when` excluded the rule.
	StatusNotApplicable Status = "n/a"
	// StatusError means the rule itself could not run; never counts as pass.
	StatusError Status = "error"
)

// Finding is one concrete problem a rule reports.
type Finding struct {
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
	// Evidence names the satisfier or detail that produced the finding.
	Evidence string `json:"evidence,omitempty"`
	// Exception is set when an exception covered this finding.
	Exception *Exception `json:"exception,omitempty"`
	// Baselined is true when the baseline file grandfathers this finding.
	Baselined bool `json:"baselined,omitempty"`
}

// Exception is a deliberate, reasoned acceptance of a finding.
type Exception struct {
	Rule   string `yaml:"rule" json:"rule"`
	Match  string `yaml:"match,omitempty" json:"match,omitempty"`
	Reason string `yaml:"reason" json:"reason"`
	Until  string `yaml:"until,omitempty" json:"until,omitempty"`
	// Expired is computed at run time.
	Expired bool `yaml:"-" json:"expired,omitempty"`
}

// Result is a rule's outcome for one scope.
type Result struct {
	Rule     Rule      `json:"rule"`
	Scope    string    `json:"scope,omitempty"`
	Status   Status    `json:"status"`
	Severity Severity  `json:"severity"`
	Findings []Finding `json:"findings,omitempty"`
	// Evidence explains a pass (which satisfier matched) or an n/a (why).
	Evidence string `json:"evidence,omitempty"`
	Err      string `json:"error,omitempty"`
	// Fixable is true for a failing result that `fleetlint fix --apply` can
	// repair in this repository as it is now.
	Fixable bool `json:"fixable,omitempty"`
}

// Counts summarizes results by status.
type Counts struct {
	Pass, Fail, Excepted, Baselined, NotApplicable, Error int
	Errors, Warnings, Infos                               int
}

// Summarize tallies a result set.
func Summarize(results []Result) Counts {
	var c Counts
	for _, r := range results {
		switch r.Status {
		case StatusPass:
			c.Pass++
		case StatusExcepted:
			c.Excepted++
		case StatusBaselined:
			c.Baselined++
		case StatusNotApplicable:
			c.NotApplicable++
		case StatusError:
			c.Error++
		case StatusFail:
			c.Fail++
			switch r.Severity {
			case SeverityError:
				c.Errors++
			case SeverityWarning:
				c.Warnings++
			case SeverityInfo:
				c.Infos++
			}
		}
	}
	return c
}
