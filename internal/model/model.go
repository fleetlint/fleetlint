// Package model holds the types shared by every layer of fleetlint: rule
// metadata, findings and results. It has no dependencies on the rest of the
// tool so that catalogs, the engine and reporters agree on one vocabulary.
package model

import (
	"errors"
	"fmt"
	"slices"
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
	// KindCapability states a requirement without checking it; its
	// implementations, one per stack (rules/<family>/<name>/<stack>.yaml),
	// check it with the stack's tools.
	KindCapability Kind = "capability"
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
	Foreach    string      `yaml:"foreach,omitempty" json:"foreach,omitempty"`
	Expr       string      `yaml:"expr,omitempty" json:"expr,omitempty"`
	Satisfiers []Satisfier `yaml:"satisfiers,omitempty" json:"satisfiers,omitempty"`
	// Accept are satisfiers a repository or later catalog added with
	// `accept:`; they are tried after the rule's own.
	Accept   []Satisfier `yaml:"-" json:"accept,omitempty"`
	Partials []Partial   `yaml:"partials,omitempty" json:"partials,omitempty"`
	Run      string      `yaml:"run,omitempty" json:"run,omitempty"`
	// Locate is a CEL expression yielding the scope-relative file a finding
	// of the rule is about when the finding names no path (`'"README.md"'`,
	// `taskrunner.file`); reports point there. It sets the finding's
	// Anchor, never its Path, so identity and fixes are unaffected.
	Locate      string         `yaml:"locate,omitempty" json:"locate,omitempty"`
	Message     string         `yaml:"message,omitempty" json:"message,omitempty"`
	Requirement string         `yaml:"requirement,omitempty" json:"requirement,omitempty"`
	Rationale   string         `yaml:"rationale,omitempty" json:"rationale,omitempty"`
	Fix         Fix            `yaml:"fix,omitempty" json:"fix,omitempty"`
	Params      map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
	Refs        []Ref          `yaml:"refs,omitempty" json:"refs,omitempty"`
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
	// Use, in a catalog's rules list, selects rules from the source's library
	// (an id or a glob) instead of defining one; the loader expands it.
	Use string `yaml:"use,omitempty" json:"-"`
	// SelectedBy is the catalog that took a library rule in with `use:`;
	// Source then names the library. Empty for an inline definition.
	SelectedBy string `yaml:"-" json:"selected_by,omitempty"`
	// Label is who owns the definition: preset, org, team or repo.
	Label string `yaml:"-" json:"label,omitempty"`
	// PolicyBy is the catalog that locked the rule or set its floor when that
	// is not the catalog that defined it.
	PolicyBy string `yaml:"-" json:"policy_by,omitempty"`
	// By, on a capability, says what its implementations are keyed by: a
	// stack (the default) or a published artifact kind. Implementations
	// inherit it.
	By By `yaml:"by,omitempty" json:"by,omitempty"`
	// Capability, Stack and Artifact are set on an implementation (an id of
	// three segments, family/name/<target>): the capability it implements
	// and the stack it does so for ("any" for every stack without its own),
	// or the artifact kind when the capability is keyed by artifact.
	Capability string `yaml:"-" json:"capability,omitempty"`
	Stack      string `yaml:"-" json:"stack,omitempty"`
	Artifact   string `yaml:"-" json:"artifact,omitempty"`
	// Gate is the capability's `when`; it is evaluated before the
	// implementation's own.
	Gate string `yaml:"-" json:"gate,omitempty"`
	// Implementations lists, on a capability, the stacks (or artifact
	// kinds) it has an implementation for.
	Implementations []string `yaml:"-" json:"implementations,omitempty"`
	// Combine, on a capability, says how its implementations add up in a
	// scope: every one must pass (all, the default) or one passing is
	// enough (any). Implementations inherit it.
	Combine Combine `yaml:"combine,omitempty" json:"combine,omitempty"`
}

// By is what a capability's implementations are keyed by.
type By string

const (
	// ByStack keys implementations by stack (rules/<family>/<name>/<stack>.yaml).
	ByStack By = "stack"
	// ByArtifact keys implementations by published artifact kind
	// (rules/release/<name>/<artifact>.yaml); each kind the release
	// publishes is checked.
	ByArtifact By = "artifact"
)

// Target is the stack or artifact kind an implementation is for.
func (r Rule) Target() string {
	if r.Artifact != "" {
		return r.Artifact
	}
	return r.Stack
}

// Combine is how a capability's implementations add up in a scope.
type Combine string

const (
	// CombineAll needs every implementation that runs to pass: each stack
	// has its own linter, so a Go and Node repository needs both.
	CombineAll Combine = "all"
	// CombineAny needs one: a release belongs to the repository, so the
	// stack that publishes it decides, not every stack present.
	CombineAny Combine = "any"
)

// SplitImplementation splits an implementation id (family/name/stack) into
// the capability id and the stack; ok is false for any other id.
func SplitImplementation(id string) (capability, stack string, ok bool) {
	i := strings.LastIndex(id, "/")
	if i < 0 || strings.Count(id, "/") != 2 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
}

// Covers reports whether an override, exception or selection naming key
// reaches this rule: its own id, or the capability it implements.
func (r Rule) Covers(key string) bool {
	return r.ID == key || (r.Capability != "" && r.Capability == key)
}

// Clone copies the rule with its own params, lists and actions, so a
// layer that adjusts it never changes the definition it came from (library
// rules are shared by every repository of a run).
func (r Rule) Clone() Rule {
	r.Tiers = slices.Clone(r.Tiers)
	r.Stacks = slices.Clone(r.Stacks)
	r.Satisfiers = slices.Clone(r.Satisfiers)
	r.Accept = slices.Clone(r.Accept)
	r.Partials = slices.Clone(r.Partials)
	r.Refs = slices.Clone(r.Refs)
	r.Implementations = slices.Clone(r.Implementations)
	r.Params, _ = deepCopy(r.Params).(map[string]any)
	actions := make([]map[string]any, len(r.Fix.Actions))
	for i, a := range r.Fix.Actions {
		actions[i], _ = deepCopy(a).(map[string]any)
	}
	if r.Fix.Actions != nil {
		r.Fix.Actions = actions
	}
	if r.AllowExceptions != nil {
		v := *r.AllowExceptions
		r.AllowExceptions = &v
	}
	return r
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if t == nil {
			return t
		}
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = deepCopy(val)
		}
		return m
	case []any:
		l := make([]any, len(t))
		for i, val := range t {
			l[i] = deepCopy(val)
		}
		return l
	}
	return v
}

// PolicySource names the catalog whose lock or floor applies to the rule.
func (r Rule) PolicySource() string {
	if r.PolicyBy != "" {
		return r.PolicyBy
	}
	return r.Source
}

// AcceptedSatisfiers returns the `accept:` satisfiers, each named
// (accept[i] when it has no name of its own).
func (r Rule) AcceptedSatisfiers() []Satisfier {
	out := make([]Satisfier, 0, len(r.Accept))
	for i, s := range r.Accept {
		if s.Name == "" {
			s.Name = fmt.Sprintf("accept[%d]", i)
		}
		if s.Expr != "" {
			out = append(out, s)
		}
	}
	return out
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

// AppliesToArtifacts reports whether an artifact-keyed implementation is in
// scope: its kind is among the published ones. Other rules always apply.
func (r Rule) AppliesToArtifacts(kinds []string) bool {
	if r.Artifact == "" {
		return true
	}
	return slices.Contains(kinds, r.Artifact)
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
	// Path and Line are where the problem was observed; they and Message
	// are the finding's identity for baselines and exceptions.
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
	// Anchor is where a report points when Path is empty: the file the rule
	// says the finding is about (its `locate`). Presentation only.
	Anchor string `json:"anchor,omitempty"`
	// Artifact names the published artifact the finding is about, for an
	// implementation keyed by artifact whose kind names its instances.
	Artifact string `json:"artifact,omitempty"`
	// Evidence names the satisfier or detail that produced the finding.
	Evidence string `json:"evidence,omitempty"`
	// Exception is set when an exception covered this finding.
	Exception *Exception `json:"exception,omitempty"`
	// Baselined is true when the baseline file grandfathers this finding.
	Baselined bool `json:"baselined,omitempty"`
}

// Where is the file a report points at: the path, else the anchor.
func (f Finding) Where() string {
	if f.Path != "" {
		return f.Path
	}
	return f.Anchor
}

// Text is the message as reports show it, after the artifact's name.
func (f Finding) Text() string {
	if f.Artifact == "" {
		return f.Message
	}
	return f.Artifact + ": " + f.Message
}

// Ref is where a rule's practice comes from. In YAML a bare URL string or
// `{url, kind, section}`; the kinds are RefKinds.
type Ref struct {
	URL     string `yaml:"url" json:"url"`
	Kind    string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Section string `yaml:"section,omitempty" json:"section,omitempty"`
}

// RefKinds lists the kinds, strongest first: a language or format specification or an
// official tool; official guidance; a public style guide; a standard for
// process or supply chain (SSDF, SLSA, Scorecard, ASVS); a linter check
// (section is its id); a convention of this catalog with no outside source.
var RefKinds = []string{"spec", "guidance", "style-guide", "standard", "check", "convention"}

// UnmarshalYAML accepts `https://...` and `{url: ..., kind: ..., section: ...}`.
func (r *Ref) UnmarshalYAML(unmarshal func(any) error) error {
	var url string
	if err := unmarshal(&url); err == nil {
		*r = Ref{URL: url}
		return nil
	}
	type plain Ref
	var full plain
	if err := unmarshal(&full); err != nil {
		return err
	}
	*r = Ref(full)
	return nil
}

// Validate checks the URL and the kind.
func (r Ref) Validate() error {
	if r.URL == "" {
		return errors.New("ref without url")
	}
	if r.Kind != "" && !slices.Contains(RefKinds, r.Kind) {
		return fmt.Errorf("ref %s: kind %q is not one of %s", r.URL, r.Kind, strings.Join(RefKinds, ", "))
	}
	return nil
}

// Typed reports a ref that says what kind of source it is; a bare URL
// has not been classified.
func (r Ref) Typed() bool { return r.Kind != "" }

// String renders the ref for a report: the URL, then kind and section.
func (r Ref) String() string {
	var extra []string
	if r.Kind != "" {
		extra = append(extra, r.Kind)
	}
	if r.Section != "" {
		extra = append(extra, r.Section)
	}
	if len(extra) == 0 {
		return r.URL
	}
	return r.URL + " (" + strings.Join(extra, ", ") + ")"
}

// Exception is a deliberate, reasoned acceptance of a finding.
type Exception struct {
	Rule   string `yaml:"rule" json:"rule"`
	Match  string `yaml:"match,omitempty" json:"match,omitempty"`
	Reason string `yaml:"reason" json:"reason"`
	Until  string `yaml:"until,omitempty" json:"until,omitempty"`
	// Expired is computed at run time.
	Expired bool `yaml:"-" json:"expired,omitempty"`
	// Label and Where say who declared the exception: the repository, a
	// scope, or a catalog (by ref). Filled by the loader.
	Label string `yaml:"-" json:"label,omitempty"`
	Where string `yaml:"-" json:"where,omitempty"`
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
	Pass          int `json:"pass"`
	Fail          int `json:"fail"`
	Excepted      int `json:"excepted"`
	Baselined     int `json:"baselined"`
	NotApplicable int `json:"not_applicable"`
	// Error counts rules that could not run; Errors, Warnings and Infos
	// count failing rules by severity.
	Error    int `json:"rule_errors"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`
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
