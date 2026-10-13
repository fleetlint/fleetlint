// Package celenv builds the CEL environment rules are evaluated in. It is the
// contract between catalogs and the engine: the variables and functions
// declared here are the whole surface an `expr` rule can use, and they are
// documented from this file.
package celenv

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/common/types/traits"
	"cel.dev/cel-go/ext"

	"github.com/fleetlint/fleetlint/internal/ci"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// Accessor documents one function available to expressions.
type Accessor struct {
	Signature, Doc string
}

// Accessors lists the functions exposed to CEL, for generated documentation.
var Accessors = []Accessor{
	{"exists(glob: string) -> bool", "True if any file in the scope matches the glob (`**` supported)."},
	{"file(path: string) -> bool", "True if the exact path exists (file or directory). (`has` is reserved by CEL.)"},
	{"glob(pattern: string) -> list<string>", "Scope-relative paths matching the glob, sorted."},
	{"tracked() -> list<string>", "Git-tracked paths in the scope."},
	{"text(path: string) -> string", "File contents, or \"\" when absent."},
	{"lines(path: string) -> list<string>", "File contents split into lines."},
	{"yaml(path: string) -> dyn", "Parsed YAML document; null when absent."},
	{"toml(path: string) -> dyn", "Parsed TOML document; null when absent."},
	{"json(path: string) -> dyn", "Parsed JSON document; null when absent. Comments and trailing commas are accepted (JWCC), as in tsconfig.json."},
	{"makefile(path: string) -> map", "`{targets: list<string>, deps: map<string, list<string>>, recipes: map<string, list<string>>}`; null when absent."},
	{"scan(path: string, regex: string) -> list<int>", "1-based line numbers where the regex matches."},
	{"matchesAny(list: list<string>, regex: string) -> bool", "True if any element matches the regex."},
	{"workflows() -> list<string>", "Paths of the repository's CI files: Actions workflows (`.github/workflows`, `.gitea/workflows`, `.forgejo/workflows`), `.gitlab-ci.yml` and the local files it includes. CI lives at the repository root, so in a member scope these are the root's files as `../` paths."},
	{"ci_kind(path: string) -> string", "The CI system of a file from `workflows()`: `github`, `gitea`, `forgejo` or `gitlab`."},
	{"tagged_workflows() -> list<string>", "CI files a tag push starts (`on.push.tags`; GitLab rules on `$CI_COMMIT_TAG` or `only: tags`), i.e. release pipelines."},
	{"steps(path: string) -> list<map>", "Every step of every job in a CI file, each as `{job, name, uses, run, with}` with `\"\"`/`{}` for absent fields. A GitLab job is one step whose `run` is its before_script, script and after_script."},
	{"jobs(path: string) -> list<map>", "Every job of a CI file as `{name, timeout_minutes, permissions, runs_on, uses, image, interruptible}`; `timeout_minutes` is 0 when unset (GitLab `timeout:` durations are converted); `interruptible` is GitLab's."},
	{"triggers(path: string) -> list<string>", "Event names a CI file reacts to (`push`, `pull_request`, `pull_request_target`, ...), whatever the `on:` shape; a GitLab pipeline reads as `push`, plus `pull_request` for merge request pipelines and `schedule`."},
	{"includes(path: string) -> list<map>", "What a GitLab CI file includes, as `{type, target, ref}` (type `local`, `project`, `remote`, `template` or `component`; ref is the project ref or component version); empty for other files."},
	{"tags() -> list<string>", "Git tags, newest version first."},
	{"tag_signed(name: string) -> bool", "True if the tag is an annotated tag carrying a PGP, SSH or X.509 signature; false for lightweight or unsigned tags. The signature is not verified."},
	{"commits(n: int) -> list<map>", "Newest n commits as `{hash, author, email, subject, body, trailers, lines, root}`; `lines` is insertions plus deletions, `root` is true for a commit without parents; empty without git."},
	{"grep(glob: string, regex: string) -> list<map>", "`{path, line, text, match}` for every line matching the regex (case-insensitive) in files matching the glob; `match` is the first capture group, or the whole match, unshortened; skips fenced code blocks; pairs with `foreach` to flag every match."},
	{"codegrep(kind: string, regex: string) -> list<map>", "`{path, line, text, match}` for every matching line of tracked source code; kind is `code`, `test` or `nontest`. Case-sensitive unless the pattern starts with `(?i)`; vendored, generated and build paths are skipped."},
	{"filesize(path: string) -> int", "Size in bytes of a tracked or untracked file; 0 when absent."},
	{"globmatch(pattern: string, path: string) -> bool", "True if the path matches the glob (`**` supported)."},
}

// Variables documents the variables exposed to CEL.
var Variables = []Accessor{
	{"repo", "Facts: `name`, `stacks`, `forge`, `visibility`, `public`, `container` (`none`, `devcontainer`, `docker`, `podman`), `tier`, `layout`, `ci`, `hooks`, `has_git`, `scope`."},
	{"release", "`{exists, mechanisms, tag_patterns, latest_tag}`."},
	{"taskrunner", "`{kind, targets, file, cmd, deps, recipes}`: the task runner whatever its kind (`make`, `just`, `task`, `npm-scripts`, `gradle`, `none`). `cmd` is how a target is invoked (`make`, `just`, `task`, `npm run`, `./gradlew`); `deps` and `recipes` map each target to what it depends on and the commands it runs."},
	{"params", "The rule's `params` map from the catalog or the repo override."},
	{"item", "In a rule with `foreach`, the current element; `\"\"` otherwise."},
}

// Env wraps a compiled CEL environment bound to one repository scope. It is
// used by one goroutine at a time.
type Env struct {
	ctx   context.Context
	env   *cel.Env
	repo  *repo.Repo
	facts facts.Facts
	cache map[string]cel.Program
	// rootCI holds the repository's CI files as seen from a member scope.
	rootCI      sync.Once
	rootCIFiles []string
}

// costLimit bounds one evaluation (about half a minute at worst), so a
// catalog's nested comprehension over every tracked file cannot run for
// hours; interruptEvery is how often a comprehension checks for
// cancellation.
var costLimit uint64 = 10_000_000

const interruptEvery = 100

// checked holds type-checked ASTs by result type and expression, shared by
// every Env: an expression is parsed and checked once per process, not once
// per scope and repository. Every Env declares the same functions.
var checked sync.Map

// checker is an Env without a repository, for type-checking only.
var checker = sync.OnceValues(func() (*Env, error) { return New(context.Background(), nil, facts.Facts{}) })

// New builds the environment for a repository scope; evaluations stop when
// ctx is done.
func New(ctx context.Context, r *repo.Repo, f facts.Facts) (*Env, error) {
	e := &Env{ctx: ctx, repo: r, facts: f, cache: map[string]cel.Program{}}
	vars := []cel.EnvOption{
		cel.Variable("repo", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("release", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("taskrunner", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("params", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("item", cel.DynType),
	}
	fns, ci, code := e.functions(), e.ciAccessors(), e.codeAccessors() //nolint:contextcheck // git lookups run under the repository's own bounded context
	opts := make([]cel.EnvOption, 0, len(vars)+len(fns)+len(ci)+len(code)+2)
	opts = append(opts, vars...)
	opts = append(opts, fns...)
	opts = append(opts, ci...)
	opts = append(opts, code...)
	opts = append(opts, ext.Strings(), ext.Lists())
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("build CEL environment: %w", err)
	}
	e.env = env
	return e, nil
}

// Compile type-checks a predicate and reports errors with CEL's positions.
// The result must be a bool.
func (e *Env) Compile(expr string) (cel.Program, error) {
	return e.compile(expr, cel.BoolType)
}

// CompileList type-checks an expression that must yield a list.
func (e *Env) CompileList(expr string) (cel.Program, error) {
	return e.compile(expr, cel.ListType(cel.DynType))
}

func (e *Env) compile(expr string, want *cel.Type) (cel.Program, error) {
	key := want.String() + "\x00" + expr
	if p, ok := e.cache[key]; ok {
		return p, nil
	}
	ast, err := e.check(key, expr, want)
	if err != nil {
		return nil, err
	}
	prg, err := e.env.Program(ast, cel.EvalOptions(cel.OptOptimize), cel.CostLimit(costLimit), cel.InterruptCheckFrequency(interruptEvery))
	if err != nil {
		return nil, fmt.Errorf("program %q: %w", expr, err)
	}
	e.cache[key] = prg
	return prg, nil
}

func (e *Env) check(key, expr string, want *cel.Type) (*cel.Ast, error) {
	if ast, ok := checked.Load(key); ok {
		return ast.(*cel.Ast), nil
	}
	ast, iss := e.env.Compile(expr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("compile %q: %w", expr, iss.Err())
	}
	if got := ast.OutputType(); !acceptable(got, want) {
		return nil, fmt.Errorf("compile %q: result must be %s, got %s", expr, want, got)
	}
	checked.Store(key, ast)
	return ast, nil
}

// ValidateRules type-checks every expression of the rules: when, expr,
// foreach, satisfiers, partials and accepted satisfiers. Config loading
// calls it, so a typo fails every repository, not only those where the
// rule happens to apply; the checked expressions are kept for evaluation.
func ValidateRules(rules []model.Rule) error {
	env, err := checker()
	if err != nil {
		return err
	}
	for _, r := range rules {
		for _, x := range expressions(r) {
			want := cel.BoolType
			if x.list {
				want = cel.ListType(cel.DynType)
			}
			if _, err := env.check(want.String()+"\x00"+x.expr, x.expr, want); err != nil {
				return fmt.Errorf("rule %s: %s: %w", r.ID, x.where, err)
			}
		}
	}
	return nil
}

type expression struct {
	where, expr string
	list        bool
}

func expressions(r model.Rule) []expression {
	var out []expression
	add := func(where, expr string, list bool) {
		if expr != "" {
			out = append(out, expression{where, expr, list})
		}
	}
	add("gate", r.Gate, false)
	add("when", r.When, false)
	add("foreach", r.Foreach, true)
	if r.Kind == model.KindExpr || r.Kind == "" {
		add("expr", r.Expr, false)
	}
	for _, s := range append(append([]model.Satisfier{}, r.Satisfiers...), r.AcceptedSatisfiers()...) {
		add("satisfier "+s.Name+" when", s.When, false)
		add("satisfier "+s.Name, s.Expr, false)
	}
	for _, p := range r.Partials {
		add("partial "+p.Name, p.Expr, false)
	}
	return out
}

// acceptable reports whether a compiled expression's type satisfies the
// wanted kind: exact match, dyn, or any list when a list is wanted.
func acceptable(got, want *cel.Type) bool {
	if got == cel.DynType || got.IsAssignableType(want) {
		return true
	}
	return want.Kind() == cel.ListKind && got.Kind() == cel.ListKind
}

// EvalList evaluates a list expression.
func (e *Env) EvalList(prg cel.Program, params map[string]any) ([]any, error) {
	out, _, err := prg.ContextEval(e.ctx, e.activation(params, nil))
	if err != nil {
		return nil, fmt.Errorf("evaluate: %w", err)
	}
	v, err := out.ConvertToNative(reflect.TypeOf([]any{}))
	if err != nil {
		return nil, fmt.Errorf("evaluate: result is not a list: %w", err)
	}
	list, _ := native(v).([]any)
	return list, nil
}

var mapType = reflect.TypeOf(map[string]any{})

// native turns what CEL built (a map literal, the result of map() or
// flatten()) into plain Go maps and lists, so a foreach item locates its
// finding whether an accessor or the expression made it.
func native(v any) any {
	if val, ok := v.(ref.Val); ok {
		return nativeVal(val)
	}
	if l, ok := v.([]any); ok {
		out := make([]any, len(l))
		for i, x := range l {
			out[i] = native(x)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map {
		return v
	}
	m := make(map[string]any, rv.Len())
	for it := rv.MapRange(); it.Next(); {
		m[fmt.Sprint(native(it.Key().Interface()))] = native(it.Value().Interface())
	}
	return m
}

func nativeVal(v ref.Val) any {
	switch v.Type() {
	case types.MapType:
		if m, err := v.ConvertToNative(mapType); err == nil {
			return native(m)
		}
	case types.ListType:
		if l, err := v.ConvertToNative(reflect.TypeOf([]any{})); err == nil {
			return native(l)
		}
	}
	return v.Value()
}

// EvalBoolWith evaluates a predicate with `item` bound.
func (e *Env) EvalBoolWith(prg cel.Program, params map[string]any, item any) (bool, error) {
	out, _, err := prg.ContextEval(e.ctx, e.activation(params, item))
	if err != nil {
		return false, fmt.Errorf("evaluate: %w", err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("evaluate: result is %T, want bool", out.Value())
	}
	return b, nil
}

// EvalBool evaluates a compiled predicate with the given params.
func (e *Env) EvalBool(prg cel.Program, params map[string]any) (bool, error) {
	return e.EvalBoolWith(prg, params, nil)
}

// Check compiles and evaluates in one step; used by rules that run once.
func (e *Env) Check(expr string, params map[string]any) (bool, error) {
	prg, err := e.Compile(expr)
	if err != nil {
		return false, err
	}
	return e.EvalBool(prg, params)
}

func (e *Env) activation(params map[string]any, item any) map[string]any {
	if params == nil {
		params = map[string]any{}
	} else {
		params, _ = repo.Normalize(copyMap(params)).(map[string]any)
	}
	if item == nil {
		item = ""
	}
	f := e.facts
	return map[string]any{
		"item": item,
		"repo": map[string]any{
			"name":       f.Name,
			"stacks":     toAnyList(f.Stacks),
			"forge":      f.Forge,
			"forge_host": f.ForgeHost,
			"visibility": string(f.Visibility),
			"public":     f.Public(),
			"container":  f.Container,
			"tier":       int64(f.Tier),
			"layout":     f.Layout,
			"ci":         toAnyList(f.CI),
			"hooks":      toAnyList(f.Hooks),
			"has_git":    f.HasGit,
			"scope":      e.repo.Scope,
		},
		"release": map[string]any{
			"exists":       f.Release.Exists,
			"mechanisms":   toAnyList(f.Release.Mechanisms),
			"tag_patterns": toAnyList(f.Release.TagPatterns),
			"latest_tag":   f.Release.LatestTag,
		},
		"taskrunner": map[string]any{
			"kind":    f.TaskRunner.Kind,
			"targets": toAnyList(f.TaskRunner.Targets),
			"file":    f.TaskRunner.File,
			"cmd":     f.TaskRunner.Cmd,
			"deps":    listMap(f.TaskRunner.Deps),
			"recipes": listMap(f.TaskRunner.Recipes),
		},
		"params": params,
	}
}

func (e *Env) functions() []cel.EnvOption {
	str := cel.StringType
	strList := cel.ListType(cel.StringType)
	return []cel.EnvOption{
		unaryStr("exists", cel.BoolType, func(s string) ref.Val { return types.Bool(len(e.repo.Glob(s)) > 0) }),
		unaryStr("file", cel.BoolType, func(s string) ref.Val { return types.Bool(e.repo.Has(s)) }),
		unaryStr("glob", strList, func(s string) ref.Val { return strs(e.repo.Glob(s)) }),
		cel.Function("tracked", cel.Overload("tracked_void", nil, strList,
			cel.FunctionBinding(func(...ref.Val) ref.Val { return strs(e.repo.TrackedInScope()) }))),
		unaryStr("text", str, func(s string) ref.Val {
			t, _, err := e.repo.TextErr(s)
			if err != nil {
				return types.NewErr("text: %v", err)
			}
			return types.String(t)
		}),
		unaryStr("lines", strList, func(s string) ref.Val {
			t, ok, err := e.repo.TextErr(s)
			switch {
			case err != nil:
				return types.NewErr("lines: %v", err)
			case !ok:
				return strs(nil)
			}
			return strs(strings.Split(strings.TrimRight(t, "\n"), "\n"))
		}),
		unaryStr("yaml", cel.DynType, func(s string) ref.Val { return e.doc(repo.FormatYAML, s) }),
		unaryStr("toml", cel.DynType, func(s string) ref.Val { return e.doc(repo.FormatTOML, s) }),
		unaryStr("json", cel.DynType, func(s string) ref.Val { return e.doc(repo.FormatJSON, s) }),
		unaryStr("makefile", cel.DynType, func(s string) ref.Val { return e.makefile(s) }),
		cel.Function("scan", cel.Overload("scan_string_string", []*cel.Type{str, str}, cel.ListType(cel.IntType),
			cel.BinaryBinding(func(p, re ref.Val) ref.Val { return e.scan(p, re) }))),
		cel.Function("matchesAny", cel.Overload("matchesany_list_string", []*cel.Type{strList, str}, cel.BoolType,
			cel.BinaryBinding(matchesAny))),
		cel.Function("workflows", cel.Overload("workflows_void", nil, strList,
			cel.FunctionBinding(func(...ref.Val) ref.Val { return strs(e.workflowPaths()) }))),
		cel.Function("tagged_workflows", cel.Overload("tagged_workflows_void", nil, strList,
			cel.FunctionBinding(func(...ref.Val) ref.Val { return strs(e.taggedWorkflows()) }))),
		unaryStr("steps", cel.ListType(cel.MapType(cel.StringType, cel.DynType)), func(s string) ref.Val {
			return types.DefaultTypeAdapter.NativeToValue(e.steps(s))
		}),
	}
}

// copyMap shallow-copies params so normalization never mutates the rule.
func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// HasFile reports whether a scope-relative path exists; the engine uses it
// to decide whether a string item in `foreach` names a file.
func (e *Env) HasFile(path string) bool { return e.repo.Has(path) }

// workflowPaths are the repository's CI files. CI configuration lives at the
// repository root, so in a member scope they are the root's, as ../ paths.
func (e *Env) workflowPaths() []string {
	if e.repo.Scope == "" {
		return ci.Files(e.repo)
	}
	e.rootCI.Do(func() {
		root, err := e.repo.Scoped("")
		if err != nil {
			return
		}
		up := strings.Repeat("../", strings.Count(e.repo.Scope, "/")+1)
		for _, f := range ci.Files(root) {
			e.rootCIFiles = append(e.rootCIFiles, up+f)
		}
	})
	return slices.Clone(e.rootCIFiles)
}

func (e *Env) taggedWorkflows() []string {
	var out []string
	for _, wf := range e.workflowPaths() {
		if len(ci.TagPatterns(e.repo, wf)) > 0 {
			out = append(out, wf)
		}
	}
	return out
}

func (e *Env) steps(path string) []any { return ci.Steps(e.repo, path) }

func unaryStr(name string, out *cel.Type, fn func(string) ref.Val) cel.EnvOption {
	return cel.Function(name, cel.Overload(name+"_string", []*cel.Type{cel.StringType}, out,
		cel.UnaryBinding(func(v ref.Val) ref.Val {
			s, ok := v.Value().(string)
			if !ok {
				return types.NewErr("%s: argument must be a string", name)
			}
			return fn(s)
		})))
}

func (e *Env) doc(format repo.Format, path string) ref.Val {
	v, ok, err := e.repo.Doc(format, path)
	if err != nil {
		return types.NewErr("%s", err.Error())
	}
	if !ok {
		return types.NullValue
	}
	return types.DefaultTypeAdapter.NativeToValue(v)
}

func (e *Env) makefile(path string) ref.Val {
	mf, ok := e.repo.Makefile(path)
	if !ok {
		return types.NullValue
	}
	deps := make(map[string]any, len(mf.Deps))
	for k, v := range mf.Deps {
		deps[k] = toAnyList(v)
	}
	recipes := make(map[string]any, len(mf.Recipes))
	for k, v := range mf.Recipes {
		recipes[k] = toAnyList(v)
	}
	return types.DefaultTypeAdapter.NativeToValue(map[string]any{
		"targets": toAnyList(mf.Targets),
		"deps":    deps,
		"recipes": recipes,
	})
}

func (e *Env) scan(pathV, reV ref.Val) ref.Val {
	path, ok1 := pathV.Value().(string)
	pattern, ok2 := reV.Value().(string)
	if !ok1 || !ok2 {
		return types.NewErr("scan: arguments must be strings")
	}
	re, err := compileRegexp(pattern)
	if err != nil {
		return types.NewErr("scan: bad regex: %v", err)
	}
	text, ok, err := e.repo.TextErr(path)
	switch {
	case err != nil:
		return types.NewErr("scan: %v", err)
	case !ok:
		return types.DefaultTypeAdapter.NativeToValue([]any{})
	}
	var hits []any
	for i, line := range strings.Split(text, "\n") {
		if re.MatchString(line) {
			hits = append(hits, int64(i+1))
		}
	}
	if hits == nil {
		hits = []any{}
	}
	return types.DefaultTypeAdapter.NativeToValue(hits)
}

func matchesAny(listV, reV ref.Val) ref.Val {
	pattern, ok := reV.Value().(string)
	if !ok {
		return types.NewErr("matchesAny: regex must be a string")
	}
	re, err := compileRegexp(pattern)
	if err != nil {
		return types.NewErr("matchesAny: bad regex: %v", err)
	}
	lister, ok := listV.(traits.Lister)
	if !ok {
		return types.NewErr("matchesAny: first argument must be a list")
	}
	for it := lister.Iterator(); it.HasNext() == types.True; {
		if s, ok := it.Next().Value().(string); ok && re.MatchString(s) {
			return types.True
		}
	}
	return types.False
}

func strs(in []string) ref.Val { return types.DefaultTypeAdapter.NativeToValue(toAnyList(in)) }

func toAnyList(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// listMap converts a map of string lists for CEL.
func listMap(m map[string][]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = toAnyList(v)
	}
	return out
}
