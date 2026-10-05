// Package engine evaluates the effective rule set against a repository and
// its scopes, applies exceptions, and returns results. It knows nothing
// about output formats or the command line.
package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/baseline"
	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/rules"
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
	Path  string
	Facts facts.Facts
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
}

// Evaluate runs every applicable rule for the root and each configured scope.
func Evaluate(ctx context.Context, r *repo.Repo, eff *config.Effective) (*Run, error) {
	return EvaluateWith(ctx, r, eff, Options{})
}

// EvaluateWith is Evaluate with options.
func EvaluateWith(ctx context.Context, r *repo.Repo, eff *config.Effective, opts Options) (*Run, error) {
	ov := eff.File.Facts.ToFacts()
	ov.DetectedVisibility = opts.Visibility
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
	abs, err := r.Abs(path)
	if err != nil {
		return err
	}
	b, exists, err := baseline.Load(abs)
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
	nested, err := config.LoadNested(sr.ScopeRoot(), now(opts))
	if err != nil {
		return nil, facts.Facts{}, sc, err
	}
	sc = mergeNested(sc, nested)
	ov := eff.File.Facts.ToFacts()
	ov.DetectedVisibility = opts.Visibility
	if len(sc.Facts.Stacks) > 0 {
		ov.Stacks = sc.Facts.Stacks
	}
	if sc.Facts.Tier != 0 {
		ov.Tier = sc.Facts.Tier
	}
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
		sc.Facts.Stacks = n.Facts.Stacks
	}
	if sc.Rules == nil {
		sc.Rules = map[string]config.RuleConf{}
	}
	for id, rc := range n.Rules {
		sc.Rules[id] = rc
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
	env, err := celenv.New(r, f)
	if err != nil {
		return nil, err
	}
	rulesInScope := eff.Rules
	if sc.Path != "" && len(sc.Rules) > 0 {
		if rulesInScope, err = eff.ScopeRules(sc.Path, sc.Rules); err != nil {
			return nil, err
		}
	}
	exceptions := append(append([]model.Exception{}, eff.Exceptions...), sc.Exceptions...)
	results := make([]model.Result, 0, len(rulesInScope))
	for _, rule := range rulesInScope {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !rule.AppliesInScope(sc.Path, hostsMembers) {
			continue
		}
		res := evaluateRule(ctx, env, r, f, rule, opts)
		res.Scope = sc.Path
		applyExceptions(&res, exceptions)
		results = append(results, res)
	}
	return results, nil
}

func evaluateRule(ctx context.Context, env *celenv.Env, r *repo.Repo, f facts.Facts, rule model.Rule, opts Options) model.Result {
	res := model.Result{Rule: rule, Severity: rule.Severity, Status: model.StatusPass}
	na, why, whenErr := notApplicable(env, f, rule)
	if whenErr != nil {
		res.Status, res.Err = model.StatusError, whenErr.Error()
		return res
	}
	if na {
		res.Status, res.Evidence = model.StatusNotApplicable, why
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
		findings, evidence, err = evalOutcome(env, rule)
		if len(findings) == 1 && findings[0].Exception == nil && findings[0].Evidence != "" && findings[0].Evidence != "unsatisfied" {
			// a partial match reports at its own severity
			if sev, ok := partialSeverity(rule, findings[0].Evidence); ok {
				res.Severity = sev
			}
		}
	case model.KindGo:
		findings, evidence, err = evalGo(ctx, r, f, rule, opts)
		if errors.Is(err, rules.ErrNeedsDeep) {
			res.Status, res.Evidence = model.StatusNotApplicable, "needs --deep"
			return res
		}
	case model.KindCommand:
		if !opts.Deep {
			res.Status, res.Evidence = model.StatusNotApplicable, "needs --deep (executes repository code)"
			return res
		}
		findings, err = evalCommand(ctx, r, f, rule)
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
func notApplicable(env *celenv.Env, f facts.Facts, rule model.Rule) (bool, string, error) {
	if !rule.AppliesToTier(f.Tier) {
		return true, fmt.Sprintf("tier %d not in %v", f.Tier, rule.Tiers), nil
	}
	if !rule.AppliesToStacks(f.Stacks) {
		return true, fmt.Sprintf("stacks %v not in %v", f.Stacks, rule.Stacks), nil
	}
	if rule.When != "" {
		ok, err := env.Check(rule.When, rule.Params)
		if err != nil {
			return false, "", fmt.Errorf("when: %w", err)
		}
		if !ok {
			return true, "when: " + rule.When, nil
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
// Otherwise the first matching partial, or the rule's message, is the finding.
func evalOutcome(env *celenv.Env, rule model.Rule) ([]model.Finding, string, error) {
	for _, s := range append(rule.Satisfiers, acceptedSatisfiers(rule)...) {
		ok, err := satisfied(env, rule, s)
		if err != nil {
			return nil, "", fmt.Errorf("satisfier %s: %w", s.Name, err)
		}
		if ok {
			return nil, "satisfied by " + s.Name, nil
		}
	}
	for _, p := range rule.Partials {
		ok, err := env.Check(p.Expr, rule.Params)
		if err != nil {
			return nil, "", fmt.Errorf("partial %s: %w", p.Name, err)
		}
		if ok {
			return []model.Finding{{Message: p.Message, Evidence: p.Name}}, "", nil
		}
	}
	msg := rule.Message
	if msg == "" {
		msg = rule.Title
	}
	return []model.Finding{{Message: msg, Evidence: "unsatisfied"}}, "", nil
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

// acceptedSatisfiers reads `accept:` entries a repository added to the rule.
func acceptedSatisfiers(rule model.Rule) []model.Satisfier {
	raw, _ := rule.Params["accept"].([]any)
	out := make([]model.Satisfier, 0, len(raw))
	for i, item := range raw {
		m, _ := item.(map[string]any)
		s := model.Satisfier{Name: fmt.Sprintf("accept[%d]", i)}
		if n, ok := m["name"].(string); ok && n != "" {
			s.Name = n
		}
		s.Expr, _ = m["expr"].(string)
		s.When, _ = m["when"].(string)
		if s.Expr != "" {
			out = append(out, s)
		}
	}
	return out
}

func partialSeverity(rule model.Rule, name string) (model.Severity, bool) {
	for _, p := range rule.Partials {
		if p.Name == name {
			return p.Severity, true
		}
	}
	return 0, false
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

func evalGo(ctx context.Context, r *repo.Repo, f facts.Facts, rule model.Rule, opts Options) ([]model.Finding, string, error) {
	checker, ok := rules.Lookup(rule.ID)
	if !ok {
		return nil, "", fmt.Errorf("no Go implementation registered for %s (binary too old for this catalog?)", rule.ID)
	}
	if dc, isDeep := checker.(rules.DeepChecker); isDeep {
		return dc.CheckDeep(ctx, rules.Context{Repo: r, Facts: f, Params: rule.Params, Deep: opts.Deep})
	}
	out, err := checker.Check(rules.Context{Repo: r, Facts: f, Params: rule.Params, Deep: opts.Deep})
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
			if ex.Rule != res.Rule.ID || ex.Expired || !matches(ex.Match, candidate) {
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
