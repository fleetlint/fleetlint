package celenv_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func newEnv(t *testing.T, files map[string]string) *celenv.Env {
	t.Helper()
	r := fixture(t, files)
	env, err := celenv.New(context.Background(), r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestListExpressions(t *testing.T) {
	t.Parallel()
	env := newEnv(t, map[string]string{"a.txt": "x", "docs/b.md": "y", "cfg.yaml": "k: v\n"})
	prg, err := env.CompileList(`glob("**")`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.EvalList(prg, nil)
	if err != nil || !reflect.DeepEqual(got, []any{"a.txt", "cfg.yaml", "docs/b.md"}) {
		t.Errorf("glob: %v %v", got, err)
	}
	// Maps and lists an expression builds come back as plain Go values.
	prg, err = env.CompileList(`[{"path": "a.txt", "n": 1, "tags": ["x"]}].map(m, m)`)
	if err != nil {
		t.Fatal(err)
	}
	got, err = env.EvalList(prg, nil)
	want := []any{map[string]any{"path": "a.txt", "n": int64(1), "tags": []any{"x"}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("map literal: %#v %v", got, err)
	}
	if again, err := env.CompileList(`glob("**")`); err != nil || again == nil {
		t.Errorf("a compiled list is cached: %v", err)
	}
	// A dyn result that is not a list is an evaluation error, not a compile error.
	prg, err = env.CompileList(`yaml("cfg.yaml")`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.EvalList(prg, nil); err == nil || !strings.Contains(err.Error(), "not a list") {
		t.Errorf("a map is not a list: %v", err)
	}
	if _, err := env.CompileList(`"nope"`); err == nil || !strings.Contains(err.Error(), "must be list") {
		t.Errorf("a string is refused at compile time: %v", err)
	}
	if _, err := env.CompileList(`1 +`); err == nil {
		t.Error("a syntax error is reported")
	}
	// Params are normalized the way documents are, so a rule compares ints.
	ok, err := env.Check(`params.n == 3 && params.names[0] == "a"`, map[string]any{"n": 3, "names": []string{"a"}})
	if err != nil || !ok {
		t.Errorf("params: %v %v", ok, err)
	}
	if _, err := env.Check(`1 +`, nil); err == nil {
		t.Error("Check reports compile errors")
	}
}

func TestCheckListAndValidateRules(t *testing.T) {
	t.Parallel()
	if err := celenv.CheckList(`glob("**/*.go")`); err != nil {
		t.Errorf("a list expression: %v", err)
	}
	if err := celenv.CheckList(`file("x")`); err == nil {
		t.Error("a bool is not a list")
	}
	good := model.Rule{
		ID: "r/good", Kind: model.KindExpr, Gate: `repo.tier > 0`, When: `file("go.mod")`, Foreach: `glob("**/*.go")`, Expr: `item != ""`,
		Satisfiers: []model.Satisfier{{Name: "s1", When: `true`, Expr: `file("a")`}},
		Accept:     []model.Satisfier{{Expr: `file("b")`}},
		Partials:   []model.Partial{{Name: "p1", Expr: `file("c")`}},
	}
	if err := celenv.ValidateRules([]model.Rule{good, {ID: "r/go", Kind: model.KindGo, Expr: "not cel"}}); err != nil {
		t.Errorf("valid rules: %v", err)
	}
	bad := map[string]model.Rule{
		"when":         {ID: "r/w", When: `nosuch()`},
		"foreach":      {ID: "r/f", Foreach: `true`},
		"expr":         {ID: "r/e", Expr: `"s"`},
		"satisfier s1": {ID: "r/s", Satisfiers: []model.Satisfier{{Name: "s1", Expr: `1`}}},
		"accept[0]":    {ID: "r/a", Accept: []model.Satisfier{{Expr: `1 +`}}},
		"partial p1":   {ID: "r/p", Partials: []model.Partial{{Name: "p1", Expr: `glob("x")`}}},
		"gate":         {ID: "r/g", Gate: `item.`},
	}
	for where, rule := range bad {
		err := celenv.ValidateRules([]model.Rule{rule})
		if err == nil || !strings.Contains(err.Error(), where) || !strings.Contains(err.Error(), rule.ID) {
			t.Errorf("%s: want an error naming it and the rule, got %v", where, err)
		}
	}
}

func TestGrep(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	env := newEnv(t, map[string]string{
		"README.md":    "Intro\nSee TODO: fix this\n```\nTODO in a code block\n```\n~~~\nTODO fenced again\n~~~\nTODO " + long + "\n",
		"docs/x.md":    "nothing here\n",
		"other.txt":    "TODO not a markdown file\n",
		"big/large.md": strings.Repeat("TODO\n", 1024*1024),
	})
	for expr, want := range map[string]bool{
		`grep("**/*.md", "todo: (\\w+)").size() == 1`:                                                     true,
		`grep("**/*.md", "todo: (\\w+)")[0].match == "fix"`:                                               true,
		`grep("**/*.md", "todo: (\\w+)")[0].path == "README.md"`:                                          true,
		`grep("**/*.md", "todo: (\\w+)")[0].line == 2`:                                                    true,
		`grep("**/*.md", "todo")[0].match == "TODO"`:                                                      true,
		`grep("**/*.md", "todo").size() == 2`:                                                             true,
		`grep("**/*.md", "todo")[1].text.endsWith("...") && grep("**/*.md", "todo")[1].text.size() == 80`: true,
		`grep("docs/*.md", "todo").size() == 0`:                                                           true,
		`grep("*.txt", "todo")[0].text == "TODO not a markdown file"`:                                     true,
	} {
		got, err := env.Check(expr, nil)
		if err != nil || got != want {
			t.Errorf("%s = %v (%v), want %v", expr, got, err, want)
		}
	}
	if _, err := env.Check(`grep("*.md", "(").size() == 0`, nil); err == nil {
		t.Error("a bad regex is an error")
	}
}

func TestWorkflowsFromAScope(t *testing.T) {
	t.Parallel()
	r := fixture(t, map[string]string{
		".github/workflows/release.yml": "on:\n  push:\n    tags: ['v*']\njobs:\n  rel:\n    steps:\n      - run: make dist\n",
		"services/api/go.mod":           "module api\n",
		"web/package.json":              "{}",
	})
	for scope, up := range map[string]string{"services/api": "../../", "web": "../"} {
		sc, err := r.Scoped(scope)
		if err != nil {
			t.Fatal(err)
		}
		env, err := celenv.New(context.Background(), sc, facts.Discover(sc, facts.Overrides{}))
		if err != nil {
			t.Fatal(err)
		}
		wf := up + ".github/workflows/release.yml"
		for expr, want := range map[string]bool{
			`workflows() == ["` + wf + `"]`:                       true,
			`tagged_workflows() == ["` + wf + `"]`:                true,
			`steps("` + wf + `").exists(s, s.run == "make dist")`: true,
			`repo.scope == "` + scope + `"`:                       true,
		} {
			got, err := env.Check(expr, nil)
			if err != nil || got != want {
				t.Errorf("%s: %s = %v (%v), want %v", scope, expr, got, err, want)
			}
		}
	}
}

func TestAccessorErrorsAndFacts(t *testing.T) {
	t.Parallel()
	env := newEnv(t, map[string]string{"bad.yaml": "a: [\n", "notes.txt": "a\nb\n", "a.go": "package a\n" + strings.Repeat("x", 300) + "\n", "go.mod": "module a\n"})
	for expr, wantErr := range map[string]bool{
		`scan("notes.txt", "(")`:                       true,
		`scan("missing.txt", "a").size() == 0`:         false,
		`scan("notes.txt", "z").size() == 0`:           false,
		`matchesAny(["a"], "(")`:                       true,
		`matchesAny([], "a") == false`:                 false,
		`yaml("bad.yaml") == null`:                     true,
		`makefile("missing") == null`:                  false,
		`codegrep("code", "(").size() == 0`:            true,
		`codegrep("code", "x+")[0].text.size() == 200`: false,
		`lines("notes.txt") == ["a", "b"]`:             false,
	} {
		_, err := env.Check(expr, nil)
		if (err != nil) != wantErr {
			t.Errorf("%s: err=%v, want error %v", expr, err, wantErr)
		}
	}
	if !env.HasFile("notes.txt") || env.HasFile("missing.txt") {
		t.Error("HasFile")
	}
	ok, err := env.Check(`repo.name == "renamed" && "go" in repo.stacks`, nil)
	if err != nil || ok {
		t.Fatalf("before SetFacts: %v %v", ok, err)
	}
	env.SetFacts(facts.Facts{Name: "renamed", Stacks: []string{"go"}})
	if ok, err := env.Check(`repo.name == "renamed" && "go" in repo.stacks`, nil); err != nil || !ok {
		t.Errorf("after SetFacts: %v %v", ok, err)
	}
}

func TestHistoryWithoutCommits(t *testing.T) {
	t.Parallel()
	r := fixture(t, map[string]string{"a.txt": "a"})
	gitRun(t, r.Root, "init", "-q")
	env, err := celenv.New(context.Background(), r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := env.Check(`commits(3).size() == 0 && tags().size() == 0 && !tag_signed("v1")`, nil); err != nil || !ok {
		t.Errorf("an empty repository has no history: %v %v", ok, err)
	}
	plain := newEnv(t, map[string]string{"a.txt": "a"})
	if ok, err := plain.Check(`commits(3).size() == 0`, nil); err != nil || !ok {
		t.Errorf("no git, no commits: %v %v", ok, err)
	}
	broken := newEnv(t, map[string]string{".git/placeholder": ""})
	if _, err := broken.Check(`commits(3).size() == 0`, nil); err == nil {
		t.Error("a failing git log is an error, not an empty history")
	}
}

func TestCommitStats(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"a.txt": "one\ntwo\n"})
	testutil.WriteFiles(t, r.Root, map[string]string{"a.txt": "one\nthree\nfour\n"})
	gitRun(t, r.Root, "commit", "-q", "-a", "-m", "feat: change\n\nbody text\n\nSigned-off-by: t <t@t>")
	env, err := celenv.New(context.Background(), r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	for expr, want := range map[string]bool{
		`commits(2).size() == 2`:                             true,
		`commits(1)[0].lines == 3`:                           true,
		`commits(2)[1].lines == 2 && commits(2)[1].root`:     true,
		`!commits(1)[0].root`:                                true,
		`commits(1)[0].body.startsWith("body text")`:         true,
		`commits(1)[0].trailers == "Signed-off-by: t <t@t>"`: true,
	} {
		got, err := env.Check(expr, nil)
		if err != nil || got != want {
			t.Errorf("%s = %v (%v), want %v", expr, got, err, want)
		}
	}
}
