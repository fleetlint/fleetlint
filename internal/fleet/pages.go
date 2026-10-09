package fleet

import (
	"html"
	"strings"
)

// actionsHTML renders an action list (our own Markdown subset: headings,
// paragraphs with `code` spans and fenced blocks) as a standalone page, so a static host that
// serves .md as plain text still shows a readable list. Everything is
// escaped; the input is trusted structure around untrusted finding text.
func actionsHTML(name string, md []byte) []byte {
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset=utf-8><title>fleetlint: ")
	b.WriteString(html.EscapeString(name))
	b.WriteString("</title><meta name=viewport content=\"width=device-width,initial-scale=1\"><style>" + baseCSS + pageCSS + "</style>" +
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
			b.WriteString("<h2>" + inlineHTML(line[3:]) + "</h2>\n")
		case strings.HasPrefix(line, "# "):
			b.WriteString("<h1>" + inlineHTML(line[2:]) + "</h1>\n")
		case strings.TrimSpace(line) == "":
		default:
			b.WriteString("<p>" + inlineHTML(line) + "</p>\n")
		}
	}
	if inFence {
		b.WriteString("</pre>\n")
	}
	return []byte(b.String())
}
