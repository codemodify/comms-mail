package mailcore

import (
	"strings"

	"golang.org/x/net/html"
)

// htmlTextVersion is HTMLToText's version. A message whose only text is
// HTML keeps the text HTMLToText made of it, and the version that made it
// (Message.HTMLText); text made by an older version is made again when the
// message is opened. Raise it whenever HTMLToText's output changes.
const htmlTextVersion = 1

// HTMLToText is the text of an HTML part, for a message that came with no
// plain-text part: what a reader sees, without the markup, as Thunderbird's
// plain-text view shows it.
//
//   - Nothing that is not shown: comments — Outlook's conditional blocks
//     and commented-out markup with them — the head, the title, style,
//     script, noscript, template and Office's <xml>.
//   - Every entity decoded (&zwnj; as much as &amp;), and the invisible
//     characters a sender repeats to pad the inbox's preview dropped; the
//     joiner inside an emoji sequence is kept.
//   - A picture as its description (alt), where it has one.
//   - White space collapsed as a browser collapses it, except in <pre>.
//   - A line for a block (div, tr, li, br…), a blank line for a paragraph
//     (p, a heading, a list, a quotation), a table row's cells on one line,
//     and never more than one blank line in a row.
func HTMLToText(src string) string {
	var w textWriter
	z := html.NewTokenizer(strings.NewReader(src))
	hidden, pre, inHead := 0, 0, false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return w.String()
		case html.TextToken:
			if hidden == 0 && !inHead {
				w.text(string(z.Text()), pre > 0)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			raw, hasAttr := z.TagName()
			name := string(raw)
			switch {
			case name == "head":
				inHead = tt == html.StartTagToken
			case name == "body":
				inHead = false // a head that was never closed
			case notShown[name]:
				if tt == html.StartTagToken {
					hidden++
				}
			case hidden == 0 && !inHead:
				w.open(name, tt == html.SelfClosingTagToken)
				if name == "pre" && tt == html.StartTagToken {
					pre++
				}
				if name == "img" && hasAttr {
					// A picture's description is its words: "Home Depot
					// Pro" for a logo. A tracking pixel has none.
					if alt := tagAttr(z, "alt"); strings.TrimSpace(alt) != "" {
						w.text(" "+alt+" ", false)
					}
				}
			}
		case html.EndTagToken:
			raw, _ := z.TagName()
			name := string(raw)
			switch {
			case name == "head":
				inHead = false
			case notShown[name]:
				if hidden > 0 {
					hidden--
				}
			case hidden == 0 && !inHead:
				w.close(name)
				if name == "pre" && pre > 0 {
					pre--
				}
			}
		}
	}
}

// tagAttr is the value of the tokenizer's current tag's attribute key, or "".
func tagAttr(z *html.Tokenizer, key string) string {
	for {
		k, v, more := z.TagAttr()
		if string(k) == key {
			return string(v)
		}
		if !more {
			return ""
		}
	}
}

// notShown are the elements whose content a reader never sees.
var notShown = map[string]bool{
	"title": true, "style": true, "script": true, "noscript": true,
	"template": true, "xml": true, "svg": true, "object": true,
	"iframe": true, "select": true,
}

// lineTags start and end a line; paraTags a paragraph, with a blank line
// before and after.
var lineTags = map[string]bool{
	"div": true, "tr": true, "li": true, "dt": true, "dd": true,
	"table": true, "caption": true, "section": true, "article": true,
	"header": true, "footer": true, "nav": true, "aside": true, "main": true,
	"center": true, "address": true, "form": true, "fieldset": true,
	"figure": true, "figcaption": true, "details": true, "summary": true,
	"hr": true,
}

var paraTags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "blockquote": true, "ul": true, "ol": true, "dl": true,
	"pre": true,
}

// textWriter lays text out as it comes, owing the breaks and spaces it is
// asked for until there is text to put after them, so neither a run of
// empty blocks nor trailing white space ever reaches the page.
type textWriter struct {
	b       strings.Builder
	breaks  int  // newlines owed: 1 a line, 2 a blank line
	space   bool // a collapsed space owed on this line
	gap     bool // a table cell's gap owed on this line
	bullet  bool // a list item's "- " owed
	inLine  bool // the current line has text
	written bool // anything at all has been written
}

func (w *textWriter) open(name string, selfClosing bool) {
	switch {
	case name == "br":
		w.breaks = min(w.breaks+1, 2)
		w.space, w.gap = false, false
	case name == "td" || name == "th":
		if w.inLine && w.breaks == 0 {
			w.gap, w.space = true, false
		}
	case paraTags[name]:
		w.owe(2)
	case lineTags[name]:
		w.owe(1)
		if name == "li" && !selfClosing {
			w.bullet = true
		}
	}
}

func (w *textWriter) close(name string) {
	switch {
	case paraTags[name]:
		w.owe(2)
	case lineTags[name]:
		w.owe(1)
	}
}

func (w *textWriter) owe(n int) {
	w.breaks = max(w.breaks, n)
	w.space, w.gap = false, false
}

func (w *textWriter) text(s string, pre bool) {
	if pre {
		for _, r := range s {
			if r == '\r' {
				continue
			}
			if r == '\n' {
				w.breaks = min(w.breaks+1, 2)
				continue
			}
			if invisible(r) {
				continue
			}
			w.put(r)
		}
		return
	}
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\u00a0':
			if w.inLine && w.breaks == 0 && !w.gap {
				w.space = true
			}
		case r == '\u200d':
			// The joiner of an emoji sequence (a family, a profession)
			// belongs between two characters; on its own it is padding.
			if w.inLine && !w.space && !w.gap && w.breaks == 0 {
				w.b.WriteRune(r)
			}
		case invisible(r):
		default:
			w.put(r)
		}
	}
}

// put writes r, with whatever is owed before it.
func (w *textWriter) put(r rune) {
	if w.breaks > 0 {
		if w.written {
			w.b.WriteString("\n\n"[:w.breaks])
		}
		w.breaks, w.inLine, w.space, w.gap = 0, false, false, false
	}
	switch {
	case w.gap:
		w.b.WriteString("  ")
	case w.space:
		w.b.WriteByte(' ')
	}
	w.space, w.gap = false, false
	if w.bullet {
		w.b.WriteString("- ")
		w.bullet = false
	}
	w.b.WriteRune(r)
	w.inLine, w.written = true, true
}

func (w *textWriter) String() string { return w.b.String() }

// invisible reports the characters that draw nothing and that senders
// repeat to pad the preview an inbox shows of a message: the zero-width
// space and non-joiner, the combining grapheme joiner, the word joiner and
// the invisible operators, the byte-order mark, the soft hyphen, the
// Mongolian vowel separator and the Hangul fillers. The zero-width joiner
// is not among them: it holds an emoji sequence together.
func invisible(r rune) bool {
	switch r {
	case '\u034f', '\u200b', '\u200c', '\u2060', '\u2061', '\u2062', '\u2063', '\u2064',
		'\ufeff', '\u00ad', '\u180e', '\u115f', '\u1160', '\u3164', '\uffa0':
		return true
	}
	return false
}
