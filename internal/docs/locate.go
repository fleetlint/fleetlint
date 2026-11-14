package docs

import (
	"fmt"
	"strings"

	"cel.dev/cel-go/common"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/operators"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/parser"
)

// locatePhrases name the values a computed `locate` points at, by their
// canonical CEL text.
var locatePhrases = map[string]string{
	"taskrunner.file":       "the task runner's file",
	"artifact.workflows[0]": "the first workflow that publishes the artifact",
	"tagged_workflows()[0]": "the first workflow a tag push starts",
}

// locateText describes in words the file a `locate` expression yields;
// ok is false for a shape it does not know.
func locateText(expr string, params map[string]any) (string, bool) {
	p, err := parser.NewParser() // without macros, so `filter` stays a call
	if err != nil {
		return "", false
	}
	tree, errs := p.Parse(common.NewTextSource(expr))
	if len(errs.GetErrors()) > 0 {
		return "", false
	}
	l := locator{info: tree.SourceInfo(), params: params}
	return l.describe(tree.Expr())
}

type locator struct {
	info   *ast.SourceInfo
	params map[string]any
}

func (l locator) describe(e ast.Expr) (string, bool) {
	if s, ok := stringLit(e); ok && s != "" {
		return "`" + s + "`", true
	}
	if phrase, ok := locatePhrases[l.text(e)]; ok {
		return phrase, true
	}
	if e.Kind() != ast.CallKind {
		return "", false
	}
	call := e.AsCall()
	args := call.Args()
	switch call.FunctionName() {
	case operators.Conditional:
		return l.unlessMissing(args[0], args[1], args[2])
	case operators.Index:
		return l.firstExisting(args[0], args[1])
	}
	return "", false
}

// unlessMissing is `cond ? value : "fallback"` where cond only tests that
// value is there: non-empty, a non-empty list's first element, an existing
// file.
func (l locator) unlessMissing(cond, value, fallback ast.Expr) (string, bool) {
	v, c := l.text(value), l.text(cond)
	list, first := strings.CutSuffix(v, "[0]")
	if c != v+` != ""` && c != "file("+v+")" && (!first || c != list+".size() > 0") {
		return "", false
	}
	what, ok := l.describe(value)
	other, isLit := stringLit(fallback)
	if !ok || !isLit {
		return "", false
	}
	return fmt.Sprintf("%s, or %s when there is none", what, orNoFile(other)), true
}

// firstExisting is `(candidates + ["fallback"])[0]`, where each candidate
// list holds only files that exist.
func (l locator) firstExisting(list, index ast.Expr) (string, bool) {
	if l.text(index) != "0" {
		return "", false
	}
	parts := addends(list)
	last := parts[len(parts)-1]
	if last.Kind() != ast.ListKind || len(last.AsList().Elements()) != 1 {
		return "", false
	}
	fallback, ok := stringLit(last.AsList().Elements()[0])
	if !ok || len(parts) < 2 {
		return "", false
	}
	var names []string
	for _, p := range parts[:len(parts)-1] {
		found, ok := l.existing(p)
		if !ok {
			return "", false
		}
		names = append(names, found...)
	}
	return fmt.Sprintf("the first existing file of %s, else %s", codeList(names), orNoFile(fallback)), true
}

// existing are the paths or globs of a list that holds only existing files:
// `glob("p")` or `names.filter(f, file(f))`.
func (l locator) existing(e ast.Expr) ([]string, bool) {
	if e.Kind() != ast.CallKind {
		return nil, false
	}
	call := e.AsCall()
	args := call.Args()
	switch {
	case call.FunctionName() == "glob" && !call.IsMemberFunction() && len(args) == 1:
		p, ok := stringLit(args[0])
		return []string{p}, ok
	case call.FunctionName() == "filter" && call.IsMemberFunction() && len(args) == 2 &&
		args[0].Kind() == ast.IdentKind && l.text(args[1]) == "file("+args[0].AsIdent()+")":
		return l.names(call.Target())
	}
	return nil, false
}

// names are the strings of a list literal or of a list parameter.
func (l locator) names(e ast.Expr) ([]string, bool) {
	var out []string
	switch {
	case e.Kind() == ast.ListKind:
		for _, el := range e.AsList().Elements() {
			s, ok := stringLit(el)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
	case e.Kind() == ast.SelectKind && l.text(e.AsSelect().Operand()) == "params":
		list, ok := l.params[e.AsSelect().FieldName()].([]any)
		if !ok {
			return nil, false
		}
		for _, v := range list {
			s, ok := v.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
	}
	return out, len(out) > 0
}

// addends flattens a chain of `+`.
func addends(e ast.Expr) []ast.Expr {
	if e.Kind() == ast.CallKind && e.AsCall().FunctionName() == operators.Add {
		args := e.AsCall().Args()
		return append(addends(args[0]), addends(args[1])...)
	}
	return []ast.Expr{e}
}

// text is the canonical CEL source of an expression; "" when it has none.
func (l locator) text(e ast.Expr) string {
	s, err := parser.Unparse(e, l.info)
	if err != nil {
		return ""
	}
	return s
}

func stringLit(e ast.Expr) (string, bool) {
	if e.Kind() != ast.LiteralKind {
		return "", false
	}
	s, ok := e.AsLiteral().(types.String)
	return string(s), ok
}

func orNoFile(path string) string {
	if path == "" {
		return "no file"
	}
	return "`" + path + "`"
}

// codeList is `a`, `b` or `c`.
func codeList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = "`" + s + "`"
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}
