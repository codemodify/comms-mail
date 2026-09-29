package mailui

import (
	"encoding/base64"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/widgets"
)

// Print and Save As. There is no print path in the toolkit, so Print writes
// the message as a page — its headers, then its HTML (inline images
// embedded) or its text — and opens it in the browser, which prints it or
// saves it as PDF. The page may not load anything: a Content-Security-Policy
// forbids every request and every script, so a remote image or style in
// the message cannot tell the sender it was printed. Save As writes the
// message exactly as stored, as .eml.

// printCSP allows nothing but the page itself, its inline styles and its
// embedded (data:) images.
// printPattern names the pages handed to the browser to print.
const printPattern = "comms-mail-print-*.html"

const printCSP = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'"

var remoteSrcRe = regexp.MustCompile(`(?i)\s(src|srcset|background)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)

// printablePage is the page Print opens: headers, then the body. images
// are the message's inline images by Content-ID.
func printablePage(m mailcore.Message, images []mailcore.InlineImage) string {
	var b strings.Builder
	esc := html.EscapeString
	fmt.Fprintf(&b, "<!doctype html>\n<html><head><meta charset=\"utf-8\">\n<meta http-equiv=\"Content-Security-Policy\" content=\"%s\">\n", printCSP)
	fmt.Fprintf(&b, "<title>%s</title>\n", esc(m.Subject))
	b.WriteString(`<style>
body{font-family:sans-serif;font-size:11pt;max-width:48em;margin:1.5em auto;padding:0 1em}
table.h{border-collapse:collapse;margin-bottom:.5em} table.h td{padding:1px 10px 1px 0;vertical-align:top}
table.h td:first-child{color:#555;white-space:nowrap} h1{font-size:14pt;margin:0 0 .4em}
pre{white-space:pre-wrap;font-family:inherit} hr{border:0;border-top:1px solid #aaa}
</style></head><body>
`)
	fmt.Fprintf(&b, "<h1>%s</h1>\n<table class=\"h\">\n", esc(m.Subject))
	for _, row := range [][2]string{{"From", m.From}, {"To", m.To}, {"Cc", m.Cc}, {"Date", m.Date.Format("Monday, 2 January 2006 15:04 MST")}} {
		if strings.TrimSpace(row[1]) != "" {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>\n", row[0], esc(row[1]))
		}
	}
	if len(m.Attachments) > 0 {
		fmt.Fprintf(&b, "<tr><td>Attachments</td><td>%s</td></tr>\n", esc(strings.Join(m.Attachments, ", ")))
	}
	b.WriteString("</table><hr>\n")
	if strings.TrimSpace(m.HTML) != "" {
		b.WriteString(printableHTML(m.HTML, images))
	} else {
		fmt.Fprintf(&b, "<pre>%s</pre>\n", esc(mailcore.DisplayBody(m)))
	}
	b.WriteString("\n</body></html>\n")
	return b.String()
}

// printableHTML is the message's (already sanitised) HTML with its inline
// images embedded and every other image source taken out — the page's
// policy would block them anyway; without the attribute the browser does
// not even try.
func printableHTML(body string, images []mailcore.InlineImage) string {
	byCID := map[string]mailcore.InlineImage{}
	for _, im := range images {
		byCID[strings.ToLower(im.CID)] = im
	}
	return remoteSrcRe.ReplaceAllStringFunc(body, func(attr string) string {
		m := remoteSrcRe.FindStringSubmatch(attr)
		name, val := strings.ToLower(m[1]), strings.Trim(m[2], `"'`)
		val = html.UnescapeString(val)
		if name == "src" {
			if rest, ok := cutFold(strings.TrimSpace(val), "cid:"); ok {
				if im, ok := byCID[strings.ToLower(strings.Trim(rest, "<>"))]; ok {
					return fmt.Sprintf(` src="data:%s;base64,%s"`, im.MIME, base64.StdEncoding.EncodeToString(im.Data))
				}
			}
			if strings.HasPrefix(strings.TrimSpace(val), "data:") {
				return attr
			}
		}
		return ""
	})
}

// printMessage opens the primary message as a page to print.
func (s *session) printMessage() {
	s.withFull("Print", func(m mailcore.Message) {
		// The page the browser prints is removed once old, the next time
		// one is made (the browser reads it when it gets round to it).
		id := m.ID
		s.async(func() (any, error) {
			var imgs []mailcore.InlineImage
			if strings.Contains(strings.ToLower(m.HTML), "cid:") {
				imgs, _ = s.cli.InlineImages(id)
			}
			mailcore.RemoveOld(filepath.Join(os.TempDir(), printPattern), mailcore.PrintedKeep)
			f, err := os.CreateTemp("", printPattern)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			if _, err := f.WriteString(printablePage(m, imgs)); err != nil {
				return nil, err
			}
			if !mailcore.OpenWithDesktop(f.Name()) {
				return f.Name(), fmt.Errorf("no browser to open it with — the page is %s", f.Name())
			}
			return f.Name(), nil
		}, func(_ any, err error) {
			if err != nil {
				widgets.Warn(s.win.Content(), "Print", err.Error(), nil)
				return
			}
			s.mark("Opened in your browser — print it, or save it as PDF, from there")
		})
	})
}

// saveMessageAs writes the primary message, exactly as stored, to a .eml.
func (s *session) saveMessageAs() {
	m, ok := s.primary()
	if !ok {
		s.mark("No message")
		return
	}
	home, _ := os.UserHomeDir()
	widgets.ShowFileDialog(s.win.Content(), widgets.FileDialogOptions{
		Title:      "Save message as",
		Mode:       widgets.FileSave,
		Path:       home,
		Name:       emlFileName(m),
		OnNavigate: mailDirEntries,
		OnPick: func(path string) {
			if strings.TrimSpace(path) == "" {
				return
			}
			if filepath.Ext(path) == "" {
				path += ".eml"
			}
			id := m.ID
			s.async(func() (any, error) {
				raw, err := s.cli.Raw(id)
				if err != nil {
					return nil, err
				}
				return nil, os.WriteFile(path, raw, 0o600)
			}, func(_ any, err error) {
				if err != nil {
					widgets.Warn(s.win.Content(), "Save As", err.Error(), nil)
					return
				}
				s.mark("Saved " + path)
			})
		},
	})
}

var unsafeFileChars = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]+`)

// emlFileName is a file name for m: its subject, made safe, and .eml.
func emlFileName(m mailcore.Message) string {
	name := strings.Join(strings.Fields(unsafeFileChars.ReplaceAllString(m.Subject, " ")), " ")
	name = strings.Trim(name, ". ")
	if r := []rune(name); len(r) > 80 {
		name = strings.TrimSpace(string(r[:80]))
	}
	if name == "" {
		name = "message"
	}
	return name + ".eml"
}
