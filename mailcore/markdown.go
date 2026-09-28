package mailcore

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A message as Markdown text: the reading pane's Markdown view, to read
// with the formatting written out or to copy into notes. HTML mail is
// converted — headings, emphasis, links, images, lists, quotes, code and
// data tables; the tables newsletters use for layout become plain blocks —
// and text mail is shown as it is, which is Markdown already. Nothing is
// fetched: an image is its address, as written.

// MessageMarkdown is m as a Markdown document: its subject as the title,
// the people and date, the body, and the names of its attachments.
func MessageMarkdown(m Message) string {
	var b strings.Builder
	subject := strings.TrimSpace(m.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	b.WriteString("# " + escapeMarkdownLine(escapeMarkdown(subject)) + "\n\n")
	field := func(name, v string) {
		if v = strings.TrimSpace(v); v != "" {
			b.WriteString("**" + name + ":** " + escapeMarkdown(v) + "  \n")
		}
	}
	field("From", m.From)
	field("To", m.To)
	field("Cc", m.Cc)
	if !m.Date.IsZero() {
		field("Date", m.Date.Format("Mon, 2 Jan 2006 15:04 MST"))
	}
	b.WriteString("\n---\n\n")
	body := ""
	if strings.TrimSpace(m.HTML) != "" {
		body = HTMLToMarkdown(m.HTML)
	} else if m.Body != "" && looksLikeHTML(m.Body) {
		body = HTMLToMarkdown(m.Body)
	} else {
		body = strings.TrimSpace(strings.ReplaceAll(m.Body, "\r\n", "\n"))
	}
	b.WriteString(body)
	if len(m.Attachments) > 0 {
		names := make([]string, len(m.Attachments))
		for i, a := range m.Attachments {
			names[i] = escapeMarkdown(a)
		}
		b.WriteString("\n\n---\n\n**Attachments:** " + strings.Join(names, ", "))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// HTMLToMarkdown converts an HTML document or fragment to Markdown.
func HTMLToMarkdown(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return strings.TrimSpace(HTMLToText(src))
	}
	c := &mdConv{}
	out := strings.Join(c.blocks(doc, 0), "\n\n")
	return strings.TrimSpace(tooManyBlankLines.ReplaceAllString(out, "\n\n"))
}

var tooManyBlankLines = regexp.MustCompile(`\n{3,}`)

// hardBreak stands for a <br> while inline text is put together, so the
// collapsing of white space does not take it out.
const hardBreak = "\x00br\x00"

type mdConv struct{}

// skipped are elements with nothing to read in them.
func skipped(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Head, atom.Script, atom.Style, atom.Title, atom.Meta, atom.Link, atom.Noscript,
		atom.Template, atom.Iframe, atom.Object, atom.Embed, atom.Svg, atom.Button, atom.Input,
		atom.Select, atom.Textarea:
		return true
	}
	return false
}

// blockLevel are the elements that stand as blocks of their own.
func blockLevel(n *html.Node) bool {
	switch n.DataAtom {
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Main,
		atom.Aside, atom.Nav, atom.Center, atom.Address, atom.Form, atom.Fieldset, atom.Figure,
		atom.Figcaption, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol,
		atom.Li, atom.Dl, atom.Dt, atom.Dd, atom.Blockquote, atom.Pre, atom.Hr, atom.Table,
		atom.Thead, atom.Tbody, atom.Tfoot, atom.Tr, atom.Td, atom.Th, atom.Caption, atom.Body,
		atom.Html:
		return true
	}
	return false
}

// blocks renders n's children as Markdown blocks: runs of inline content
// become paragraphs, block elements their own blocks.
func (c *mdConv) blocks(n *html.Node, depth int) []string {
	if depth > 60 {
		return nil
	}
	var out []string
	var para strings.Builder
	flush := func() {
		if p := finishParagraph(para.String()); p != "" {
			out = append(out, p)
		}
		para.Reset()
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		switch {
		case ch.Type == html.TextNode:
			para.WriteString(escapeMarkdown(collapseSpace(ch.Data)))
		case ch.Type == html.ElementNode && skipped(ch):
		case ch.Type == html.ElementNode && blockLevel(ch):
			flush()
			out = append(out, c.block(ch, depth+1)...)
		case ch.Type == html.ElementNode:
			para.WriteString(c.inline(ch, depth+1))
		case ch.Type == html.DocumentNode:
			out = append(out, c.blocks(ch, depth+1)...)
		}
	}
	flush()
	return out
}

// block renders one block element.
func (c *mdConv) block(n *html.Node, depth int) []string {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		text := strings.TrimSpace(strings.ReplaceAll(finishParagraph(c.inlineChildren(n, depth)), "  \n", " "))
		if text == "" {
			return nil
		}
		return []string{strings.Repeat("#", level) + " " + text}
	case atom.Hr:
		return []string{"---"}
	case atom.Pre:
		code := strings.Trim(textContent(n), "\n")
		if strings.TrimSpace(code) == "" {
			return nil
		}
		fence := "```"
		if strings.Contains(code, "```") {
			fence = "~~~"
		}
		return []string{fence + "\n" + code + "\n" + fence}
	case atom.Blockquote:
		inner := strings.Join(c.blocks(n, depth), "\n\n")
		if inner == "" {
			return nil
		}
		return []string{prefixLines(inner, "> ", "> ")}
	case atom.Ul, atom.Ol:
		return c.list(n, depth)
	case atom.Table:
		return c.table(n, depth)
	case atom.Dt:
		if t := finishParagraph(c.inlineChildren(n, depth)); t != "" {
			return []string{"**" + t + "**"}
		}
		return nil
	case atom.Li:
		// A list item outside a list: a list of one.
		inner := strings.Join(c.blocks(n, depth), "\n\n")
		if inner == "" {
			return nil
		}
		return []string{prefixLines(inner, "- ", "  ")}
	}
	return c.blocks(n, depth)
}

// list renders ul / ol: "- " or "1. " markers, the item's further lines
// (and lists inside it) indented under its text.
func (c *mdConv) list(n *html.Node, depth int) []string {
	ordered := n.DataAtom == atom.Ol
	num := 1
	if s, err := strconv.Atoi(attr(n, "start")); err == nil && ordered {
		num = s
	}
	var items []string
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode || skipped(ch) {
			continue
		}
		var parts []string
		if ch.DataAtom == atom.Li {
			parts = c.blocks(ch, depth+1)
		} else {
			parts = c.block(ch, depth+1) // a stray nested list
		}
		// A list inside an item sits right under its text.
		var inner string
		for i, p := range parts {
			if i > 0 {
				if listStart.MatchString(p) {
					inner += "\n"
				} else {
					inner += "\n\n"
				}
			}
			inner += p
		}
		if strings.TrimSpace(inner) == "" {
			continue
		}
		marker := "- "
		if ordered {
			marker = strconv.Itoa(num) + ". "
			num++
		}
		items = append(items, prefixLines(inner, marker, strings.Repeat(" ", len(marker))))
	}
	if len(items) == 0 {
		return nil
	}
	return []string{strings.Join(items, "\n")}
}

// table renders a data table as a Markdown table, and a layout table (the
// scaffolding of most HTML mail) as the blocks in its cells.
func (c *mdConv) table(n *html.Node, depth int) []string {
	var rows [][]*html.Node
	var walk func(x *html.Node)
	walk = func(x *html.Node) {
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			switch ch.DataAtom {
			case atom.Thead, atom.Tbody, atom.Tfoot:
				walk(ch)
			case atom.Tr:
				var cells []*html.Node
				for td := ch.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
						cells = append(cells, td)
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			}
		}
	}
	walk(n)
	if !dataTable(n, rows) {
		var out []string
		for _, row := range rows {
			for _, cell := range row {
				out = append(out, c.blocks(cell, depth+1)...)
			}
		}
		return out
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	text := make([][]string, len(rows))
	width := make([]int, cols)
	for i, r := range rows {
		text[i] = make([]string, cols)
		for j := 0; j < cols; j++ {
			if j < len(r) {
				t := finishParagraph(c.inlineChildren(r[j], depth+1))
				t = strings.ReplaceAll(strings.ReplaceAll(t, "  \n", " "), "\n", " ")
				text[i][j] = strings.ReplaceAll(t, "|", `\|`)
			}
			width[j] = max(width[j], 3, utf8.RuneCountInString(text[i][j]))
		}
	}
	line := func(cells []string) string {
		var b strings.Builder
		b.WriteString("|")
		for j, t := range cells {
			b.WriteString(" " + t + strings.Repeat(" ", width[j]-utf8.RuneCountInString(t)) + " |")
		}
		return b.String()
	}
	lines := []string{line(text[0])}
	sep := make([]string, cols)
	for j := range sep {
		sep[j] = strings.Repeat("-", width[j])
	}
	lines = append(lines, line(sep))
	for _, r := range text[1:] {
		lines = append(lines, line(r))
	}
	out := []string{strings.Join(lines, "\n")}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && ch.DataAtom == atom.Caption {
			if t := finishParagraph(c.inlineChildren(ch, depth+1)); t != "" {
				out = append([]string{"*" + t + "*"}, out...)
			}
		}
	}
	return out
}

// dataTable tells a table of data from one laying out a page: a data
// table has no table inside it, no blocks or images in its cells, short
// cells, and either header cells or at least two rows of two columns.
func dataTable(n *html.Node, rows [][]*html.Node) bool {
	if len(rows) == 0 {
		return false
	}
	headers, cols := false, 0
	for _, r := range rows {
		cols = max(cols, len(r))
		for _, cell := range r {
			if cell.DataAtom == atom.Th {
				headers = true
			}
			if hasDescendant(cell, func(x *html.Node) bool {
				switch x.DataAtom {
				case atom.Table, atom.Img, atom.P, atom.Div, atom.Ul, atom.Ol, atom.Blockquote, atom.Pre, atom.H1, atom.H2, atom.H3:
					return true
				}
				return false
			}) || utf8.RuneCountInString(strings.TrimSpace(textContent(cell))) > 200 {
				return false
			}
		}
	}
	if cols < 2 {
		return false
	}
	return headers || len(rows) >= 2
}

// inlineChildren renders n's children as inline text.
func (c *mdConv) inlineChildren(n *html.Node, depth int) string {
	var b strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		switch ch.Type {
		case html.TextNode:
			b.WriteString(escapeMarkdown(collapseSpace(ch.Data)))
		case html.ElementNode:
			if skipped(ch) {
				continue
			}
			if blockLevel(ch) {
				// A block inside inline text (a <div> in a link): a
				// space around it keeps the words apart.
				b.WriteString(" " + c.inlineChildren(ch, depth+1) + " ")
				continue
			}
			b.WriteString(c.inline(ch, depth+1))
		}
	}
	return b.String()
}

// inline renders one inline element.
func (c *mdConv) inline(n *html.Node, depth int) string {
	if depth > 60 {
		return ""
	}
	wrap := func(mark string) string {
		inner := c.inlineChildren(n, depth)
		core := strings.TrimSpace(inner)
		if core == "" || strings.Contains(core, hardBreak) {
			return inner
		}
		lead := inner[:len(inner)-len(strings.TrimLeft(inner, " "))]
		trail := inner[len(strings.TrimRight(inner, " ")):]
		return lead + mark + core + mark + trail
	}
	switch n.DataAtom {
	case atom.Br:
		return hardBreak
	case atom.Strong, atom.B:
		return wrap("**")
	case atom.Em, atom.I, atom.Cite:
		return wrap("*")
	case atom.S, atom.Strike, atom.Del:
		return wrap("~~")
	case atom.Code, atom.Kbd, atom.Tt, atom.Samp:
		t := collapseSpace(textContent(n))
		if strings.TrimSpace(t) == "" {
			return t
		}
		if strings.Contains(t, "`") {
			return "`` " + t + " ``"
		}
		return "`" + t + "`"
	case atom.Q:
		return "“" + c.inlineChildren(n, depth) + "”"
	case atom.Img:
		return imageMarkdown(n)
	case atom.A:
		return linkMarkdown(n, c.inlineChildren(n, depth))
	}
	return c.inlineChildren(n, depth)
}

func linkMarkdown(n *html.Node, text string) string {
	href := strings.TrimSpace(attr(n, "href"))
	label := strings.TrimSpace(text)
	low := strings.ToLower(href)
	switch {
	case href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "data:"):
		return text
	case label == "":
		return "<" + href + ">"
	}
	if unescapeMarkdown(label) == href || "mailto:"+unescapeMarkdown(label) == href {
		return "<" + href + ">"
	}
	dest := href
	if strings.ContainsAny(dest, " ()<>") {
		dest = "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(dest) + ">"
	}
	lead := text[:len(text)-len(strings.TrimLeft(text, " "))]
	trail := text[len(strings.TrimRight(text, " ")):]
	return lead + "[" + label + "](" + dest + ")" + trail
}

// imageMarkdown is an image as ![alt](address). A tracking pixel (1×1)
// is left out; an image embedded in the HTML (data:) is named, not
// spelled out.
func imageMarkdown(n *html.Node) string {
	w, h := attr(n, "width"), attr(n, "height")
	if w == "1" || h == "1" || w == "0" || h == "0" {
		return ""
	}
	alt := escapeMarkdown(collapseSpace(strings.TrimSpace(attr(n, "alt"))))
	src := strings.TrimSpace(attr(n, "src"))
	switch {
	case src == "":
		return alt
	case strings.HasPrefix(strings.ToLower(src), "data:"):
		if alt == "" {
			return "[image]"
		}
		return "[image: " + alt + "]"
	}
	if strings.ContainsAny(src, " ()<>") {
		src = "<" + src + ">"
	}
	return "![" + alt + "](" + src + ")"
}

// finishParagraph tidies inline text into a paragraph: runs of spaces
// become one, lines are trimmed, a <br> becomes a hard line break, and a
// line that would start a list, heading or quote is escaped.
func finishParagraph(s string) string {
	s = multiSpace.ReplaceAllString(s, " ")
	parts := strings.Split(s, hardBreak)
	var lines []string
	for _, p := range parts {
		lines = append(lines, escapeMarkdownLine(strings.TrimSpace(p)))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "  \n")
}

var multiSpace = regexp.MustCompile(`[ \t]+`)

var listStart = regexp.MustCompile(`^(- |\d+\. )`)

// collapseSpace folds HTML white space to single spaces, keeping one at
// either end where there was some, so words either side stay apart.
func collapseSpace(s string) string {
	if s == "" {
		return ""
	}
	words := strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
	if words == "" {
		return " "
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	if unicode.IsSpace(first) {
		words = " " + words
	}
	if unicode.IsSpace(last) {
		words += " "
	}
	return words
}

var mdEscaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "`", "\\`", "[", `\[`, "]", `\]`)

// escapeMarkdown escapes what would otherwise read as Markdown in text:
// backslashes, emphasis stars, backticks, brackets, and underscores at
// the edge of a word (inside one, snake_case, they are left alone).
func escapeMarkdown(s string) string {
	s = mdEscaper.Replace(s)
	if !strings.Contains(s, "_") {
		return s
	}
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if r == '_' {
			before := i > 0 && (unicode.IsLetter(rs[i-1]) || unicode.IsDigit(rs[i-1]))
			after := i+1 < len(rs) && (unicode.IsLetter(rs[i+1]) || unicode.IsDigit(rs[i+1]))
			if !(before && after) {
				b.WriteString(`\_`)
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

var lineStartMarker = regexp.MustCompile(`^(#{1,6}\s|>|[-+]\s|\d+[.)]\s|=+\s*$|-{3,}\s*$)`)

// escapeMarkdownLine keeps a line of text from reading as a heading, quote,
// list item or rule.
func escapeMarkdownLine(line string) string {
	if lineStartMarker.MatchString(line) {
		if m := regexp.MustCompile(`^\d+`).FindString(line); m != "" {
			return m + `\` + line[len(m):]
		}
		return `\` + line
	}
	return line
}

func unescapeMarkdown(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\*`, "*", "\\`", "`", `\[`, "[", `\]`, "]", `\_`, "_").Replace(s)
}

// prefixLines puts first before the first line of s and rest before the
// others (an empty line gets rest without its trailing space).
func prefixLines(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		p := rest
		if i == 0 {
			p = first
		}
		if l == "" {
			p = strings.TrimRight(p, " ")
		}
		lines[i] = p + l
	}
	return strings.Join(lines, "\n")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(x *html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		if x.Type == html.ElementNode && x.DataAtom == atom.Br {
			b.WriteString("\n")
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

func hasDescendant(n *html.Node, pred func(*html.Node) bool) bool {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && (pred(ch) || hasDescendant(ch, pred)) {
			return true
		}
	}
	return false
}
