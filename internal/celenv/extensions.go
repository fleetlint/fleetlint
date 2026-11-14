package celenv

import (
	"runtime/debug"
	"slices"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/decls"
	"cel.dev/cel-go/ext"
)

// Extension is a cel-go extension library the environment enables.
type Extension struct {
	// Name is the library's name in cel-go's ext package (Strings, Lists).
	Name   string
	option cel.EnvOption
}

// Extensions are the libraries enabled besides CEL's standard one.
var Extensions = []Extension{
	{Name: "Strings", option: ext.Strings()},
	{Name: "Lists", option: ext.Lists()},
}

// Function is a function or macro an extension adds.
type Function struct {
	Name string
	// Member is true for a function called on a value (`s.split(",")`).
	Member bool
}

// Functions are what the extension adds to the standard environment,
// sorted by name; internal helpers (`@...`) are left out.
func (x Extension) Functions() ([]Function, error) {
	base, err := cel.NewEnv()
	if err != nil {
		return nil, err
	}
	env, err := cel.NewEnv(x.option)
	if err != nil {
		return nil, err
	}
	var out []Function
	for name, decl := range env.Functions() {
		if base.HasFunction(name) || strings.HasPrefix(name, "@") {
			continue
		}
		member := slices.ContainsFunc(decl.OverloadDecls(), func(o *decls.OverloadDecl) bool { return o.IsMemberFunction() })
		out = append(out, Function{Name: name, Member: member})
	}
	known := map[string]bool{}
	for _, m := range base.Macros() {
		known[m.Function()] = true
	}
	for _, m := range env.Macros() {
		if !known[m.Function()] {
			out = append(out, Function{Name: m.Function(), Member: m.IsReceiverStyle()})
		}
	}
	slices.SortFunc(out, func(a, b Function) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// ExtensionDocs is the documentation of cel-go's ext package for the
// version built in.
func ExtensionDocs() string {
	const module = "cel.dev/cel-go"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == module && dep.Replace == nil && strings.HasPrefix(dep.Version, "v") {
				return "https://pkg.go.dev/" + module + "@" + dep.Version + "/ext"
			}
		}
	}
	return "https://pkg.go.dev/" + module + "/ext"
}
