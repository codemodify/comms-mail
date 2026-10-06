package mailcore

import (
	"strings"
	"testing"
)

// Gmail's Authentication-Results: its server, each check with the domain
// it checked and whether that is From's, the key's selector, and DMARC's
// policy, which it says in a comment.
func TestReportReadsTheChecks(t *testing.T) {
	raw := lf2crlf("Authentication-Results: mx.google.com;\n" +
		"       dkim=pass header.i=@example.com header.s=s1 header.b=AbCd;\n" +
		"       spf=pass (google.com: domain of bounce@mail.example.com designates 1.2.3.4 as permitted sender) smtp.mailfrom=bounce@mail.example.com;\n" +
		"       dmarc=pass (p=REJECT sp=REJECT dis=NONE) header.from=example.com\n" +
		"DKIM-Signature: v=1; a=rsa-sha256; c=relaxed/relaxed; d=example.com; s=s1;\n t=1700000000; h=from:to:subject; bh=x; b=y\n" +
		"DKIM-Signature: v=1; a=rsa-sha1; d=esp.net; s=k; l=120; h=from; bh=x; b=y\n" +
		"From: Ann <ann@example.com>\nTo: me@example.org\nSubject: Hi\n\nHello.\n")
	r := SecurityReportOf(raw)
	if r.Server != "mx.google.com" || r.Trust != AuthByUnknown || r.Sender.Auth != AuthPass {
		t.Fatalf("server %q trust %q verdict %q", r.Server, r.Trust, r.Sender.Auth)
	}
	want := []AuthCheck{
		{Method: "dkim", Result: "pass", Domain: "example.com", Aligned: true, Selector: "s1"},
		{Method: "spf", Result: "pass", Domain: "mail.example.com", Aligned: true},
		{Method: "dmarc", Result: "pass", Domain: "example.com", Aligned: true, Policy: "reject", Disposition: "none"},
	}
	if len(r.Checks) != len(want) {
		t.Fatalf("checks %+v", r.Checks)
	}
	for i := range want {
		if r.Checks[i] != want[i] {
			t.Errorf("check %d: %+v, want %+v", i, r.Checks[i], want[i])
		}
	}
	if len(r.DKIM) != 2 {
		t.Fatalf("dkim %+v", r.DKIM)
	}
	if d := r.DKIM[0]; d.Domain != "example.com" || !d.Aligned || d.Result != "pass" || d.Weak != "" || d.Signed.Unix() != 1700000000 {
		t.Errorf("first signature %+v", d)
	}
	if d := r.DKIM[1]; d.Domain != "esp.net" || d.Aligned || d.Result != "" || d.Weak == "" || d.Length != 120 {
		t.Errorf("weak signature %+v", d)
	}
}

// Microsoft 365 names no server, says DMARC's outcome as an action, and
// adds its own overall verdict; a mailing list's ARC set says what it saw.
func TestReportReadsMicrosoftAndARC(t *testing.T) {
	raw := lf2crlf("Authentication-Results: spf=pass (sender IP is 1.2.3.4)\n smtp.mailfrom=lists.example.org; dkim=fail (body hash did not verify)\n header.d=example.com;dmarc=fail action=quarantine\n header.from=example.com;compauth=pass reason=100\n" +
		"ARC-Seal: i=1; a=rsa-sha256; t=1700000000; cv=none; d=example.org; s=arc; b=z\n" +
		"ARC-Authentication-Results: i=1; lists.example.org; spf=pass smtp.mailfrom=example.com;\n dkim=pass header.d=example.com; dmarc=pass header.from=example.com\n" +
		"From: Ann <ann@example.com>\nTo: list@lists.example.org\nSubject: Hi\n\nHello.\n")
	r := SecurityReportOf(raw)
	if r.Server != "?" {
		t.Fatalf("server %q", r.Server)
	}
	var dmarc, comp AuthCheck
	for _, c := range r.Checks {
		switch c.Method {
		case "dmarc":
			dmarc = c
		case "compauth":
			comp = c
		}
	}
	if dmarc.Result != "fail" || dmarc.Disposition != "quarantine" || comp.Result != "pass" || comp.Reason != "100" {
		t.Fatalf("dmarc %+v compauth %+v", dmarc, comp)
	}
	if len(r.ARC) != 1 {
		t.Fatalf("arc %+v", r.ARC)
	}
	a := r.ARC[0]
	if a.Instance != 1 || a.Server != "lists.example.org" || a.Signer != "example.org" || a.Seal != "none" || len(a.Checks) != 3 || a.Checks[1].Domain != "example.com" {
		t.Fatalf("arc set %+v", a)
	}
}

// Whose the verdict is: an account's own provider's server — by its
// servers' names, the provider's other domains, or the server that put
// the message in the mailbox — counts; any other server's came with the
// message and does not, and says so; mail with no account counts, as it
// always did.
func TestReportTrustsOnlyYourProvider(t *testing.T) {
	st := newImportStore(t)
	st.mu.Lock()
	st.cfg.Accounts = []AccountConfig{
		{ID: "g", Address: "me@gmail.com", IMAP: ServerConfig{Host: "imap.gmail.com:993"}},
		{ID: "h", Address: "me@example.org", IMAP: ServerConfig{Host: "imap.example.net:993"}},
	}
	st.mu.Unlock()
	add := func(acct, subject, head string) MessageID {
		t.Helper()
		raw := lf2crlf(head + "From: Ann <ann@example.com>\nTo: me@example.org\nSubject: " + subject + "\n\nHello.\n")
		m, err := ParseRFC822(raw, FolderID(acct+"-inbox"), acct)
		if err != nil {
			t.Fatal(err)
		}
		m.ID = MessageID(acct + "-" + subject)
		st.mu.Lock()
		st.Messages = append(st.Messages, m)
		st.WriteRawLocked(m, raw)
		st.mu.Unlock()
		return m.ID
	}
	pass := "Authentication-Results: %s; dmarc=pass header.from=example.com\n"
	ar := func(server string) string { return strings.Replace(pass, "%s", server, 1) }
	gmail := add("g", "gmail", ar("mx.google.com"))
	forged := add("g", "forged", ar("mx.evil.example"))
	delivered := add("h", "delivered", "Received: from mx.mailhost.io by mx.mailhost.io with ESMTPS id 1; Mon, 5 Oct 2026 10:00:00 +0000\n"+ar("mx.mailhost.io"))
	unnamed := add("h", "unnamed", "Authentication-Results: dmarc=pass header.from=example.com\n")
	imported := add(LocalAccountID, "imported", ar("mx.anywhere.example"))

	for _, c := range []struct {
		id    MessageID
		trust string
		auth  string
	}{
		{gmail, AuthByProvider, AuthPass},
		{forged, AuthByOther, AuthNone},
		{delivered, AuthByProvider, AuthPass},
		{unnamed, AuthByOther, AuthNone},
		{imported, AuthByUnknown, AuthPass},
	} {
		sc, err := st.SenderCheck(c.id)
		if err != nil {
			t.Fatal(err)
		}
		if sc.AuthTrust != c.trust || sc.Auth != c.auth {
			t.Errorf("%s: trust %q verdict %q, want %q %q", c.id, sc.AuthTrust, sc.Auth, c.trust, c.auth)
		}
		says := strings.Join(sc.Notes, " ")
		if (c.trust == AuthByOther) != strings.Contains(says, "proves nothing") {
			t.Errorf("%s: notes %q", c.id, sc.Notes)
		}
		m, _ := st.GetMessage(c.id)
		if m.Auth != c.auth {
			t.Errorf("%s: the window gets the verdict %q, want %q", c.id, m.Auth, c.auth)
		}
		r, err := st.SecurityReport(c.id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Trust != c.trust || r.Sender.Auth != c.auth || len(r.Checks) != 1 {
			t.Errorf("%s: report %+v", c.id, r)
		}
	}
	// The cache keeps what the message says.
	st.mu.Lock()
	for _, m := range st.Messages {
		if m.ID == forged && m.Auth != AuthPass {
			t.Errorf("the cache lost the forged verdict: %q", m.Auth)
		}
	}
	st.mu.Unlock()
}

// A message cached before who wrote its verdict was kept gets it from its
// raw message.
func TestReportFillsOldCache(t *testing.T) {
	st := newImportStore(t)
	st.mu.Lock()
	st.cfg.Accounts = []AccountConfig{{ID: "g", Address: "me@gmail.com", IMAP: ServerConfig{Host: "imap.gmail.com:993"}}}
	st.mu.Unlock()
	raw := lf2crlf("Authentication-Results: mx.evil.example; dmarc=pass header.from=example.com\nFrom: ann@example.com\nSubject: Old\n\nHi.\n")
	m, err := ParseRFC822(raw, "g-inbox", "g")
	if err != nil {
		t.Fatal(err)
	}
	m.ID, m.AuthServer, m.DeliveredBy = "g-old", "", ""
	st.mu.Lock()
	st.Messages = append(st.Messages, m)
	st.WriteRawLocked(m, raw)
	st.mu.Unlock()
	if sc, _ := st.SenderCheck("g-old"); sc.AuthServer != "mx.evil.example" || sc.Auth != AuthNone {
		t.Fatalf("old cache: %+v", sc)
	}
}
