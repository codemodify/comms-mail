package mailcore

import (
	"strings"
	"testing"
	"time"
)

func TestHTMLToMarkdown(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"headings and emphasis": {
			`<h1>Release <em>notes</em></h1><p>This is <b>bold</b>, <i>italic</i> and <code>code()</code>.</p>`,
			"# Release *notes*\n\nThis is **bold**, *italic* and `code()`.",
		},
		"links": {
			`<p>See <a href="https://example.com/a b">the docs</a>, <a href="https://example.com">https://example.com</a>, <a href="mailto:ada@example.com">ada@example.com</a> and <a href="javascript:x()">this</a>.</p>`,
			"See [the docs](<https://example.com/a b>), <https://example.com>, <mailto:ada@example.com> and this.",
		},
		"lists, nested": {
			`<ul><li>One</li><li>Two<ul><li>Two a</li></ul></li></ul><ol start="3"><li>Third</li><li>Fourth</li></ol>`,
			"- One\n- Two\n  - Two a\n\n3. Third\n4. Fourth",
		},
		"quote and code block": {
			"<blockquote><p>Quoted line</p><p>Second</p></blockquote><pre>if a &lt; b {\n  go()\n}</pre>",
			"> Quoted line\n>\n> Second\n\n```\nif a < b {\n  go()\n}\n```",
		},
		"line breaks and entities": {
			`<p>Ada Lovelace<br>Analytical Engine Ltd &amp; Co.<br/>London</p>`,
			"Ada Lovelace  \nAnalytical Engine Ltd & Co.  \nLondon",
		},
		"data table": {
			`<table><tr><th>Item</th><th>Price</th></tr><tr><td>Tea</td><td>£3</td></tr><tr><td>Cake | slice</td><td>£4</td></tr></table>`,
			"| Item          | Price |\n| ------------- | ----- |\n| Tea           | £3    |\n| Cake \\| slice | £4    |",
		},
		"layout table": {
			`<table><tr><td><table><tr><td><img src="https://cdn.example.com/logo.png" alt="Logo"></td></tr></table></td></tr><tr><td><p>Hello there.</p><p>Second paragraph.</p></td></tr></table>`,
			"![Logo](https://cdn.example.com/logo.png)\n\nHello there.\n\nSecond paragraph.",
		},
		"images": {
			`<p><img src="cid:logo@x" alt="Logo"> <img src="data:image/png;base64,AAAA" alt="Chart"> <img src="https://t.example.com/p.gif" width="1" height="1"></p>`,
			"![Logo](cid:logo@x) [image: Chart]",
		},
		"nothing to read": {
			`<html><head><title>T</title><style>p{color:red}</style><script>alert(1)</script></head><body><p>Body</p></body></html>`,
			"Body",
		},
		"text that looks like markup": {
			`<p>* not a list</p><p># not a heading</p><p>1. not a list either</p><p>snake_case and _edge_ and [brackets]</p>`,
			"\\* not a list\n\n\\# not a heading\n\n1\\. not a list either\n\nsnake_case and \\_edge\\_ and \\[brackets\\]",
		},
		"white space": {
			"<div>\n  Hello\n  <span>big</span>\n  world\n</div>",
			"Hello big world",
		},
	} {
		if got := HTMLToMarkdown(tc.in); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", name, got, tc.want)
		}
	}
}

func TestMessageMarkdown(t *testing.T) {
	m := Message{
		Subject: "Quarterly *report*", From: "Ada <ada@example.com>", To: "bob@example.org",
		Date: time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC),
		HTML: "<p>Hi <b>Bob</b>,</p><p>Figures attached.</p>", Attachments: []string{"q3_report.pdf"},
	}
	want := "# Quarterly \\*report\\*\n\n" +
		"**From:** Ada <ada@example.com>  \n**To:** bob@example.org  \n**Date:** Mon, 28 Sep 2026 09:30 UTC  \n\n---\n\n" +
		"Hi **Bob**,\n\nFigures attached.\n\n---\n\n**Attachments:** q3_report.pdf\n"
	if got := MessageMarkdown(m); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// A text message is Markdown as it is.
	plain := MessageMarkdown(Message{Subject: "s", Body: "Line one\n> quoted\n\n- a list"})
	if !strings.HasSuffix(plain, "---\n\nLine one\n> quoted\n\n- a list\n") {
		t.Fatalf("plain:\n%s", plain)
	}
}
