package mailcore

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// MarkdownToHTML renders Markdown as the HTML subset the window's
// rich-text view draws: headings, paragraphs with hard line breaks,
// emphasis, code, links and images, nested lists, quotes, fenced and
// indented code, rules, and tables (GitHub's pipe tables: a header row,
// then the rows under it). Raw HTML in the Markdown is shown as text,
// never passed through. It reads what MessageMarkdown writes, and the Markdown people
// write in plain-text mail (CommonMark's common ground, GitHub's tables).
func MarkdownToHTML(md string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\t", "    "), "\n")
	var b strings.Builder
	renderBlocks(&b, lines, 0)
	return b.String()
}

var (
	mdHeading   = regexp.MustCompile(`^ {0,3}(#{1,6})(?:\s+(.*?))?\s*#*\s*$`)
	mdRule      = regexp.MustCompile(`^ {0,3}([-*_])(?:\s*[-*_]){2,}\s*$`)
	mdFence     = regexp.MustCompile("^ {0,3}(```+|~~~+)")
	mdBullet    = regexp.MustCompile(`^( *)([-*+]) +(.*)$`)
	mdOrdered   = regexp.MustCompile(`^( *)(\d{1,9})[.)] +(.*)$`)
	mdTableSep  = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$`)
	mdQuoteLine = regexp.MustCompile(`^ {0,3}> ?(.*)$`)
	mdSetext    = regexp.MustCompile(`^ {0,3}(=+|-+)\s*$`)
)

func renderBlocks(b *strings.Builder, lines []string, depth int) {
	if depth > 30 {
		b.WriteString("<p>" + inlineMarkdown(strings.Join(lines, " ")) + "</p>")
		return
	}
	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case strings.TrimSpace(line) == "":
			i++
		case mdFence.MatchString(line):
			fence := mdFence.FindStringSubmatch(line)[1]
			var code []string
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimLeft(lines[i], " "), fence) {
				code = append(code, lines[i])
				i++
			}
			i++ // the closing fence
			b.WriteString("<pre>" + html.EscapeString(strings.Join(code, "\n")) + "</pre>")
		case mdHeading.MatchString(line):
			m := mdHeading.FindStringSubmatch(line)
			n := strconv.Itoa(len(m[1]))
			b.WriteString("<h" + n + ">" + inlineMarkdown(m[2]) + "</h" + n + ">")
			i++
		case mdRule.MatchString(line):
			b.WriteString("<hr>")
			i++
		case mdQuoteLine.MatchString(line):
			var inner []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && mdQuoteLine.MatchString(lines[i]) {
				inner = append(inner, mdQuoteLine.FindStringSubmatch(lines[i])[1])
				i++
			}
			b.WriteString("<blockquote>")
			renderBlocks(b, inner, depth+1)
			b.WriteString("</blockquote>")
		case mdBullet.MatchString(line) || mdOrdered.MatchString(line):
			i = renderList(b, lines, i, depth)
		case strings.HasPrefix(line, "    "):
			var code []string
			for i < len(lines) && (strings.HasPrefix(lines[i], "    ") || strings.TrimSpace(lines[i]) == "") {
				code = append(code, strings.TrimPrefix(lines[i], "    "))
				i++
			}
			for len(code) > 0 && strings.TrimSpace(code[len(code)-1]) == "" {
				code = code[:len(code)-1]
			}
			b.WriteString("<pre>" + html.EscapeString(strings.Join(code, "\n")) + "</pre>")
		case strings.Contains(line, "|") && i+1 < len(lines) && mdTableSep.MatchString(lines[i+1]):
			var rows []string
			for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				rows = append(rows, strings.TrimSpace(lines[i]))
				i++
			}
			writeTable(b, rows)
		default:
			var para []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !startsBlock(lines, i) &&
				!(len(para) > 0 && mdSetext.MatchString(lines[i])) {
				para = append(para, lines[i])
				i++
			}
			if len(para) == 0 { // a line startsBlock took but no case did
				para, i = []string{lines[i]}, i+1
			}
			if i < len(lines) && len(para) > 0 && mdSetext.MatchString(lines[i]) {
				// Text underlined with === or --- is a heading.
				level := "2"
				if strings.Contains(lines[i], "=") {
					level = "1"
				}
				b.WriteString("<h" + level + ">" + inlineMarkdown(strings.Join(para, " ")) + "</h" + level + ">")
				i++
				continue
			}
			b.WriteString("<p>" + paragraphHTML(para) + "</p>")
		}
	}
}

// writeTable writes a pipe table: rows[0] is the header, rows[1] the
// --- line under it, the rest its body. A row with fewer cells than the
// header is filled out, one with more is cut to it.
func writeTable(b *strings.Builder, rows []string) {
	head := tableCells(rows[0])
	b.WriteString("<table><tr>")
	for _, c := range head {
		b.WriteString("<th>" + inlineMarkdown(c) + "</th>")
	}
	b.WriteString("</tr>")
	for _, r := range rows[2:] {
		cells := tableCells(r)
		b.WriteString("<tr>")
		for j := range head {
			c := ""
			if j < len(cells) {
				c = cells[j]
			}
			b.WriteString("<td>" + inlineMarkdown(c) + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
}

// tableCells splits a table row at its pipes — not an escaped one (\|),
// which stays in the cell — without the pipes at either end.
func tableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	if strings.HasSuffix(row, "|") && !strings.HasSuffix(row, `\|`) {
		row = row[:len(row)-1]
	}
	var cells []string
	start := 0
	for i := 0; i < len(row); i++ {
		switch row[i] {
		case '\\':
			i++
		case '|':
			cells = append(cells, strings.TrimSpace(row[start:i]))
			start = i + 1
		}
	}
	return append(cells, strings.TrimSpace(row[start:]))
}

// startsBlock reports whether lines[i] begins a block other than a
// paragraph (so the paragraph before it ends).
func startsBlock(lines []string, i int) bool {
	l := lines[i]
	return mdFence.MatchString(l) || mdHeading.MatchString(l) || mdQuoteLine.MatchString(l) ||
		mdBullet.MatchString(l) || mdOrdered.MatchString(l) ||
		(mdRule.MatchString(l) && !mdSetext.MatchString(l)) ||
		(strings.Contains(l, "|") && i+1 < len(lines) && mdTableSep.MatchString(lines[i+1]))
}

// paragraphHTML joins a paragraph's lines: a line ending in two spaces or a
// backslash breaks there, the others run on.
func paragraphHTML(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		hard := strings.HasSuffix(l, "  ") || strings.HasSuffix(l, `\`)
		l = strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(l, " "), `\`))
		b.WriteString(inlineMarkdown(l))
		if i < len(lines)-1 {
			if hard {
				b.WriteString("<br>")
			} else {
				b.WriteString(" ")
			}
		}
	}
	return b.String()
}

// renderList renders the list starting at lines[i] and returns the index
// after it. Items are the lines at the list's indent that carry a marker;
// what is indented under one belongs to it (its other paragraphs, a list
// inside it).
func renderList(b *strings.Builder, lines []string, i, depth int) int {
	marker := func(l string) (indent int, ordered bool, num int, rest string, ok bool) {
		if m := mdBullet.FindStringSubmatch(l); m != nil {
			return len(m[1]), false, 0, m[3], true
		}
		if m := mdOrdered.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			return len(m[1]), true, n, m[3], true
		}
		return 0, false, 0, "", false
	}
	indent, ordered, start, _, _ := marker(lines[i])
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	if ordered && start != 1 {
		b.WriteString(`<ol start="` + strconv.Itoa(start) + `">`)
	} else {
		b.WriteString("<" + tag + ">")
	}
	for i < len(lines) {
		ind, ord, _, rest, ok := marker(lines[i])
		if !ok || ind != indent || ord != ordered {
			break
		}
		item := []string{rest}
		i++
		// The item's continuation: lines indented deeper than its marker,
		// and blank lines between them.
		for i < len(lines) {
			l := lines[i]
			if strings.TrimSpace(l) == "" {
				if i+1 < len(lines) && leadingSpaces(lines[i+1]) > indent {
					item = append(item, "")
					i++
					continue
				}
				break
			}
			if leadingSpaces(l) <= indent {
				if _, _, _, _, isItem := marker(l); isItem || startsBlock(lines, i) {
					break
				}
				item = append(item, strings.TrimSpace(l)) // a lazy continuation
				i++
				continue
			}
			item = append(item, dedent(l, indent+2))
			i++
		}
		b.WriteString("<li>")
		if len(item) == 1 || !hasBlock(item) {
			b.WriteString(paragraphHTML(item))
		} else {
			var inner strings.Builder
			renderBlocks(&inner, item, depth+1)
			// A tight item's first paragraph is its text, not a <p>.
			s := inner.String()
			if strings.HasPrefix(s, "<p>") {
				if end := strings.Index(s, "</p>"); end > 0 {
					s = s[3:end] + s[end+4:]
				}
			}
			b.WriteString(s)
		}
		b.WriteString("</li>")
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			if i+1 < len(lines) {
				if ind, ord, _, _, ok := marker(lines[i+1]); ok && ind == indent && ord == ordered {
					i++ // a blank line between items
					continue
				}
			}
			break
		}
	}
	b.WriteString("</" + tag + ">")
	return i
}

func hasBlock(item []string) bool {
	for i := 1; i < len(item); i++ {
		if strings.TrimSpace(item[i]) == "" || startsBlock(item, i) {
			return true
		}
	}
	return false
}

func leadingSpaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func dedent(s string, n int) string {
	if k := leadingSpaces(s); k < n {
		n = k
	}
	return s[n:]
}

var (
	mdAutolink = regexp.MustCompile(`^<((?:https?|mailto|ftp):[^\s<>]+)>`)
	mdLinkDest = regexp.MustCompile(`^\(\s*(<[^>]*>|[^\s()]*(?:\([^\s()]*\)[^\s()]*)*)(?:\s+"[^"]*")?\s*\)`)
)

// inlineMarkdown renders a line's inline Markdown: code spans, emphasis,
// strikethrough, links, images, autolinks and backslash escapes. Text is
// escaped; HTML in it stays text.
func inlineMarkdown(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && strings.IndexByte("\\`*_{}[]()#+-.!|~<>", s[i+1]) >= 0:
			b.WriteString(html.EscapeString(s[i+1 : i+2]))
			i += 2
		case c == '`':
			n := 0
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			fence := s[i : i+n]
			if end := strings.Index(s[i+n:], fence); end >= 0 {
				code := strings.TrimSpace(s[i+n : i+n+end])
				b.WriteString("<code>" + html.EscapeString(code) + "</code>")
				i += n + end + n
			} else {
				b.WriteString(html.EscapeString(fence))
				i += n
			}
		case c == '<' && mdAutolink.MatchString(s[i:]):
			m := mdAutolink.FindStringSubmatch(s[i:])
			text := strings.TrimPrefix(m[1], "mailto:")
			b.WriteString(`<a href="` + html.EscapeString(m[1]) + `">` + html.EscapeString(text) + "</a>")
			i += len(m[0])
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			if text, dest, n, ok := linkAt(s[i+1:]); ok {
				b.WriteString(`<img src="` + html.EscapeString(dest) + `" alt="` + html.EscapeString(unescapeMarkdown(text)) + `">`)
				i += 1 + n
				continue
			}
			b.WriteString("!")
			i++
		case c == '[':
			if text, dest, n, ok := linkAt(s[i:]); ok {
				b.WriteString(`<a href="` + html.EscapeString(dest) + `">` + inlineMarkdown(text) + "</a>")
				i += n
				continue
			}
			b.WriteString("[")
			i++
		case c == '*' || c == '_' || c == '~':
			if out, n, ok := emphasisAt(s, i); ok {
				b.WriteString(out)
				i += n
				continue
			}
			b.WriteByte(c)
			i++
		default:
			j := i + 1
			for j < len(s) && strings.IndexByte("\\`<![*_~", s[j]) < 0 {
				j++
			}
			b.WriteString(html.EscapeString(s[i:j]))
			i = j
		}
	}
	return b.String()
}

// linkAt reads [text](dest) at the start of s.
func linkAt(s string) (text, dest string, n int, ok bool) {
	depth := 0
	for j := 0; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				m := mdLinkDest.FindStringSubmatch(s[j+1:])
				if m == nil {
					return "", "", 0, false
				}
				dest = strings.Trim(m[1], "<>")
				low := strings.ToLower(dest)
				if strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "vbscript:") {
					dest = ""
				}
				return s[1:j], dest, j + 1 + len(m[0]), true
			}
		}
	}
	return "", "", 0, false
}

// emphasisAt reads **strong**, *em*, __strong__, _em_ or ~~del~~ at s[i].
func emphasisAt(s string, i int) (string, int, bool) {
	for _, d := range []struct{ mark, open, close string }{
		{"**", "<b>", "</b>"}, {"__", "<b>", "</b>"}, {"~~", "<s>", "</s>"},
		{"*", "<i>", "</i>"}, {"_", "<i>", "</i>"},
	} {
		if !strings.HasPrefix(s[i:], d.mark) {
			continue
		}
		rest := s[i+len(d.mark):]
		if rest == "" || rest[0] == ' ' {
			continue // a lone star or underscore is text
		}
		if d.mark[0] == '_' && i > 0 && isWordByte(s[i-1]) {
			continue // snake_case
		}
		end := closingMark(rest, d.mark)
		if end <= 0 {
			continue
		}
		after := i + len(d.mark) + end + len(d.mark)
		if d.mark[0] == '_' && after < len(s) && isWordByte(s[after]) {
			continue
		}
		return d.open + inlineMarkdown(rest[:end]) + d.close, after - i, true
	}
	return "", 0, false
}

// closingMark finds mark closing an emphasis that opened just before s:
// not after a space, not inside a code span.
func closingMark(s, mark string) int {
	for j := 0; j+len(mark) <= len(s); j++ {
		switch {
		case s[j] == '\\':
			j++
		case s[j] == '`':
			if end := strings.IndexByte(s[j+1:], '`'); end >= 0 {
				j += end + 1
			}
		case strings.HasPrefix(s[j:], mark) && j > 0 && s[j-1] != ' ':
			if len(mark) == 1 && j+1 < len(s) && s[j+1] == mark[0] {
				j++ // part of a double mark: not this one's end
				continue
			}
			return j
		}
	}
	return -1
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
