// Package celenv builds the CEL environment rules are evaluated in. It is the
// contract between catalogs and the engine: the variables and functions
// declared here are the whole surface an `expr` rule can use, and they are
// documented from this file.
package celenv

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/common/types/traits"
	"cel.dev/cel-go/ext"

	"github.com/fleetlint/fleetlint/internal/facts"
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
	{"json(path: string) -> dyn", "Parsed JSON document; null when absent."},
	{"makefile(path: string) -> map", "`{targets: list<string>, deps: map<string, list<string>>, recipes: map<string, list<string>>}`; null when absent."},
	{"scan(path: string, regex: string) -> list<int>", "1-based line numbers where the regex matches."},
	{"matchesAny(list: list<string>, regex: string) -> bool", "True if any element matches the regex."},
	{"workflows() -> list<string>", "Paths of CI workflow files (`.github/workflows`, `.gitea/workflows`)."},
	{"tagged_workflows() -> list<string>", "Workflow paths triggered by tag pushes (`on.push.tags`), i.e. release pipelines."},
	{"steps(path: string) -> list<map>", "Every step of every job in a workflow, each as `{job, name, uses, run, with}` with `\"\"`/`{}` for absent fields."},
	{"jobs(path: string) -> list<map>", "Every job of a workflow as `{name, timeout_minutes, permissions, runs_on, uses}`; `timeout_minutes` is 0 when unset."},
	{"triggers(path: string) -> list<string>", "Event names a workflow reacts to (`push`, `pull_request`, `pull_request_target`, ...), whatever the `on:` shape."},
	{"tags() -> list<string>", "Git tags, newest version first."},
	{"tag_signed(name: string) -> bool", "True if the tag is an annotated tag carrying a PGP, SSH or X.509 signature; false for lightweight or unsigned tags. The signature is not verified."},
	{"commits(n: int) -> list<map>", "Newest n commits as `{hash, author, email, subject, body, trailers, lines}`; `lines` is insertions plus deletions; empty without git."},
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

// Env wraps a compiled CEL environment bound to one repository scope.
type Env struct {
	env   *cel.Env
	repo  *repo.Repo
	facts facts.Facts
	cache map[string]cel.Program
}

// New builds the environment for a repository scope.
func New(r *repo.Repo, f facts.Facts) (*Env, error) {
	e := &Env{repo: r, facts: f, cache: map[string]cel.Program{}}
	vars := []cel.EnvOption{
		cel.Variable("repo", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("release", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("taskrunner", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("params", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("item", cel.DynType),
	}
	fns, ci, code := e.functions(), e.ciAccessors(), e.codeAccessors()
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
	ast, iss := e.env.Compile(expr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("compile %q: %w", expr, iss.Err())
	}
	got := ast.OutputType()
	if !acceptable(got, want) {
		return nil, fmt.Errorf("compile %q: result must be %s, got %s", expr, want, got)
	}
	prg, err := e.env.Program(ast, cel.EvalOptions(cel.OptOptimize))
	if err != nil {
		return nil, fmt.Errorf("program %q: %w", expr, err)
	}
	e.cache[key] = prg
	return prg, nil
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
	out, _, err := prg.Eval(e.activation(params, nil))
	if err != nil {
		return nil, fmt.Errorf("evaluate: %w", err)
	}
	v, err := out.ConvertToNative(reflect.TypeOf([]any{}))
	if err != nil {
		return nil, fmt.Errorf("evaluate: result is not a list: %w", err)
	}
	list, _ := v.([]any)
	return list, nil
}

// EvalBoolWith evaluates a predicate with `item` bound.
func (e *Env) EvalBoolWith(prg cel.Program, params map[string]any, item any) (bool, error) {
	out, _, err := prg.Eval(e.activation(params, item))
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
		unaryStr("text", str, func(s string) ref.Val { t, _ := e.repo.Text(s); return types.String(t) }),
		unaryStr("lines", strList, func(s string) ref.Val {
			t, ok := e.repo.Text(s)
			if !ok {
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

func sortStrings(s []string) { sort.Strings(s) }

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

func (e *Env) workflowPaths() []string {
	return append(e.repo.Glob(".github/workflows/*.y*ml"), e.repo.Glob(".gitea/workflows/*.y*ml")...)
}

func (e *Env) taggedWorkflows() []string {
	var out []string
	for _, wf := range e.workflowPaths() {
		doc, ok, err := e.repo.Doc(repo.FormatYAML, wf)
		if !ok || err != nil {
			continue
		}
		m, _ := doc.(map[string]any)
		on, _ := m["on"].(map[string]any)
		push, _ := on["push"].(map[string]any)
		if tags, _ := push["tags"].([]any); len(tags) > 0 {
			out = append(out, wf)
		}
	}
	return out
}

// steps flattens a workflow's jobs into one list of step maps with stable
// keys so expressions never hit a missing field.
func (e *Env) steps(path string) []any {
	doc, ok, err := e.repo.Doc(repo.FormatYAML, path)
	if !ok || err != nil {
		return []any{}
	}
	m, _ := doc.(map[string]any)
	jobs, _ := m["jobs"].(map[string]any)
	out := []any{}
	for jobName, j := range jobs {
		job, _ := j.(map[string]any)
		list, _ := job["steps"].([]any)
		for _, s := range list {
			step, _ := s.(map[string]any)
			with, _ := step["with"].(map[string]any)
			if with == nil {
				with = map[string]any{}
			}
			out = append(out, map[string]any{
				"job":  jobName,
				"name": str(step["name"]),
				"uses": str(step["uses"]),
				"run":  str(step["run"]),
				"with": with,
			})
		}
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

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
	re, err := regexp.Compile(pattern)
	if err != nil {
		return types.NewErr("scan: bad regex: %v", err)
	}
	text, ok := e.repo.Text(path)
	if !ok {
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
	re, err := regexp.Compile(pattern)
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
