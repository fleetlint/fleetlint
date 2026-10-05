package celenv

import (
	"regexp"
	"strconv"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// ciAccessors are the functions that look at workflows, tags, history and
// file sizes. They are split from the core accessors so each file stays
// readable; the documentation table in Accessors covers both.
func (e *Env) ciAccessors() []cel.EnvOption {
	str := cel.StringType
	strList := cel.ListType(cel.StringType)
	mapList := cel.ListType(cel.MapType(cel.StringType, cel.DynType))
	return []cel.EnvOption{
		unaryStr("filesize", cel.IntType, func(s string) ref.Val { return types.Int(e.repo.Size(s)) }),
		cel.Function("globmatch", cel.Overload("globmatch_string_string", []*cel.Type{str, str}, cel.BoolType,
			cel.BinaryBinding(func(pattern, name ref.Val) ref.Val {
				p, ok1 := pattern.Value().(string)
				n, ok2 := name.Value().(string)
				if !ok1 || !ok2 {
					return types.NewErr("globmatch: arguments must be strings")
				}
				return types.Bool(repo.MatchGlob(p, n))
			}))),
		unaryStr("triggers", strList, func(s string) ref.Val { return strs(e.triggers(s)) }),
		unaryStr("jobs", mapList, func(s string) ref.Val { return types.DefaultTypeAdapter.NativeToValue(e.jobs(s)) }),
		cel.Function("grep", cel.Overload("grep_string_string", []*cel.Type{str, str}, mapList,
			cel.BinaryBinding(func(pattern, re ref.Val) ref.Val {
				p, ok1 := pattern.Value().(string)
				rx, ok2 := re.Value().(string)
				if !ok1 || !ok2 {
					return types.NewErr("grep: arguments must be strings")
				}
				out, err := e.grep(p, rx)
				if err != nil {
					return types.NewErr("grep: %s", err.Error())
				}
				return types.DefaultTypeAdapter.NativeToValue(out)
			}))),
		cel.Function("tags", cel.Overload("tags_void", nil, strList,
			cel.FunctionBinding(func(...ref.Val) ref.Val { return strs(e.repo.Tags()) }))),
		cel.Function("commits", cel.Overload("commits_int", []*cel.Type{cel.IntType}, mapList,
			cel.UnaryBinding(func(n ref.Val) ref.Val {
				count, ok := n.Value().(int64)
				if !ok || count <= 0 {
					return types.NewErr("commits: argument must be a positive int")
				}
				return types.DefaultTypeAdapter.NativeToValue(e.commits(int(count)))
			}))),
	}
}

// triggers lists the event names a workflow reacts to, whatever the YAML
// shape (`on: push`, `on: [push, pull_request]`, `on: {push: {...}}`).
func (e *Env) triggers(path string) []string {
	doc, ok, err := e.repo.Doc(repo.FormatYAML, path)
	if !ok || err != nil {
		return nil
	}
	m, _ := doc.(map[string]any)
	switch on := m["on"].(type) {
	case string:
		return []string{on}
	case []any:
		out := make([]string, 0, len(on))
		for _, v := range on {
			out = append(out, str(v))
		}
		return out
	case map[string]any:
		out := make([]string, 0, len(on))
		for k := range on {
			out = append(out, k)
		}
		sortStrings(out)
		return out
	}
	return nil
}

// jobs lists a workflow's jobs as `{name, timeout_minutes, permissions, runs_on, uses}`
// with zero values for absent fields. timeout_minutes is 0 when unset.
func (e *Env) jobs(path string) []any {
	doc, ok, err := e.repo.Doc(repo.FormatYAML, path)
	if !ok || err != nil {
		return []any{}
	}
	m, _ := doc.(map[string]any)
	jobs, _ := m["jobs"].(map[string]any)
	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}
	sortStrings(names)
	out := make([]any, 0, len(names))
	for _, name := range names {
		job, _ := jobs[name].(map[string]any)
		perms := job["permissions"]
		if perms == nil {
			perms = map[string]any{}
		}
		out = append(out, map[string]any{
			"name":            name,
			"timeout_minutes": intOf(job["timeout-minutes"]),
			"permissions":     perms,
			"runs_on":         str(job["runs-on"]),
			"uses":            str(job["uses"]),
		})
	}
	return out
}

// commits returns the newest n commits as `{hash, author, email, subject,
// body, trailers, lines}`; lines is insertions plus deletions.
// Without git, or on error, the list is empty; rules that need history should
// guard with `repo.has_git`.
func (e *Env) commits(n int) []any {
	const sep = "\x1f"
	out, err := e.repo.Git("log", "-n", strconv.Itoa(n), "--format=%H"+sep+"%an"+sep+"%ae"+sep+"%s"+sep+"%b"+sep+"%(trailers)"+"\x1e", "--shortstat")
	if err != nil || out == "" {
		return []any{}
	}
	// git prints each commit's shortstat after its record separator, so it
	// arrives at the head of the next chunk.
	records := strings.Split(out, "\x1e")
	list := make([]any, 0, len(records))
	for _, rec := range records {
		stat, rec := splitShortstat(rec)
		if stat >= 0 && len(list) > 0 {
			list[len(list)-1].(map[string]any)["lines"] = int64(stat)
		}
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, sep, 6)
		for len(f) < 6 {
			f = append(f, "")
		}
		list = append(list, map[string]any{
			"hash": f[0], "author": f[1], "email": f[2], "subject": f[3],
			"body": strings.TrimSpace(f[4]), "trailers": strings.TrimSpace(f[5]), "lines": int64(0),
		})
	}
	return list
}

var shortstatRe = regexp.MustCompile(`^\s*\d+ files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?\s*`)

// splitShortstat separates a leading `git log --shortstat` line from the
// record that follows it. It returns -1 when there is no such line.
func splitShortstat(chunk string) (lines int, rest string) {
	m := shortstatRe.FindStringSubmatch(chunk)
	if m == nil {
		return -1, strings.TrimSpace(chunk)
	}
	ins, _ := strconv.Atoi(m[1])
	del, _ := strconv.Atoi(m[2])
	return ins + del, strings.TrimSpace(chunk[len(m[0]):])
}

// grep returns `{path, line, text}` for every line of every file matching
// the glob that matches the regular expression (case-insensitive). Files
// over the text size cap are skipped, as everywhere else.
func (e *Env) grep(pattern, re string) ([]any, error) {
	rx, err := regexp.Compile("(?i)" + re)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, path := range e.repo.Glob(pattern) {
		text, ok := e.repo.Text(path)
		if !ok {
			continue
		}
		out = append(out, grepText(rx, path, text)...)
	}
	return out, nil
}

// grepText returns the matches in one file, leaving out fenced code blocks:
// in documentation they are examples, not prose.
func grepText(rx *regexp.Regexp, path, text string) []any {
	var out []any
	inFence := false
	for i, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := rx.FindStringSubmatch(line); m != nil {
			out = append(out, map[string]any{"path": path, "line": int64(i + 1), "text": excerpt(line), "match": captured(m)})
		}
	}
	return out
}

// captured is what a rule gets as `item.match`: the first capture group
// when the pattern has one, else the whole match, never shortened.
func captured(m []string) string {
	if len(m) > 1 {
		return m[1]
	}
	return m[0]
}

// excerpt keeps a matched line short enough to read in a finding.
func excerpt(line string) string {
	line = strings.TrimSpace(line)
	if len(line) > 80 {
		return line[:77] + "..."
	}
	return line
}

func intOf(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case uint64:
		return int64(t) //nolint:gosec // workflow timeouts never approach the int64 range
	case string:
		i, _ := strconv.ParseInt(t, 10, 64)
		return i
	}
	return 0
}
