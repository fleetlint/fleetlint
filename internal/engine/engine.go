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

	"github.com/fleetlint/fleetlint/internal/artifacts"
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
	if err := detectArtifacts(ctx, r, &root, eff, ov.Artifacts); err != nil {
		return nil, err
	}
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

// detectArtifacts fills the root's published artifacts from the catalog's
// definitions, or from the pinned list. Detection needs the rule environment
// (workflows, steps), so it runs after the other facts.
func detectArtifacts(ctx context.Context, r *repo.Repo, root *facts.Facts, eff *config.Effective, pinned *[]facts.Artifact) error {
	if len(eff.Artifacts) == 0 && pinned == nil {
		return nil
	}
	env, err := celenv.New(ctx, r, *root)
	if err != nil {
		return err
	}
	found, err := artifacts.Detect(env, eff.Artifacts, pinned)
	if err != nil {
		return err
	}
	root.Artifacts = found
	root.Sources["artifacts"] = facts.SourceDetected
	if pinned != nil {
		root.Sources["artifacts"] = facts.SourceConfigured
	}
	return nil
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
	f := facts.Discover(sr, ov)
	f.Artifacts = root.Artifacts // what the repository publishes is decided at the root
	return sr, f, sc, nil
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
	combineAny(results)
	return results, nil
}

// combineAny settles the capabilities whose implementations combine with
// `any`: once one implementation passes in the scope, the others that
// failed pass too, naming the one that carried it. Errors stay errors.
func combineAny(results []model.Result) {
	carried := map[string]string{}
	for _, res := range results {
		if res.Rule.Combine == model.CombineAny && res.Status == model.StatusPass {
			if _, ok := carried[res.Rule.Capability]; !ok {
				carried[res.Rule.Capability] = res.Rule.ID
			}
		}
	}
	for i := range results {
		res := &results[i]
		by, ok := carried[res.Rule.Capability]
		if !ok || res.Rule.Combine != model.CombineAny || res.Status != model.StatusFail {
			continue
		}
		res.Status, res.Severity, res.Findings = model.StatusPass, res.Rule.Severity, nil
		res.Evidence = "satisfied by " + by + " (combine: any)"
	}
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
	findings, evidence, severity, skipped, err := evalPerArtifact(ctx, env, r, f, rule, caps)
	switch {
	case err != nil:
		res.Status, res.Err = model.StatusError, err.Error()
		return res
	case skipped != "":
		res.Status, res.Evidence = model.StatusNotApplicable, skipped
		return res
	}
	res.Findings, res.Evidence, res.Severity = findings, evidence, severity
	if len(findings) > 0 {
		res.Status = model.StatusFail
	}
	return res
}

// perArtifact says whether the rule runs once per published artifact: an
// implementation keyed by artifact, with artifacts of its kind published.
func perArtifact(f facts.Facts, rule model.Rule) bool {
	return rule.Artifact != "" && len(f.ArtifactInstances(rule.Artifact)) > 0
}

// evalPerArtifact runs an implementation keyed by artifact once per
// published artifact of its kind, with `artifact` bound, so each is checked
// on its own: the implementation's `when` is asked for each, a finding
// names the artifact that fails, the evidence the ones that pass. Any
// other rule runs once. The severity is the rule's, or a partial's when
// every failing artifact matched a partial. skipped is set when the rule
// applied to no published artifact.
func evalPerArtifact(ctx context.Context, env *celenv.Env, r *repo.Repo, f facts.Facts, rule model.Rule, caps capabilities) (findings []model.Finding, evidence string, severity model.Severity, skipped string, err error) {
	if !perArtifact(f, rule) {
		found, evidence, partial, err := evalKind(ctx, env, r, f, rule, caps)
		return found, evidence, partialSeverity(rule, found, partial), "", err
	}
	defer env.BindArtifact(facts.Artifact{})
	var (
		evidences []string
		worst     *model.Partial // the strongest partial, nil once an artifact failed outright
		applied   int
	)
	for _, a := range f.ArtifactInstances(rule.Artifact) {
		found, evidence, partial, applies, err := evalInstance(ctx, env, r, f, rule, caps, a)
		switch {
		case err != nil:
			return nil, "", rule.Severity, "", err
		case !applies:
			continue
		case len(found) == 0:
			applied++
			if evidence != "" {
				evidences = append(evidences, evidence)
			}
			continue
		}
		applied++
		findings = append(findings, found...)
		worst = strongerPartial(worst, partial, len(findings) == len(found))
	}
	if applied == 0 {
		return nil, "", rule.Severity, "when: " + rule.When, nil
	}
	return findings, strings.Join(evidences, "; "), partialSeverity(rule, findings, worst), "", nil
}

// partialSeverity is the severity a result reports at: a partial's when the
// findings come from partial matches only, else the rule's.
func partialSeverity(rule model.Rule, findings []model.Finding, partial *model.Partial) model.Severity {
	if partial != nil && len(findings) > 0 {
		return partial.Severity
	}
	return rule.Severity
}

// strongerPartial folds one artifact's partial into the result's: nil once
// any artifact failed outright (its own or an earlier one), else the one
// with the higher severity. first says whether this is the first failure.
func strongerPartial(sofar, next *model.Partial, first bool) *model.Partial {
	switch {
	case next == nil, !first && sofar == nil:
		return nil
	case first || next.Severity > sofar.Severity:
		return next
	}
	return sofar
}

// evalInstance binds one published artifact, asks the rule's own `when`
// for it and evaluates the rule when it applies; findings, evidence and an
// error carry the artifact's name.
func evalInstance(ctx context.Context, env *celenv.Env, r *repo.Repo, f facts.Facts, rule model.Rule, caps capabilities, a facts.Artifact) (findings []model.Finding, evidence string, partial *model.Partial, applies bool, err error) {
	env.BindArtifact(a)
	if rule.When != "" {
		ok, err := env.Check(rule.When, rule.Params)
		if err != nil {
			return nil, "", nil, false, fmt.Errorf("when: %w", err)
		}
		if !ok {
			return nil, "", nil, false, nil
		}
	}
	findings, evidence, partial, err = evalKind(ctx, env, r, f, rule, caps)
	if err != nil {
		if a.Name != "" {
			err = fmt.Errorf("%s: %w", a, err)
		}
		return nil, "", nil, true, err
	}
	for i := range findings {
		findings[i].Message = label(a, findings[i].Message)
	}
	if evidence != "" {
		evidence = label(a, evidence)
	}
	return findings, evidence, partial, true, nil
}

// label prefixes a message with the artifact's name, when it has one.
func label(a facts.Artifact, msg string) string {
	if a.Name == "" {
		return msg
	}
	return a.Name + ": " + msg
}

// evalKind evaluates the rule by its kind; a partial is returned when a
// partial of an outcome rule matched.
func evalKind(ctx context.Context, env *celenv.Env, r *repo.Repo, f facts.Facts, rule model.Rule, caps capabilities) ([]model.Finding, string, *model.Partial, error) {
	switch rule.Kind {
	case model.KindExpr:
		findings, err := evalExpr(env, rule)
		return findings, "", nil, err
	case model.KindOutcome:
		return evalOutcome(env, rule, caps.fallback(rule))
	case model.KindGo:
		findings, evidence, err := evalGo(ctx, r, f, rule)
		return findings, evidence, nil, err
	case model.KindCommand:
		findings, err := evalCommand(ctx, r, f, rule)
		return findings, "", nil, err
	case model.KindCapability:
		return nil, "", nil, fmt.Errorf("capability %s is checked by its implementations", rule.ID)
	default:
		return nil, "", nil, fmt.Errorf("unknown rule kind %q", rule.Kind)
	}
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
	if !rule.AppliesToArtifacts(f.ArtifactKinds()) {
		return true, fmt.Sprintf("no %s is published (artifacts: %s)", rule.Artifact, orNone(f.ArtifactKinds())), nil
	}
	if why := implementationScope(f, rule, caps); why != "" {
		return true, why, nil
	}
	gates := []struct{ label, expr, why string }{{"gate", rule.Gate, "capability when: "}, {"when", rule.When, "when: "}}
	if perArtifact(f, rule) {
		gates = gates[:1] // the implementation's own `when` is asked per published artifact
	}
	for _, c := range gates {
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
	// Without a stack the stack-neutral implementation runs: every repository
	// is checked by something. Where that means nothing (no dependencies,
	// no code), the implementation's own `when` says so.
	switch {
	case rule.Stack == stacks.Any && len(f.Stacks) > 0 && len(caps.uncovered(rule.Capability, f.Stacks)) == 0:
		return "every stack here has its own implementation"
	case rule.Stack != "" && rule.When == "false":
		return "the capability does not apply to " + rule.Stack + " (see the requirement)"
	case rule.Artifact != "" && rule.When == "false":
		return "the capability does not apply to the artifact kind " + rule.Artifact + " (see the requirement)"
	}
	return ""
}

// capabilityGap is a capability's own result: shown where a present stack
// (or published artifact kind) has no implementation, so the gap in the
// catalog is visible, and for an artifact-keyed capability where nothing
// is published, so a release the detection missed is not a silent pass.
func capabilityGap(env *celenv.Env, rule model.Rule, f facts.Facts, caps capabilities) (model.Result, bool) {
	present := f.Stacks
	if rule.By == model.ByArtifact {
		present = f.ArtifactKinds()
	}
	gaps := caps.gaps(rule.ID, present)
	nothingPublished := rule.By == model.ByArtifact && len(present) == 0
	if (len(gaps) == 0 && !nothingPublished) || !rule.AppliesToTier(f.Tier) {
		return model.Result{}, false
	}
	if rule.When != "" {
		if ok, err := env.Check(rule.When, rule.Params); err != nil || !ok {
			return model.Result{}, false // where the capability does not apply, no gap either
		}
	}
	res := model.Result{Rule: rule, Severity: rule.Severity, Status: model.StatusNotApplicable}
	switch {
	case nothingPublished:
		res.Evidence = "no published artifact detected: nothing to check (pin facts.artifacts if the release publishes something detection misses)"
	case rule.By == model.ByArtifact:
		res.Evidence = fmt.Sprintf("no implementation for %s (add rules/%s/<artifact>.yaml)", strings.Join(gaps, ", "), rule.ID)
	default:
		res.Evidence = fmt.Sprintf("no implementation for %s (add rules/%s/<stack>.yaml)", strings.Join(gaps, ", "), rule.ID)
	}
	return res, true
}

// capabilities indexes the implementations among a scope's rules: which
// stacks (or artifact kinds) each capability covers, and its stack-neutral
// implementation.
type capabilities struct {
	targets map[string]map[string]bool
	neutral map[string]model.Rule
}

func capabilitiesOf(rules []model.Rule) capabilities {
	c := capabilities{targets: map[string]map[string]bool{}, neutral: map[string]model.Rule{}}
	for _, r := range rules {
		switch {
		case r.Capability == "":
		case r.Stack == stacks.Any:
			c.neutral[r.Capability] = r
		default:
			if c.targets[r.Capability] == nil {
				c.targets[r.Capability] = map[string]bool{}
			}
			c.targets[r.Capability][r.Target()] = true
		}
	}
	return c
}

// uncovered are the present stacks (or artifact kinds) without their own
// implementation.
func (c capabilities) uncovered(capability string, present []string) []string {
	var out []string
	for _, s := range present {
		if !c.targets[capability][s] {
			out = append(out, s)
		}
	}
	return out
}

// gaps are the present stacks or artifact kinds nothing checks the
// capability for.
func (c capabilities) gaps(capability string, present []string) []string {
	if _, ok := c.neutral[capability]; ok {
		return nil
	}
	return c.uncovered(capability, present)
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
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
