package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/fleetlint/fleetlint/internal/docs"
)

func cliReferencePage(t *testing.T) string {
	t.Helper()
	pages, err := docs.Generate(docs.Options{CLI: reference()})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		if p.Path == "cli-reference.md" {
			return string(p.Content)
		}
	}
	t.Fatal("no cli-reference.md")
	return ""
}

// Every command and flag of the tree is in the CLI reference, with every
// exit code.
func TestCLIReferenceCoversTheTree(t *testing.T) {
	t.Parallel()
	page := cliReferencePage(t)
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "help" {
			return
		}
		if inCompletion := c.HasParent() && c.Parent().Name() == "completion"; !inCompletion && !strings.Contains(page, "### "+c.CommandPath()+"\n") {
			t.Errorf("no section for %s", c.CommandPath())
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !strings.Contains(page, "--"+f.Name+"`") && !strings.Contains(page, "--"+f.Name+" ") {
				t.Errorf("%s: flag --%s missing", c.CommandPath(), f.Name)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	root := reference().Root
	walk(root)
	for _, code := range []int{ExitOK, ExitFindings, ExitConfig, ExitInternal} {
		if strings.Count(page, "| `"+strconv.Itoa(code)+"` |") != 1 {
			t.Errorf("exit code %d is not documented once", code)
		}
	}
	if !strings.Contains(page, "| `--color` | on when stdout is a terminal") {
		t.Error("--color's default depends on the terminal and is given in words")
	}
}

// envReaders are the functions that read the environment by name, and the
// argument that names the variable.
var envReaders = map[string]int{"Getenv": 0, "LookupEnv": 0}

// The environment table names every variable the module reads, and nothing
// it does not: a variable read with os.Getenv or os.LookupEnv, directly or
// through a function that passes its parameter on, with a constant name.
func TestEnvironmentTableIsComplete(t *testing.T) {
	t.Parallel()
	read := envReads(t, filepath.Join("..", ".."))
	listed := map[string]bool{}
	install, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range environment {
		for _, name := range v.Names {
			listed[name] = true
			switch {
			case v.InstallOnly && !strings.Contains(string(install), "${"+name):
				t.Errorf("%s is not read by scripts/install.sh", name)
			case !v.InstallOnly && !read[name]:
				t.Errorf("%s is listed but not read", name)
			}
		}
	}
	for name := range read {
		if !listed[name] {
			t.Errorf("%s is read but not in the environment table (internal/cli/reference.go)", name)
		}
	}
}

// envReads walks the module's non-test sources (test helpers excluded) for
// the variables they read.
func envReads(t *testing.T, root string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == "testdata" || d.Name() == "testutil" || strings.HasPrefix(d.Name(), ".") && path != root):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		files = append(files, f)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	consts := stringConsts(files)
	readers := wrappers(files)
	read := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			arg, ok := readers[calleeName(call)]
			if !ok || arg >= len(call.Args) {
				return true
			}
			switch name := constValue(call.Args[arg], consts); {
			case name != "":
				read[name] = true
			case !isParam(call.Args[arg], f, call):
				t.Errorf("%s: cannot tell which variable this reads", fset.Position(call.Pos()))
			}
			return true
		})
	}
	return read
}

// wrappers extends envReaders with the functions that pass a parameter on
// to one of them, until no more are found.
func wrappers(files []*ast.File) map[string]int {
	readers := map[string]int{}
	for k, v := range envReaders {
		readers[k] = v
	}
	for grown := true; grown; {
		grown = false
		for _, f := range files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if _, known := readers[fn.Name.Name]; known {
					continue
				}
				if i := forwardedParam(fn, readers); i >= 0 {
					readers[fn.Name.Name] = i
					grown = true
				}
			}
		}
	}
	return readers
}

// forwardedParam is the index of the parameter fn passes to a reader as the
// variable's name; -1 if none.
func forwardedParam(fn *ast.FuncDecl, readers map[string]int) int {
	var params []string
	for _, field := range fn.Type.Params.List {
		for _, n := range field.Names {
			params = append(params, n.Name)
		}
	}
	found := -1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if arg, ok := readers[calleeName(call)]; ok && arg < len(call.Args) {
			if id, ok := call.Args[arg].(*ast.Ident); ok && slices.Contains(params, id.Name) {
				found = slices.Index(params, id.Name)
			}
		}
		return true
	})
	return found
}

func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

var envName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// stringConsts are the string constants of the sources, by name.
func stringConsts(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range spec.Names {
				if i < len(spec.Values) {
					if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						consts[name.Name], _ = strconv.Unquote(lit.Value)
					}
				}
			}
			return true
		})
	}
	return consts
}

// constValue is the variable name an argument spells: a string literal or
// a constant; "" otherwise.
func constValue(e ast.Expr, consts map[string]string) string {
	var v string
	switch e := e.(type) {
	case *ast.BasicLit:
		v, _ = strconv.Unquote(e.Value)
	case *ast.Ident:
		v = consts[e.Name]
	case *ast.SelectorExpr:
		v = consts[e.Sel.Name]
	}
	if envName.MatchString(v) {
		return v
	}
	return ""
}

// isParam reports whether the argument is a parameter of the function the
// call is in: the call is then a wrapper's, and its callers are checked.
func isParam(e ast.Expr, f *ast.File, call *ast.CallExpr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || call.Pos() < fn.Body.Pos() || call.End() > fn.Body.End() {
			continue
		}
		for _, field := range fn.Type.Params.List {
			for _, n := range field.Names {
				if n.Name == id.Name {
					return true
				}
			}
		}
	}
	return false
}
