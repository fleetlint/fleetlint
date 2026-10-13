package celenv

import (
	"regexp"
	"strconv"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"

	"github.com/fleetlint/fleetlint/internal/ci"
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
		unaryStr("triggers", strList, func(s string) ref.Val { return strs(ci.Triggers(e.repo, s)) }),
		unaryStr("jobs", mapList, func(s string) ref.Val { return types.DefaultTypeAdapter.NativeToValue(ci.Jobs(e.repo, s)) }),
		unaryStr("ci_kind", str, func(s string) ref.Val { return types.String(ci.Kind(s)) }),
		unaryStr("includes", mapList, func(s string) ref.Val { return types.DefaultTypeAdapter.NativeToValue(ci.Includes(e.repo, s)) }),
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
		unaryStr("tag_signed", cel.BoolType, func(s string) ref.Val { return types.Bool(e.repo.TagSigned(s)) }),
		cel.Function("commits", cel.Overload("commits_int", []*cel.Type{cel.IntType}, mapList,
			cel.UnaryBinding(func(n ref.Val) ref.Val {
				count, ok := n.Value().(int64)
				if !ok || count <= 0 {
					return types.NewErr("commits: argument must be a positive int")
				}
				list, err := e.commits(int(count))
				if err != nil {
					return types.NewErr("commits: %v", err)
				}
				return types.DefaultTypeAdapter.NativeToValue(list)
			}))),
	}
}

// commits returns the newest n commits as `{hash, author, email, subject,
// body, trailers, lines, root}`; lines is insertions plus deletions, root
// marks a commit without parents.
// Without git or without commits the list is empty (rules that need
// history guard with `repo.has_git`); a failing git log is an error, so a
// rule about commits never passes on history it could not read.
func (e *Env) commits(n int) ([]any, error) {
	const sep = "\x1f"
	if !e.repo.HasGit() {
		return []any{}, nil
	}
	out, err := e.repo.Git("log", "-n", strconv.Itoa(n), "--format=%H"+sep+"%P"+sep+"%an"+sep+"%ae"+sep+"%s"+sep+"%b"+sep+"%(trailers)"+"\x1e", "--shortstat")
	switch {
	case err != nil && strings.Contains(err.Error(), "does not have any commits"):
		return []any{}, nil
	case err != nil:
		return nil, err
	case out == "":
		return []any{}, nil
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
		f := strings.SplitN(rec, sep, 7)
		for len(f) < 7 {
			f = append(f, "")
		}
		list = append(list, map[string]any{
			"hash": f[0], "root": f[1] == "", "author": f[2], "email": f[3], "subject": f[4],
			"body": strings.TrimSpace(f[5]), "trailers": strings.TrimSpace(f[6]), "lines": int64(0),
		})
	}
	return list, nil
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
	rx, err := compileRegexp("(?i)" + re)
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
