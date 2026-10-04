package mailcore

import (
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func header(lines ...string) mail.Header {
	h := mail.Header{}
	for _, l := range lines {
		k, v, _ := strings.Cut(l, ": ")
		h[k] = append(h[k], v)
	}
	return h
}

// The receiving server's verdict, from the topmost Authentication-Results
// header only — what came with the message proves nothing.
func TestAuthVerdict(t *testing.T) {
	for _, c := range []struct {
		name, from string
		h          mail.Header
		want, why  string
	}{
		{"DMARC pass", "ann@example.com", header("Authentication-Results: mail.nchip.com; dkim=pass header.d=example.com; spf=pass smtp.mailfrom=example.com; dmarc=pass header.from=example.com"), AuthPass, ""},
		{"DMARC fail", "pay@paypal.com", header("Authentication-Results: mail.nchip.com; dkim=none; spf=softfail smtp.mailfrom=evil.biz; dmarc=fail (p=REJECT; dis=NONE) header.from=paypal.com"), AuthFail, "dmarc"},
		{"forged pass below the server's fail", "pay@paypal.com", header(
			"Authentication-Results: mail.nchip.com; dmarc=fail header.from=paypal.com",
			"Authentication-Results: mx.paypal.com; dmarc=pass header.from=paypal.com"), AuthFail, "dmarc"},
		{"DKIM for the From domain", "a@example.com", header("Authentication-Results: mx; dkim=pass header.i=@news.example.com"), AuthPass, ""},
		{"DKIM for someone else's domain", "a@acme.com", header("Authentication-Results: mx; dkim=pass header.d=sendgrid.net; spf=pass smtp.mailfrom=bounces.sendgrid.net"), AuthNone, ""},
		{"DKIM fail", "a@acme.com", header("Authentication-Results: mx; dkim=fail header.d=acme.com"), AuthFail, "dkim"},
		{"SPF fail", "a@acme.com", header("Authentication-Results: mx; spf=fail smtp.mailfrom=acme.com"), AuthFail, "spf"},
		{"Microsoft 365, no server id", "a@acme.com", header("Authentication-Results: spf=pass (sender IP is 192.0.2.1) smtp.mailfrom=acme.com; dkim=none (message not signed) header.d=none;dmarc=none action=none header.from=acme.com;compauth=fail reason=001"), AuthFail, "compauth"},
		{"Microsoft 365 pass", "a@acme.com", header("Authentication-Results: spf=pass (sender IP is 192.0.2.1) smtp.mailfrom=acme.com; dkim=pass (signature was verified) header.d=acme.com;dmarc=pass action=none header.from=acme.com;compauth=pass reason=100"), AuthPass, ""},
		{"no header", "a@acme.com", header("Subject: x"), AuthNone, ""},
	} {
		if got, why := authVerdict(c.h, c.from); got != c.want || why != c.why {
			t.Errorf("%s: %s %q, want %s %q", c.name, got, why, c.want, c.why)
		}
	}
}

// What a forged sender tries on a reader, against the people you write to.
func TestCheckSender(t *testing.T) {
	known := knownPeople{
		addrs:   map[string]bool{"ada@example.com": true, "bob@bank-example.com": true},
		names:   map[string][]string{"bob builder": {"bob@bank-example.com"}},
		domains: map[string]bool{"example.com": true, "bank-example.com": true},
	}
	for _, c := range []struct {
		name, from, replyTo, auth, why string
		warn, note                     string // in a warning / a note, or "" for none
	}{
		{"a sender you write to", "Bob Builder <bob@bank-example.com>", "", AuthPass, "", "", ""},
		{"the name shows another address", `"service@paypal.com" <x@evil.biz>`, "", AuthNone, "", "The name shows service@paypal.com", ""},
		{"the name says another domain", "PayPal.com Support <x@evil.biz>", "", AuthNone, "", "The name says paypal.com", ""},
		{"the name says its own domain", "Booking.com <noreply@booking.com>", "", AuthPass, "", "", ""},
		{"an abbreviation is no domain", "St.Mary Hospital <office@stmary.org>", "", AuthPass, "", "", ""},
		{"a contact's name on another address", "Bob Builder <bob.builder@gmail.com>", "", AuthPass, "", "someone you write to at bob@bank-example.com", ""},
		{"a look-alike domain", "Bank <alerts@bank-examp1e.com>", "", AuthNone, "", "looks like bank-example.com", ""},
		{"another alphabet", "PayPal <x@xn--pypal-4ve.com>", "", AuthNone, "", "letters from another alphabet", ""},
		{"the server says it is not them", "PayPal <pay@paypal.com>", "", AuthFail, "dmarc", "could not confirm this is from paypal.com: it failed paypal.com's own sender policy (DMARC)", ""},
		{"replies elsewhere", "Shop <news@shop.example>", "deals@other.example", AuthPass, "", "", "Replies go to deals@other.example"},
	} {
		got := checkSender(Message{From: c.from, ReplyTo: c.replyTo, Auth: c.auth, AuthWhy: c.why}, known)
		w, n := strings.Join(got.Warnings, " | "), strings.Join(got.Notes, " | ")
		switch {
		case c.warn == "" && w != "":
			t.Errorf("%s: warned %q", c.name, w)
		case c.warn != "" && !strings.Contains(w, c.warn):
			t.Errorf("%s: warnings %q, want %q", c.name, w, c.warn)
		case c.note == "" && n != "":
			t.Errorf("%s: noted %q", c.name, n)
		case c.note != "" && !strings.Contains(n, c.note):
			t.Errorf("%s: notes %q, want %q", c.name, n, c.note)
		}
	}
}

// The people you write to are the recipients of what you sent, not
// everyone who wrote to you; and a message cached before the verdict was
// read gets it from its raw message.
func TestSenderCheckFromTheStore(t *testing.T) {
	st := newImportStore(t)
	dir := t.TempDir()
	write := func(name, raw string) {
		if err := os.WriteFile(filepath.Join(dir, name), lf2crlf(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.eml", "From: Ada <ada@example.com>\nTo: Bob Builder <bob@bank-example.com>\nSubject: Sent one\n\nHi Bob.\n")
	write("b.eml", "From: Scammer <x@examp1e-shop.com>\nTo: ada@example.com\nSubject: Inbox one\n\nHello.\n")
	write("c.eml", "Authentication-Results: mx.local; dmarc=fail header.from=bank-example.com\nFrom: Bob Builder <bob@bank-examp1e.com>\nTo: ada@example.com\nSubject: Look-alike\n\nPay me.\n")
	if _, err := st.ImportLocalMail([]LocalMailStore{{Source: "Folder", Name: "Sent", Path: dir, Kind: StoreEML}}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]MessageID{}
	var folder FolderID
	for _, f := range st.ListFolders(LocalAccountID) {
		folder = f.ID
		for _, m := range st.ListMessages(f.ID) {
			ids[m.Subject] = m.ID
		}
	}
	// Make the folder a Sent folder holding only the message to Bob.
	st.mu.Lock()
	for i := range st.Folders {
		if st.Folders[i].ID == folder {
			st.Folders[i].Kind = FolderSent
		}
	}
	inbox := Folder{ID: folder + "-in", AccountID: LocalAccountID, Name: "Inbox", Kind: FolderInbox}
	st.Folders = append(st.Folders, inbox)
	for i := range st.Messages {
		if st.Messages[i].Subject != "Sent one" {
			st.Messages[i].Folder = inbox.ID
		}
		if st.Messages[i].Subject == "Look-alike" {
			st.Messages[i].Auth, st.Messages[i].AuthWhy = "", "" // cached by an older build
		}
	}
	st.mu.Unlock()
	knownCache.mu.Lock()
	knownCache.of = nil
	knownCache.mu.Unlock()

	got, err := st.SenderCheck(ids["Look-alike"])
	if err != nil {
		t.Fatal(err)
	}
	w := strings.Join(got.Warnings, " | ")
	if got.Auth != AuthFail || !strings.Contains(w, "DMARC") || !strings.Contains(w, "looks like bank-example.com") || !strings.Contains(w, "someone you write to at bob@bank-example.com") {
		t.Fatalf("look-alike: %+v", got)
	}
	// The scammer wrote to you; that makes their domain nobody's to
	// imitate, and example-shop's look-alike is not flagged against it.
	got, _ = st.SenderCheck(ids["Inbox one"])
	if strings.Contains(strings.Join(got.Warnings, " "), "looks like") {
		t.Fatalf("an inbox sender counted as someone you write to: %+v", got)
	}
}
