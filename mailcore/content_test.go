package mailcore

import (
	"bytes"
	"net/mail"
	"slices"
	"testing"
)

// The links worth a word: one that runs code, a look-alike of a domain
// you write to, one that shows a domain and goes to another, one to an
// address, an international domain, a short link — worst first; and none
// for a link that goes where it says, or whose text is no domain.
func TestContentLinks(t *testing.T) {
	html := `<p><a href="https://www.paypal.com/x">paypal.com</a>
<a href="https://evil.example/login">www.paypal.com</a>
<a href="javascript:alert(1)">click</a>
<a href="http://203.0.113.9/a">Your invoice</a>
<a href="https://xn--pypal-4ve.com/">Pay</a>
<a href="https://bit.ly/abc">Read more</a>
<a href="https://bank-examp1e.com/">Sign in</a>
<a href="https://news.example.org/v2.1">Version 2.1 notes</a>
<a href="mailto:a@example.com">a@example.com</a>
<a href="#top">top</a></p>`
	r := CheckContent(html, "", nil, nil, map[string]bool{"bank-example.com": true})
	var kinds []string
	for _, l := range r.Links {
		kinds = append(kinds, l.Kind)
	}
	want := []string{LinkScript, LinkLookAlike, LinkElsewhere, LinkAddress, LinkIDN, LinkShortener}
	if !slices.Equal(kinds, want) || r.LinkCount != 8 {
		t.Fatalf("links %v (%d), want %v (8): %+v", kinds, r.LinkCount, want, r.Links)
	}
	if l := r.Links[1]; l.Like != "bank-example.com" {
		t.Errorf("look-alike %+v", l)
	}
	if l := r.Links[2]; l.Shown != "paypal.com" || l.Host != "evil.example" {
		t.Errorf("elsewhere %+v", l)
	}
	if l := r.Links[4]; l.Unicode == "" || l.Unicode == l.Host {
		t.Errorf("idn %+v", l)
	}
	// Plain text: its addresses are links too.
	if r := CheckContent("", "See http://198.51.100.4/x, then https://example.com.", nil, nil, nil); r.LinkCount != 2 || len(r.Links) != 1 || r.Links[0].Kind != LinkAddress {
		t.Fatalf("plain text %+v", r)
	}
}

// The images that report the message was opened — a pixel, or hidden —
// and every organisation the HTML would load something from; forms, and
// a password asked for.
func TestContentTrackersAndForms(t *testing.T) {
	html := `<html><head><link rel="stylesheet" href="https://cdn.styles.example/a.css">
<style>.x{background:url('https://img.bg.example/b.png')}</style></head><body>
<img src="https://t.tracker.example/o.gif" width="1" height="1">
<img src="https://open.mailer.example/p.png" style="display: none">
<img src="https://images.shop.example/hero.jpg" width="600">
<img src="cid:logo">
<div style="background-image:url(https://deco.example/c.png)">x</div>
<form action="https://collect.example/login"><input type="password" name="p"></form>
</body></html>`
	r := CheckContent(html, "", nil, nil, nil)
	if !slices.Equal(r.Trackers, []string{"mailer.example", "tracker.example"}) {
		t.Errorf("trackers %v", r.Trackers)
	}
	if !slices.Equal(r.Remote, []string{"bg.example", "deco.example", "mailer.example", "shop.example", "styles.example", "tracker.example"}) {
		t.Errorf("remote %v", r.Remote)
	}
	if !slices.Equal(r.Forms, []string{"https://collect.example/login"}) || !r.Password {
		t.Errorf("forms %v password %v", r.Forms, r.Password)
	}
}

// Attachments worth a word: what runs, what hides its ending, macros,
// archives, a type that disagrees with the name; nothing for a plain PDF.
func TestContentAttachments(t *testing.T) {
	parts := []Part{
		{ID: "2", MIMEType: "application/pdf", Filename: "report.pdf"},
		{ID: "3", MIMEType: "application/x-msdownload", Filename: "setup.exe"},
		{ID: "4", MIMEType: "application/octet-stream", Filename: "invoice.pdf.exe"},
		{ID: "5", MIMEType: "application/octet-stream", Filename: "invoice‮fdp.exe"},
		{ID: "6", MIMEType: "application/vnd.ms-excel.sheet.macroEnabled.12", Filename: "budget.xlsm"},
		{ID: "7", MIMEType: "application/zip", Filename: "photos.zip"},
		{ID: "8", MIMEType: "image/png", Filename: "scan.pdf"},
		{ID: "9", MIMEType: "application/octet-stream", Filename: "disk.iso"},
		{ID: "1", MIMEType: "text/plain"},
	}
	r := CheckContent("", "", parts, nil, nil)
	got := map[string]string{}
	for _, a := range r.Attachments {
		got[a.Name] = a.Kind
	}
	want := map[string]string{"setup.exe": AttachRuns, "invoice.pdf.exe": AttachHidden, "invoice‮fdp.exe": AttachHidden,
		"budget.xlsm": AttachMacros, "photos.zip": AttachArchive, "scan.pdf": AttachMismatch, "disk.iso": AttachRuns}
	if len(got) != len(want) {
		t.Fatalf("attachments %v", got)
	}
	for n, k := range want {
		if got[n] != k {
			t.Errorf("%q: %q, want %q", n, got[n], k)
		}
	}
}

// What the other headers say: a read receipt asked for, bounces and the
// Message-ID on other domains, a date after it arrived, a list's way out.
func TestContentHeaders(t *testing.T) {
	raw := lf2crlf("Received: from a by mx.example.org with ESMTPS id 1; Mon, 5 Oct 2026 10:00:00 +0000\n" +
		"Disposition-Notification-To: Ann <ann@example.com>\nReturn-Path: <bounce@esp.example>\n" +
		"Message-ID: <123@mailer.example>\nDate: Tue, 6 Oct 2026 10:00:00 +0000\n" +
		"List-Unsubscribe: <https://example.com/u>, <mailto:u@example.com>\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\n" +
		"From: Ann <ann@example.com>\nSubject: x\n\nx\n")
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, n := range CheckContent("", "", nil, m.Header, nil).Headers {
		kinds = append(kinds, n.Kind)
	}
	if want := []string{HeaderReceipt, HeaderReturnPath, HeaderMessageID, HeaderFuture, HeaderOneClick}; !slices.Equal(kinds, want) {
		t.Fatalf("headers %v, want %v", kinds, want)
	}
}

// The report checks the message as it came: its HTML as sent, before
// anything is taken out for showing.
func TestReportChecksContent(t *testing.T) {
	raw := lf2crlf("From: a@example.com\nSubject: x\nContent-Type: text/html\n\n<a href=\"https://evil.example/\">paypal.com</a><script>x()</script>\n")
	r := SecurityReportOf(raw)
	if len(r.Content.Links) != 1 || r.Content.Links[0].Kind != LinkElsewhere {
		t.Fatalf("content %+v", r.Content)
	}
}
