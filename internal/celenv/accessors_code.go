package celenv

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// Source files codegrep looks at, and the paths it never reads: vendored,
// generated and build output.
var (
	codeExts = map[string]bool{
		".go": true, ".py": true, ".rs": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
		".cjs": true, ".kt": true, ".kts": true, ".java": true, ".dart": true, ".swift": true, ".sh": true,
	}
	codeSkipRe = regexp.MustCompile(`(^|/)(vendor|third_party|node_modules|packages|build|dist|target|out|\.dart_tool|\.gradle|coverage|generated)/` +
		`|\.(g|freezed|gr|mocks|config)\.dart$|\.pb\.go$|_pb2\.py$|\.min\.js$|(^|/)l10n/app_localizations[^/]*\.dart$`)
	testPathRe = regexp.MustCompile(`(^|/)(test|tests|__tests__|spec|androidTest|integration_test|testdata)/|_test\.|\.test\.|\.spec\.|(^|/)test_[^/]*\.py$|Test\.(kt|java)$`)
)

const codeLineMax = 200

// codeAccessors are the functions that read source code line by line.
func (e *Env) codeAccessors() []cel.EnvOption {
	str := cel.StringType
	mapList := cel.ListType(cel.MapType(cel.StringType, cel.DynType))
	return []cel.EnvOption{
		cel.Function("codegrep", cel.Overload("codegrep_string_string", []*cel.Type{str, str}, mapList,
			cel.BinaryBinding(func(kind, re ref.Val) ref.Val {
				k, ok1 := kind.Value().(string)
				rx, ok2 := re.Value().(string)
				if !ok1 || !ok2 {
					return types.NewErr("codegrep: arguments must be strings")
				}
				out, err := e.codegrep(k, rx)
				if err != nil {
					return types.NewErr("codegrep: %s", err.Error())
				}
				return types.DefaultTypeAdapter.NativeToValue(out)
			}))),
	}
}

// codegrep returns `{path, line, text}` for every line of tracked source
// code that matches the regular expression. kind selects the files: "code"
// is all source files, "test" and "nontest" split them by path. Matching is
// case-sensitive; a pattern opts out with `(?i)`.
func (e *Env) codegrep(kind, re string) ([]any, error) {
	if kind != "code" && kind != "test" && kind != "nontest" {
		return nil, fmt.Errorf("kind %q is not code, test or nontest", kind)
	}
	rx, err := regexp.Compile(re)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, p := range e.repo.TrackedInScope() {
		if !codeFile(kind, p) {
			continue
		}
		text, ok := e.repo.Text(p)
		if !ok {
			continue
		}
		for i, line := range strings.Split(text, "\n") {
			if rx.MatchString(line) {
				out = append(out, map[string]any{"path": p, "line": int64(i + 1), "text": clip(line)})
			}
		}
	}
	return out, nil
}

// codeFile reports whether codegrep reads the path for the given kind.
func codeFile(kind, p string) bool {
	if !codeExts[path.Ext(p)] || codeSkipRe.MatchString(p) {
		return false
	}
	isTest := testPathRe.MatchString(p)
	return kind == "code" || (kind == "test") == isTest
}

// clip trims a source line and bounds its length for use in a finding.
func clip(line string) string {
	line = strings.TrimSpace(line)
	if len(line) > codeLineMax {
		return line[:codeLineMax]
	}
	return line
}
