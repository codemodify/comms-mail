package mailcore

import (
	"strings"
	"testing"
)

func TestMarkdownToHTML(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"headings":             {"# One\n## Two ##\nThree\n=====", "<h1>One</h1><h2>Two</h2><h1>Three</h1>"},
		"paragraph and breaks": {"Ada Lovelace  \nLondon\nEngland\n\nNext", "<p>Ada Lovelace<br>London England</p><p>Next</p>"},
		"emphasis": {"**bold**, *em*, __b__, _e_, ~~gone~~ and snake_case_name",
			"<p><b>bold</b>, <i>em</i>, <b>b</b>, <i>e</i>, <s>gone</s> and snake_case_name</p>"},
		"code":  {"Use `a < b` here", "<p>Use <code>a &lt; b</code> here</p>"},
		"fence": {"```\nif a < b {\n  go()\n}\n```", "<pre>if a &lt; b {\n  go()\n}</pre>"},
		"links": {"[docs](https://example.com/a) and <https://example.com> and [bad](javascript:alert(1))",
			`<p><a href="https://example.com/a">docs</a> and <a href="https://example.com">https://example.com</a> and <a href="">bad</a></p>`},
		"images": {"![Logo](cid:logo@x) ![banner](https://i.example/b.png)",
			`<p><img src="cid:logo@x" alt="Logo"> <img src="https://i.example/b.png" alt="banner"></p>`},
		"nested lists": {"- One\n- Two\n  - Two a\n\n3. Third\n4. Fourth",
			`<ul><li>One</li><li>Two<ul><li>Two a</li></ul></li></ul><ol start="3"><li>Third</li><li>Fourth</li></ol>`},
		"quote":            {"> Quoted line\n>\n> Second", "<blockquote><p>Quoted line</p><p>Second</p></blockquote>"},
		"rule":             {"a\n\n---\n\nb", "<p>a</p><hr><p>b</p>"},
		"table":            {"| Item | Price |\n| ---- | ----- |\n| Tea  | £3    |", "<pre>| Item | Price |\n| ---- | ----- |\n| Tea  | £3    |</pre>"},
		"raw html is text": {"<script>alert(1)</script> & co", "<p>&lt;script&gt;alert(1)&lt;/script&gt; &amp; co</p>"},
		"escapes":          {`\*not em\* and 1\. not a list`, "<p>*not em* and 1. not a list</p>"},
	} {
		if got := MarkdownToHTML(tc.in); got != tc.want {
			t.Errorf("%s:\ngot  %s\nwant %s", name, got, tc.want)
		}
	}
}

// What HTMLToMarkdown writes renders back to the same structure.
func TestMarkdownRoundTrip(t *testing.T) {
	src := `<h2>Weekly</h2><p>This is <b>bold</b> and <a href="https://example.com">a link</a>.</p><ul><li>One</li><li>Two</li></ul><blockquote><p>Quoted</p></blockquote>`
	got := MarkdownToHTML(HTMLToMarkdown(src))
	for _, want := range []string{"<h2>Weekly</h2>", "<b>bold</b>", `<a href="https://example.com">a link</a>`, "<ul><li>One</li><li>Two</li></ul>", "<blockquote><p>Quoted</p></blockquote>"} {
		if !strings.Contains(got, want) {
			t.Errorf("round trip lacks %q:\n%s", want, got)
		}
	}
}
