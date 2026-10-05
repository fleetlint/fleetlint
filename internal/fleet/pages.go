package fleet

import (
	"html"
	"strings"
)

// actionsHTML renders an action list (our own Markdown subset: headings,
// paragraphs and fenced blocks) as a standalone page, so a static host that
// serves .md as plain text still shows a readable list. Everything is
// escaped; the input is trusted structure around untrusted finding text.
func actionsHTML(name string, md []byte) []byte {
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset=utf-8><title>fleetlint: ")
	b.WriteString(html.EscapeString(name))
	b.WriteString("</title><style>body{font:15px/1.5 system-ui,sans-serif;max-width:60rem;margin:2rem auto;padding:0 1rem;color:#1b1b1b}" +
		"pre{background:#f4f4f4;padding:.75rem;overflow:auto}h2{margin-top:2rem}a{color:#005a9c}</style>" +
		"<p><a href=\"../index.html\">&larr; fleet report</a></p>\n")
	inFence := false
	for _, line := range strings.Split(string(md), "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			if inFence {
				b.WriteString("</pre>\n")
			} else {
				b.WriteString("<pre>")
			}
			inFence = !inFence
		case inFence:
			b.WriteString(html.EscapeString(line))
			b.WriteString("\n")
		case strings.HasPrefix(line, "## "):
			b.WriteString("<h2>" + html.EscapeString(line[3:]) + "</h2>\n")
		case strings.HasPrefix(line, "# "):
			b.WriteString("<h1>" + html.EscapeString(line[2:]) + "</h1>\n")
		case strings.TrimSpace(line) == "":
		default:
			b.WriteString("<p>" + html.EscapeString(line) + "</p>\n")
		}
	}
	if inFence {
		b.WriteString("</pre>\n")
	}
	return []byte(b.String())
}
