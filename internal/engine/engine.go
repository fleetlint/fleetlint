// Package engine evaluates the effective rule set against a repository and
// its scopes, applies exceptions, and returns results. It knows nothing
// about output formats or the command line.
package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/baseline"
	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/rules"
	"github.com/fleetlint/fleetlint/internal/stacks"
)

// Run is one evaluation of a repository.
type Run struct {
	Facts   facts.Facts
	Scopes  []ScopeRun
	Results []model.Result
	Config  *config.Effective
	// ScopeExceptions are exceptions declared by scopes or nested files, for reports.
	ScopeExceptions []model.Exception
	// Baseline describes the ratchet file, when one is configured.
	Baseline *BaselineInfo
}

// BaselineInfo summarizes how the baseline applied to this run.
type BaselineInfo struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Entries int    `json:"entries"`
	// Used is how many entries matched a current finding; Entries-Used is paid-off debt.
	Used int `json:"used"`
}

// ScopeRun records the facts of one scope (the root scope has Path "").
type ScopeRun struct {
	Path  string      `json:"path"`
	Facts facts.Facts `json:"facts"`
}

// Options tune an evaluation.
type Options struct {
	// Deep enables rules that execute repository code (make check, etc.).
	Deep bool
	// Now is injected for deterministic exception expiry in tests.
	Now time.Time
	// Visibility is what the forge reported for the repository, if anyone
	// asked; it is used when the repository itself does not say.
	Visibility facts.Visibility
	// Tier is what a fleet file sets; the configuration file wins over it.
	Tier int
}

// Evaluate runs every applicable rule for the root and each configured scope.
func Evaluate(ctx context.Context, r *repo.Repo, eff *config.Effective) (*Run, error) {
	return EvaluateWith(ctx, r, eff, Options{})
}

// EvaluateWith is Evaluate with options.
func EvaluateWith(ctx context.Context, r *repo.Repo, eff *config.Effective, opts Options) (*Run, error) {
	ov := eff.ScopeFacts(config.Scope{})
	ov.DetectedVisibility = opts.Visibility
	ov.FleetTier = opts.Tier
	root := facts.Discover(r, ov)
	run := &Run{Facts: root, Config: eff}

	sub := resolveScopes(root, eff)
	scopes := make([]config.Scope, 0, 1+len(sub))
	scopes = append(scopes, config.Scope{Path: ""})
	scopes = append(scopes, sub...)
	hostsMembers := len(scopes) > 1 && len(root.Stacks) == 0
	for _, sc := range scopes {
		sr, f, sc, err := prepareScope(r, root, eff, sc, opts)
		if err != nil {
			return nil, err
		}
		sc.Exceptions = eff.RenameExceptions(sc.Exceptions, sc.Path+"/"+config.FileName)
		if err := eff.CheckExceptions(sc.Path+"/"+config.FileName, sc.Exceptions); err != nil {
			return nil, err
		}
		run.Scopes = append(run.Scopes, ScopeRun{Path: sc.Path, Facts: f})
		run.ScopeExceptions = append(run.ScopeExceptions, sc.Exceptions...)
		results, err := evaluateScope(ctx, sr, f, eff, sc, hostsMembers, opts)
		if err != nil {
			return nil, err
		}
		run.Results = append(run.Results, results...)
	}
	if err := applyBaseline(run, r, eff); err != nil {
		return nil, err
	}
	return run, nil
}

func applyBaseline(run *Run, r *repo.Repo, eff *config.Effective) error {
	path := eff.File.Baseline
	if path == "" {
		if !r.Has(baseline.DefaultFile) {
			return nil
		}
		path = baseline.DefaultFile
	}
	b, exists, err := baseline.Load(r, path)
	if err != nil {
		return err
	}
	info := &BaselineInfo{Path: path, Exists: exists, Entries: b.Len()}
	if exists {
		info.Used = b.Apply(run.Results)
	}
	run.Baseline = info
	return nil
}

// prepareScope resolves a scope's repository view, nested config and facts.
func prepareScope(r *repo.Repo, root facts.Facts, eff *config.Effective, sc config.Scope, opts Options) (*repo.Repo, facts.Facts, config.Scope, error) {
	if sc.Path == "" {
		return r, root, sc, nil
	}
	sr, err := r.Scoped(sc.Path)
	if err != nil {
		return nil, facts.Facts{}, sc, err
	}
	nested, err := config.LoadNested(r.Root, sc.Path, now(opts))
	if err != nil {
		return nil, facts.Facts{}, sc, err
	}
	sc = mergeNested(sc, nested)
	ov := eff.ScopeFacts(sc)
	ov.DetectedVisibility = opts.Visibility
	ov.FleetTier = opts.Tier
	return sr, facts.Discover(sr, ov), sc, nil
}

func now(opts Options) time.Time {
	if opts.Now.IsZero() {
		return time.Now()
	}
	return opts.Now
}

// mergeNested folds a scope's own .fleetlint.yaml into the scope: the nested
// file wins for facts and rule configuration, and its exceptions are added.
func mergeNested(sc config.Scope, n *config.Nested) config.Scope {
	if n == nil {
		return sc
	}
	if n.Facts.Tier != 0 {
		sc.Facts.Tier = n.Facts.Tier
	}
	if len(n.Facts.Stacks) > 0 {
		sc.Stacks = n.Facts.Stacks
	}
	if n.Facts.TaskRunner != "" {
		sc.TaskRunner = n.Facts.TaskRunner
	}
	if sc.Overrides == nil {
		sc.Overrides = map[string]catalog.Override{}
	}
	for id, ov := range n.Overrides {
		sc.Overrides[id] = ov
	}
	sc.Exceptions = append(sc.Exceptions, n.Exceptions...)
	return sc
}

// resolveScopes returns the configured scopes, or the discovered workspace
// members when the config does not mention scopes at all.
func resolveScopes(root facts.Facts, eff *config.Effective) []config.Scope {
	if eff.File.Scopes != nil {
		return *eff.File.Scopes
	}
	out := make([]config.Scope, 0, len(root.Members))
	for _, m := range root.Members {
		out = append(out, config.Scope{Path: m})
	}
	return out
}

func evaluateScope(ctx context.Context, r *repo.Repo, f facts.Facts, eff *config.Effective, sc config.Scope, hostsMembers bool, opts Options) ([]model.Result, error) {
	env, err := celenv.New(ctx, r, f)
	if err != nil {
		return nil, err
	}
	rulesInScope := eff.Rules
	if sc.Path != "" && len(sc.Overrides) > 0 {
		if rulesInScope, err = eff.ScopeRules(sc.Path, sc.Overrides); err != nil {
			return nil, err
		}
	}
	exceptions := append(append([]model.Exception{}, eff.Exceptions...), sc.Exceptions...)
	caps := capabilitiesOf(rulesInScope)
	results := make([]model.Result, 0, len(rulesInScope))
	for _, rule := range rulesInScope {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !rule.AppliesInScope(sc.Path, hostsMembers) {
			continue
		}
		if rule.Kind == model.KindCapability {
			if res, ok := capabilityGap(env, rule, f, caps); ok {
				res.Scope = sc.Path
				results = append(results, res)
			}
			continue
		}
		res := evaluateRule(ctx, env, r, f, rule, opts, caps)
		res.Scope = sc.Path
		applyExceptions(&res, exceptions)
		results = append(results, res)
	}
	return results, nil
}

func evaluateRule(ctx context.Context, env *celenv.Env, r *repo.Repo, f facts.Facts, rule model.Rule, opts Options, caps capabilities) model.Result {
	res := model.Result{Rule: rule, Severity: rule.Severity, Status: model.StatusPass}
	na, why, whenErr := notApplicable(env, f, rule, caps)
	if whenErr != nil {
		res.Status, res.Err = model.StatusError, whenErr.Error()
		return res
	}
	if na {
		res.Status, res.Evidence = model.StatusNotApplicable, why
		return res
	}
	if executesCode(rule) && !opts.Deep {
		res.Status, res.Evidence = model.StatusNotApplicable, "needs --deep (executes repository code)"
		return res
	}
	var (
		findings []model.Finding
		evidence string
		err      error
	)
	switch rule.Kind {
	case model.KindExpr:
		findings, err = evalExpr(env, rule)
	case model.KindOutcome:
		var partial *model.Partial
		findings, evidence, partial, err = evalOutcome(env, rule, caps.fallback(rule))
		if partial != nil {
			res.Severity = partial.Severity // a partial match reports at its own severity
		}
	case model.KindGo:
		findings, evidence, err = evalGo(ctx, r, f, rule)
	case model.KindCommand:
		findings, err = evalCommand(ctx, r, f, rule)
	case model.KindCapability:
		err = fmt.Errorf("capability %s is checked by its implementations", rule.ID)
	default:
		err = fmt.Errorf("unknown rule kind %q", rule.Kind)
	}
	if err != nil {
		res.Status, res.Err = model.StatusError, err.Error()
		return res
	}
	res.Findings, res.Evidence = findings, evidence
	if len(findings) > 0 {
		res.Status = model.StatusFail
	}
	return res
}

// notApplicable decides whether a rule is out of scope. An error means the
// `when` expression could not be evaluated; the caller reports a rule error,
// because a rule that cannot run must never pass.
func notApplicable(env *celenv.Env, f facts.Facts, rule model.Rule, caps capabilities) (bool, string, error) {
	if !rule.AppliesToTier(f.Tier) {
		return true, fmt.Sprintf("tier %d not in %v", f.Tier, rule.Tiers), nil
	}
	if !rule.AppliesToStacks(f.Stacks) {
		return true, fmt.Sprintf("stacks %v not in %v", f.Stacks, rule.Stacks), nil
	}
	if why := implementationScope(f, rule, caps); why != "" {
		return true, why, nil
	}
	for _, c := range []struct{ label, expr, why string }{{"gate", rule.Gate, "capability when: "}, {"when", rule.When, "when: "}} {
		if c.expr == "" {
			continue
		}
		ok, err := env.Check(c.expr, rule.Params)
		if err != nil {
			return false, "", fmt.Errorf("%s: %w", c.label, err)
		}
		if !ok {
			return true, c.why + c.expr, nil
		}
	}
	return false, "", nil
}

func evalExpr(env *celenv.Env, rule model.Rule) ([]model.Finding, error) {
	msg := rule.Message
	if msg == "" {
		msg = rule.Title
	}
	if rule.Foreach == "" {
		ok, err := env.Check(rule.Expr, rule.Params)
		if err != nil || ok {
			return nil, err
		}
		return []model.Finding{{Message: msg, Evidence: "expr"}}, nil
	}
	return evalForeach(env, rule, msg)
}

// evalOutcome tries each satisfier (catalog ones first, then the repo's
// `accept` additions); the first that holds is the evidence of a pass.
// Otherwise the first matching partial, returned so its severity applies,
// or the rule's message is the finding.
func evalOutcome(env *celenv.Env, rule model.Rule, fallback *model.Rule) ([]model.Finding, string, *model.Partial, error) {
	type candidate struct {
		owner model.Rule
		s     model.Satisfier
	}
	var list []candidate
	for _, s := range append(append([]model.Satisfier{}, rule.Satisfiers...), rule.AcceptedSatisfiers()...) {
		list = append(list, candidate{rule, s})
	}
	// The capability's stack-neutral implementation counts for every stack.
	if fallback != nil {
		for _, s := range append(append([]model.Satisfier{}, fallback.Satisfiers...), fallback.AcceptedSatisfiers()...) {
			list = append(list, candidate{*fallback, s})
		}
	}
	for _, c := range list {
		ok, err := satisfied(env, c.owner, c.s)
		if err != nil {
			return nil, "", nil, fmt.Errorf("satisfier %s: %w", c.s.Name, err)
		}
		if ok {
			return nil, "satisfied by " + c.s.Name, nil, nil
		}
	}
	for i, p := range rule.Partials {
		ok, err := env.Check(p.Expr, rule.Params)
		if err != nil {
			return nil, "", nil, fmt.Errorf("partial %s: %w", p.Name, err)
		}
		if ok {
			return []model.Finding{{Message: p.Message, Evidence: p.Name}}, "", &rule.Partials[i], nil
		}
	}
	msg := rule.Message
	if msg == "" {
		msg = rule.Title
	}
	return []model.Finding{{Message: msg, Evidence: "unsatisfied"}}, "", nil, nil
}

func satisfied(env *celenv.Env, rule model.Rule, s model.Satisfier) (bool, error) {
	if s.When != "" {
		applies, err := env.Check(s.When, rule.Params)
		if err != nil || !applies {
			return false, err
		}
	}
	return env.Check(s.Expr, rule.Params)
}

// evalForeach evaluates Expr once per element of Foreach; each false element
// is a finding located at the element when it is a path-like string.
func evalForeach(env *celenv.Env, rule model.Rule, msg string) ([]model.Finding, error) {
	listPrg, err := env.CompileList(rule.Foreach)
	if err != nil {
		return nil, err
	}
	items, err := env.EvalList(listPrg, rule.Params)
	if err != nil {
		return nil, err
	}
	prg, err := env.Compile(rule.Expr)
	if err != nil {
		return nil, err
	}
	var findings []model.Finding
	for _, item := range items {
		ok, err := env.EvalBoolWith(prg, rule.Params, item)
		if err != nil {
			return nil, fmt.Errorf("item %v: %w", item, err)
		}
		if !ok {
			findings = append(findings, foreachFinding(env, msg, item))
		}
	}
	return findings, nil
}

// foreachFinding locates a finding from the failing item: a string is a
// path; a map uses its path/line keys and a short label from name, subject
// or hash, so a job or commit reads as itself instead of a dumped map.
func foreachFinding(env *celenv.Env, msg string, item any) model.Finding {
	f := model.Finding{Message: msg, Evidence: "foreach"}
	switch v := item.(type) {
	case string:
		if env.HasFile(v) {
			f.Path = v
		} else {
			f.Message = msg + " (" + v + ")"
		}
	case map[string]any:
		if p, ok := v["path"].(string); ok {
			f.Path = p
		}
		if n, ok := v["line"].(int64); ok {
			f.Line = int(n)
		}
		if label := itemLabel(v); label != "" {
			f.Message = msg + " (" + label + ")"
		}
	default:
		f.Message = fmt.Sprintf("%s (%v)", msg, item)
	}
	return f
}

func itemLabel(m map[string]any) string {
	for _, key := range []string{"name", "subject", "text"} {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	if h, ok := m["hash"].(string); ok && len(h) >= 12 {
		return h[:12]
	}
	return ""
}

// executesCode reports whether a rule runs repository code: command
// rules, and Go rules that say so. They run only under --deep.
func executesCode(rule model.Rule) bool {
	if rule.Kind == model.KindCommand {
		return true
	}
	if rule.Kind == model.KindGo {
		c, ok := rules.Lookup(rule.ID)
		return ok && c.Deep()
	}
	return false
}

func evalGo(ctx context.Context, r *repo.Repo, f facts.Facts, rule model.Rule) ([]model.Finding, string, error) {
	checker, ok := rules.Lookup(rule.ID)
	if !ok {
		return nil, "", fmt.Errorf("no Go implementation registered for %s (binary too old for this catalog?)", rule.ID)
	}
	out, err := checker.Check(ctx, rules.Context{Repo: r, Facts: f, Params: rule.Params})
	if err != nil {
		return nil, "", err
	}
	return out.Findings, out.Evidence, nil
}

// applyExceptions marks findings covered by a non-expired exception and
// downgrades the result to excepted when all of them are. Path patterns are
// repository-relative, so a scope's findings are matched with the scope prefix.
func applyExceptions(res *model.Result, exceptions []model.Exception) {
	if res.Status != model.StatusFail {
		return
	}
	covered := 0
	for i := range res.Findings {
		candidate := res.Findings[i]
		if res.Scope != "" && candidate.Path != "" {
			candidate.Path = res.Scope + "/" + candidate.Path
		}
		for j := range exceptions {
			ex := &exceptions[j]
			if !res.Rule.Covers(ex.Rule) || ex.Expired || !matches(ex.Match, candidate) {
				continue
			}
			res.Findings[i].Exception = ex
			covered++
			break
		}
	}
	if covered == len(res.Findings) {
		res.Status = model.StatusExcepted
	}
}

// matches applies an exception's match pattern to a finding: empty matches
// everything; otherwise a glob against the path, or a regex (prefixed `re:`)
// against the message.
func matches(pattern string, f model.Finding) bool {
	if pattern == "" {
		return true
	}
	if re, ok := strings.CutPrefix(pattern, "re:"); ok {
		rx, err := regexp.Compile(re)
		return err == nil && rx.MatchString(f.Message)
	}
	if f.Path == "" {
		return false
	}
	return repo.MatchGlob(pattern, f.Path)
}

// implementationScope says why an implementation does not run here: the
// stack-neutral one when every stack has its own, a stack's exemption
// (`when: "false"`) with a reason a person can read. "" means it runs.
func implementationScope(f facts.Facts, rule model.Rule, caps capabilities) string {
	switch {
	case rule.Stack == stacks.Any && len(f.Stacks) == 0:
		return "no stack detected here"
	case rule.Stack == stacks.Any && len(caps.uncovered(rule.Capability, f.Stacks)) == 0:
		return "every stack here has its own implementation"
	case rule.Stack != "" && rule.When == "false":
		return "the capability does not apply to " + rule.Stack + " (see the requirement)"
	}
	return ""
}

// capabilityGap is a capability's own result: shown only where a present
// stack has no implementation, so the gap in the catalog is visible.
func capabilityGap(env *celenv.Env, rule model.Rule, f facts.Facts, caps capabilities) (model.Result, bool) {
	gaps := caps.gaps(rule.ID, f.Stacks)
	if len(gaps) == 0 || !rule.AppliesToTier(f.Tier) {
		return model.Result{}, false
	}
	if rule.When != "" {
		if ok, err := env.Check(rule.When, rule.Params); err != nil || !ok {
			return model.Result{}, false // where the capability does not apply, no gap either
		}
	}
	return model.Result{
		Rule: rule, Severity: rule.Severity, Status: model.StatusNotApplicable,
		Evidence: fmt.Sprintf("no implementation for %s (add rules/%s/<stack>.yaml)", strings.Join(gaps, ", "), rule.ID),
	}, true
}

// capabilities indexes the implementations among a scope's rules: which
// stacks each capability covers, and its stack-neutral implementation.
type capabilities struct {
	stacks  map[string]map[string]bool
	neutral map[string]model.Rule
}

func capabilitiesOf(rules []model.Rule) capabilities {
	c := capabilities{stacks: map[string]map[string]bool{}, neutral: map[string]model.Rule{}}
	for _, r := range rules {
		switch {
		case r.Capability == "":
		case r.Stack == stacks.Any:
			c.neutral[r.Capability] = r
		default:
			if c.stacks[r.Capability] == nil {
				c.stacks[r.Capability] = map[string]bool{}
			}
			c.stacks[r.Capability][r.Stack] = true
		}
	}
	return c
}

// uncovered are the present stacks without their own implementation.
func (c capabilities) uncovered(capability string, present []string) []string {
	var out []string
	for _, s := range present {
		if !c.stacks[capability][s] {
			out = append(out, s)
		}
	}
	return out
}

// gaps are the present stacks nothing checks the capability for.
func (c capabilities) gaps(capability string, present []string) []string {
	if _, ok := c.neutral[capability]; ok {
		return nil
	}
	return c.uncovered(capability, present)
}

// fallback returns the stack-neutral implementation whose satisfiers also
// count for a stack's own outcome implementation.
func (c capabilities) fallback(r model.Rule) *model.Rule {
	if r.Capability == "" || r.Stack == stacks.Any {
		return nil
	}
	if fb, ok := c.neutral[r.Capability]; ok && fb.Kind == model.KindOutcome {
		return &fb
	}
	return nil
}
