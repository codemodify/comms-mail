package mail

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Scripted fake IMAP server
// ---------------------------------------------------------------------------

// scriptIMAP is a fake server driven by a per-command handler, so each test
// can script exactly the behaviour it needs (missing STARTTLS, a SASL "+"
// continuation, reordered FETCH attributes, COPYUID, a shrinking UID set).
type scriptIMAP struct {
	t        *testing.T
	ln       net.Listener
	caps     string
	handle   func(s *imapSession, tag, cmd, line string) bool
	mu       chan struct{}
	commands []string
}

type imapSession struct {
	w   *bufio.Writer
	srv *scriptIMAP
}

func (s *imapSession) send(format string, a ...any) {
	_, _ = fmt.Fprintf(s.w, format+"\r\n", a...)
	_ = s.w.Flush()
}

func (s *imapSession) raw(b string) {
	_, _ = s.w.WriteString(b)
	_ = s.w.Flush()
}

func newScriptIMAP(t *testing.T, caps string, handle func(s *imapSession, tag, cmd, line string) bool) *scriptIMAP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &scriptIMAP{t: t, ln: ln, caps: caps, handle: handle, mu: make(chan struct{}, 1)}
	srv.mu <- struct{}{}
	t.Cleanup(func() { _ = ln.Close() })
	go srv.serve()
	return srv
}

func (srv *scriptIMAP) addr() string { return srv.ln.Addr().String() }

func (srv *scriptIMAP) log(line string) {
	<-srv.mu
	srv.commands = append(srv.commands, line)
	srv.mu <- struct{}{}
}

func (srv *scriptIMAP) sawPrefix(prefix string) bool {
	<-srv.mu
	defer func() { srv.mu <- struct{}{} }()
	for _, c := range srv.commands {
		if strings.HasPrefix(strings.ToUpper(c), strings.ToUpper(prefix)) {
			return true
		}
	}
	return false
}

func (srv *scriptIMAP) serve() {
	for {
		c, err := srv.ln.Accept()
		if err != nil {
			return
		}
		go srv.session(c)
	}
}

func (srv *scriptIMAP) session(c net.Conn) {
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	s := &imapSession{w: w, srv: srv}
	s.send("* OK script imap")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag, cmd := fields[0], strings.ToUpper(fields[1])
		srv.log(strings.TrimPrefix(line, tag+" "))
		if srv.handle != nil && srv.handle(s, tag, cmd, line) {
			continue
		}
		switch cmd {
		case "CAPABILITY":
			s.send("* CAPABILITY IMAP4rev1 %s", srv.caps)
			s.send("%s OK capability", tag)
		case "LOGIN":
			s.send("%s OK logged in", tag)
		case "LOGOUT":
			s.send("* BYE")
			s.send("%s OK logout", tag)
			return
		case "NOOP":
			s.send("%s OK noop", tag)
		default:
			s.send("%s BAD %s", tag, cmd)
		}
	}
}

func plainIMAPConfig(addr string) ServerConfig {
	return ServerConfig{Host: addr, User: "ada@example.com", Pass: "secret", TLSMode: string(TLSPlain)}
}

// ---------------------------------------------------------------------------
// TLS mode resolution + STARTTLS stripping
// ---------------------------------------------------------------------------

func TestTLSModeResolution(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		cfg  ServerConfig
		def  defaultPorts
		want TLSMode
	}{
		{"explicit ssl", ServerConfig{Host: "h:1234", TLSMode: "ssl"}, imapPorts, TLSImplicit},
		{"explicit plain", ServerConfig{Host: "h:993", TLSMode: "plain"}, imapPorts, TLSPlain},
		{"993 default", ServerConfig{Host: "h:993"}, imapPorts, TLSImplicit},
		{"143 default", ServerConfig{Host: "h:143"}, imapPorts, TLSStartTLS},
		// The regression: tls:true on a STARTTLS port used to resolve to
		// "neither implicit TLS nor STARTTLS", i.e. a cleartext LOGIN.
		{"tls true on 143 upgrades", ServerConfig{Host: "h:143", TLS: &yes}, imapPorts, TLSStartTLS},
		{"tls true on 110 upgrades", ServerConfig{Host: "h:110", TLS: &yes}, popPorts, TLSStartTLS},
		{"tls true on 587 upgrades", ServerConfig{Host: "h:587", TLS: &yes}, smtpPorts, TLSStartTLS},
		{"tls true on 993 stays ssl", ServerConfig{Host: "h:993", TLS: &yes}, imapPorts, TLSImplicit},
		{"starttls wins", ServerConfig{Host: "h:993", StartTLS: &yes}, imapPorts, TLSStartTLS},
		{"tls false custom port", ServerConfig{Host: "h:1234", TLS: &no}, imapPorts, TLSPlain},
		{"tls false on 143", ServerConfig{Host: "h:143", TLS: &no}, imapPorts, TLSStartTLS},
		{"unknown port imap", ServerConfig{Host: "h:1234"}, imapPorts, TLSImplicit},
		{"unknown port smtp", ServerConfig{Host: "h:1234"}, smtpPorts, TLSStartTLS},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cfg.Mode(c.def); got != c.want {
				t.Fatalf("Mode = %q, want %q", got, c.want)
			}
		})
	}
}

func TestIMAPRefusesWhenSTARTTLSStripped(t *testing.T) {
	// A MITM that strips the STARTTLS capability must not get a cleartext
	// LOGIN: the client fails closed instead.
	srv := newScriptIMAP(t, "AUTH=PLAIN", nil)
	cfg := ServerConfig{Host: srv.addr(), User: "ada@example.com", Pass: "secret", TLSMode: string(TLSStartTLS)}
	c := newIMAPClient(cfg, "ada@example.com")
	err := c.connect()
	if err == nil {
		c.close()
		t.Fatal("connect must fail when the server does not advertise STARTTLS")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("error should name STARTTLS, got %v", err)
	}
	if srv.sawPrefix("LOGIN") {
		t.Fatal("credentials were sent over the unencrypted connection")
	}
}

func TestIMAPRefusesCleartextCredentialsToRemoteHost(t *testing.T) {
	cfg := ServerConfig{Host: "mail.example.com:1234", User: "ada", Pass: "secret", TLSMode: string(TLSPlain)}
	c := newIMAPClient(cfg, "ada@example.com")
	err := c.loginLocked(TLSPlain, cfg.Host)
	if err == nil || !strings.Contains(err.Error(), "unencrypted") {
		t.Fatalf("want refusal for a remote cleartext login, got %v", err)
	}
	// Loopback (a local dev/test server) is still allowed.
	if err := c.loginLocked(TLSPlain, "127.0.0.1:1234"); err != nil && strings.Contains(err.Error(), "unencrypted") {
		t.Fatal("loopback must stay usable")
	}
}

func TestSMTPRefusesWhenSTARTTLSStripped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	authSeen := make(chan string, 4)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		w := bufio.NewWriter(c)
		r := bufio.NewReader(c)
		send := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
		send("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			up := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(up, "EHLO"):
				// Deliberately no 250-STARTTLS.
				send("250-fake")
				send("250 AUTH PLAIN LOGIN")
			case strings.HasPrefix(up, "AUTH"):
				authSeen <- up
				send("235 ok")
			case strings.HasPrefix(up, "QUIT"):
				send("221 bye")
				return
			default:
				send("250 ok")
			}
		}
	}()

	cfg := ServerConfig{Host: ln.Addr().String(), User: "ada@example.com", Pass: "secret", TLSMode: string(TLSStartTLS)}
	err = SendSMTP(cfg, "ada@example.com", []string{"bob@example.com"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("send should fail closed, got %v", err)
	}
	select {
	case a := <-authSeen:
		t.Fatalf("credentials were sent in the clear: %s", a)
	default:
	}
}

// ---------------------------------------------------------------------------
// XOAUTH2 "+" continuation
// ---------------------------------------------------------------------------

func TestIMAPXOAUTH2ContinuationDoesNotHang(t *testing.T) {
	// A rejected XOAUTH2 exchange answers with "+ <base64 json>" and waits
	// for an empty line before sending the tagged NO. Without that empty
	// line both sides block forever.
	srv := newScriptIMAP(t, "AUTH=XOAUTH2", func(s *imapSession, tag, cmd, line string) bool {
		if cmd != "AUTHENTICATE" {
			return false
		}
		s.send("+ eyJzdGF0dXMiOiI0MDEifQ==")
		// Wait for the client's empty line before finishing.
		s.send("%s NO invalid credentials", tag)
		return true
	})
	t.Setenv(EnvXOAuth, "fake-bearer-token")
	cfg := ServerConfig{Host: srv.addr(), User: "ada@example.com", Auth: "xoauth2", TLSMode: string(TLSPlain)}
	c := newIMAPClient(cfg, "ada@example.com")
	c.cmdTimeout = 5 * time.Second

	done := make(chan error, 1)
	go func() { done <- c.connect() }()
	select {
	case err := <-done:
		if err == nil {
			c.close()
			t.Fatal("expected an auth failure")
		}
		if !strings.Contains(err.Error(), "invalid credentials") {
			t.Fatalf("want the server's NO, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("XOAUTH2 continuation hung the client")
	}
}

func TestIMAPCommandDeadlineFiresOnSilentServer(t *testing.T) {
	// A server that accepts the connection then says nothing must not wedge
	// the client (and, through it, every RPC) forever.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte("* OK silent\r\n"))
		select {}
	}()
	c := newIMAPClient(plainIMAPConfig(ln.Addr().String()), "ada@example.com")
	c.cmdTimeout = 400 * time.Millisecond
	start := time.Now()
	if err := c.connect(); err == nil {
		c.close()
		t.Fatal("connect should time out")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("deadline did not fire (took %s)", d)
	}
}

// ---------------------------------------------------------------------------
// FETCH parsing: attribute order and flag lists
// ---------------------------------------------------------------------------

func TestParseFetchAttributeOrderIndependent(t *testing.T) {
	// ENVELOPE first, UID last, and a subject that contains the token "UID ".
	// The old substring scan read the UID out of the subject and dropped the
	// message (UID 0 is skipped by the sync).
	line := `* 1 FETCH (ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" "SQUID Game UID 9" ` +
		`(("Bot" NIL "bot" "example.com")) NIL NIL (("Ada" NIL "ada" "example.com")) NIL NIL NIL "<m1@ex>") ` +
		`RFC822.SIZE 4321 FLAGS (\Seen \Flagged kw) UID 42)`
	got := parseUIDFetchMeta([]string{line})
	if len(got) != 1 {
		t.Fatalf("parsed %d messages", len(got))
	}
	m := got[0]
	if m.UID != 42 {
		t.Fatalf("UID = %d, want 42", m.UID)
	}
	if m.Size != 4321 {
		t.Fatalf("size = %d, want 4321", m.Size)
	}
	if m.Subject != "SQUID Game UID 9" {
		t.Fatalf("subject = %q", m.Subject)
	}
	if !imapFlagSeen(m.Flags) || !imapFlagStar(m.Flags) {
		t.Fatalf("flags = %v", m.Flags)
	}
	if kw := imapKeywords(m.Flags); len(kw) != 1 || kw[0] != "kw" {
		t.Fatalf("keywords = %v", kw)
	}
	if m.From != "Bot <bot@example.com>" {
		t.Fatalf("from = %q", m.From)
	}
	if m.RFCMessageID != "<m1@ex>" {
		t.Fatalf("message-id = %q", m.RFCMessageID)
	}
}

func TestParseFetchUIDFirstStillWorks(t *testing.T) {
	line := `* 7 FETCH (UID 17 FLAGS (\Seen) RFC822.SIZE 80)`
	got := parseUIDFetchMeta([]string{line})
	if len(got) != 1 || got[0].UID != 17 || got[0].Size != 80 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseSexpNeverSpinsOnGarbage(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = parseSexp(`(UID 1 FLAGS (\Seen) ) ) ( %% ** "unterminated`, 0)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("parseSexp spun on malformed input")
	}
}

func TestParseCopyUID(t *testing.T) {
	cases := map[string]uint32{
		"OK [COPYUID 1234 17 300] Copy completed": 300,
		"ok [copyuid 1 1:3 10:12] done":           12,
		"OK [COPYUID 9 4,7 21,25] moved":          25,
		"OK Copy completed":                       0,
		"NO [TRYCREATE] no such mailbox":          0,
	}
	for line, want := range cases {
		got, ok := parseCopyUID(line)
		if want == 0 {
			if ok {
				t.Fatalf("%q: expected no COPYUID, got %d", line, got)
			}
			continue
		}
		if !ok || got != want {
			t.Fatalf("%q: got %d/%v, want %d", line, got, ok, want)
		}
	}
}

// ---------------------------------------------------------------------------
// MOVE / COPYUID re-keying and UID EXPUNGE
// ---------------------------------------------------------------------------

func TestMoveRekeysUIDFromCOPYUID(t *testing.T) {
	srv := newScriptIMAP(t, "MOVE UIDPLUS", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case strings.Contains(up, "UID MOVE"):
			s.send("%s OK [COPYUID 1 7 4242] Move completed", tag)
			return true
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.send("* 1 EXISTS")
			s.send("* OK [UIDVALIDITY 1]")
			s.send("* OK [UIDNEXT 8]")
			s.send("%s OK [READ-WRITE] selected", tag)
			return true
		}
		return false
	})

	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	cfg := MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: srv.addr(), User: "ada@example.com", Pass: "secret", TLSMode: string(TLSPlain)},
		SMTP: ServerConfig{Host: "smtp.example.com:587"},
	}}}
	st, err := NewLocalStoreDir(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.folders = append(st.folders,
		Folder{ID: "home/inbox", AccountID: "home", Name: "Inbox", Kind: FolderInbox, Remote: "INBOX"},
		Folder{ID: "home/archive", AccountID: "home", Name: "Archives", Kind: FolderArchive, Remote: "Archives"},
	)
	st.messages = append(st.messages, Message{
		ID: "home/inbox:7", Folder: "home/inbox", AccountID: "home", UID: 7, Subject: "hi",
	})
	st.writeRawLocked(st.messages[0], []byte("Subject: hi\r\n\r\nbody\r\n"))

	if err := st.Move([]MessageID{"home/inbox:7"}, "home/archive"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.indexLocked("home/inbox:7"); ok {
		t.Fatal("old id must not survive a move")
	}
	i, ok := st.indexLocked("home/archive:4242")
	if !ok {
		t.Fatalf("message was not re-keyed to the COPYUID destination; have %+v", st.messages)
	}
	if got := st.messages[i].UID; got != 4242 {
		t.Fatalf("UID = %d, want 4242", got)
	}
	if st.messages[i].Folder != "home/archive" {
		t.Fatalf("folder = %s", st.messages[i].Folder)
	}
	if raw := st.readRawLocked(st.messages[i]); len(raw) == 0 {
		t.Fatal("cached blob should follow the message to its new id")
	}
}

func TestMoveWithoutCOPYUIDDropsStaleEntry(t *testing.T) {
	// No UIDPLUS: keeping the source UID would later address an unrelated
	// message in the destination mailbox, so the entry is dropped and the
	// next sync re-adds it.
	srv := newScriptIMAP(t, "MOVE", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case strings.Contains(up, "UID MOVE"):
			s.send("%s OK Move completed", tag)
			return true
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.send("* OK [UIDVALIDITY 1]")
			s.send("%s OK [READ-WRITE] selected", tag)
			return true
		}
		return false
	})
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: srv.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.folders = append(st.folders,
		Folder{ID: "home/inbox", AccountID: "home", Name: "Inbox", Kind: FolderInbox, Remote: "INBOX"},
		Folder{ID: "home/archive", AccountID: "home", Name: "Archives", Kind: FolderArchive, Remote: "Archives"},
	)
	st.messages = append(st.messages, Message{ID: "home/inbox:7", Folder: "home/inbox", AccountID: "home", UID: 7})
	if err := st.Move([]MessageID{"home/inbox:7"}, "home/archive"); err != nil {
		t.Fatal(err)
	}
	for _, m := range st.messages {
		if m.UID == 7 && m.Folder == "home/archive" {
			t.Fatal("a stale source UID must not be kept in the destination folder")
		}
	}
}

func TestExpungeUsesUIDEXPUNGEWhenUIDPLUS(t *testing.T) {
	srv := newScriptIMAP(t, "UIDPLUS", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case strings.Contains(up, "UID EXPUNGE"), strings.Contains(up, "UID STORE"):
			s.send("%s OK done", tag)
			return true
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.send("%s OK [READ-WRITE] selected", tag)
			return true
		}
		return false
	})
	c := newIMAPClient(plainIMAPConfig(srv.addr()), "ada@example.com")
	if err := c.connect(); err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if _, err := c.selectBox("INBOX", false); err != nil {
		t.Fatal(err)
	}
	if err := c.expungeUID(9); err != nil {
		t.Fatal(err)
	}
	if !srv.sawPrefix("UID EXPUNGE 9") {
		t.Fatal("UIDPLUS server should get UID EXPUNGE, not a bare EXPUNGE")
	}
}

// ---------------------------------------------------------------------------
// Deletion reconciliation without QRESYNC
// ---------------------------------------------------------------------------

func TestSyncReconcilesDeletionsWithoutQRESYNC(t *testing.T) {
	// The server never sends VANISHED. A message deleted from another client
	// must still leave the cache.
	var live = []uint32{1, 2, 3}
	setLive := func(u []uint32) { live = u }

	srv := newScriptIMAP(t, "IMAP4rev1", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case cmd == "LIST":
			s.send(`* LIST (\HasNoChildren) "/" "INBOX"`)
			s.send("%s OK list", tag)
			return true
		case cmd == "LSUB":
			s.send("%s OK lsub", tag)
			return true
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.send("* %d EXISTS", len(live))
			s.send("* OK [UIDVALIDITY 1]")
			s.send("* OK [UIDNEXT 99]")
			s.send("%s OK [READ-ONLY] selected", tag)
			return true
		case strings.Contains(up, "UID FETCH") && strings.Contains(up, "(UID)"):
			for i, u := range live {
				s.send("* %d FETCH (UID %d)", i+1, u)
			}
			s.send("%s OK fetch", tag)
			return true
		case strings.Contains(up, "UID FETCH"):
			for i, u := range live {
				s.send(`* %d FETCH (UID %d FLAGS (\Seen) RFC822.SIZE 10 ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" "m%d" (("B" NIL "b" "ex.com")) NIL NIL (("A" NIL "a" "ex.com")) NIL NIL NIL "<m%d@ex>"))`, i+1, u, u, u)
			}
			s.send("%s OK fetch", tag)
			return true
		}
		return false
	})

	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: srv.addr(), User: "ada", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync("home"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if n := len(st.ListMessages("home/inbox")); n != 3 {
		t.Fatalf("first sync cached %d messages, want 3", n)
	}

	setLive([]uint32{1, 3}) // UID 2 deleted elsewhere
	if _, err := st.Sync("home"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	got := st.ListMessages("home/inbox")
	if len(got) != 2 {
		t.Fatalf("after reconciliation %d messages remain, want 2", len(got))
	}
	for _, m := range got {
		if m.UID == 2 {
			t.Fatal("UID 2 was deleted on the server but survived in the cache")
		}
	}
}

// ---------------------------------------------------------------------------
// Header construction
// ---------------------------------------------------------------------------

func TestBuildRFC822NeverEmitsBcc(t *testing.T) {
	raw, err := BuildRFC822Strict(Message{
		From: "ada@example.com", To: "bob@example.com",
		Bcc: "secret@example.com", Subject: "hi", Body: "text",
	}, Identity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "Bcc:") || strings.Contains(s, "secret@example.com") {
		t.Fatalf("Bcc leaked into the wire message:\n%s", s)
	}
	if !strings.Contains(s, "To: bob@example.com") {
		t.Fatalf("To missing:\n%s", s)
	}
}

func TestBuildRFC822RejectsHeaderInjection(t *testing.T) {
	for _, c := range []struct{ name, field string }{
		{"to", "To"}, {"cc", "Cc"}, {"from", "From"},
	} {
		msg := Message{From: "ada@example.com", To: "bob@example.com", Subject: "s", Body: "b"}
		bad := "victim@example.com\r\nX-Injected: yes"
		switch c.field {
		case "To":
			msg.To = bad
		case "Cc":
			msg.Cc = bad
		case "From":
			msg.From = bad
		}
		if _, err := BuildRFC822Strict(msg, Identity{}, nil); err == nil {
			t.Fatalf("%s: CR/LF must be rejected", c.name)
		}
	}
	// Subject and Message-ID too.
	if _, err := BuildRFC822Strict(Message{From: "a@b.c", To: "d@e.f", Subject: "x\r\nBcc: evil@x"}, Identity{}, nil); err == nil {
		t.Fatal("subject injection must be rejected")
	}
	// The lenient wrapper sanitises rather than emitting a new header line.
	raw := BuildRFC822(Message{From: "a@b.c", To: "d@e.f\r\nX-Injected: yes", Subject: "s", Body: "b"}, Identity{}, nil)
	if strings.Contains(string(raw), "\r\nX-Injected:") {
		t.Fatalf("lenient builder let an injected header through:\n%s", raw)
	}
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	for _, line := range strings.Split(head, "\r\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok || strings.EqualFold(strings.TrimSpace(name), "x-injected") {
			t.Fatalf("unexpected header line %q", line)
		}
	}
}

func TestBuildRFC822EncodesUnicodeHeaders(t *testing.T) {
	raw, err := BuildRFC822Strict(Message{
		From: "Ada Lovelace <ada@example.com>", To: `"Bob Beispiel" <bob@example.com>`,
		Subject: "Grüße — Übersicht", Body: "b",
	}, Identity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, line := range strings.Split(s, "\r\n") {
		if line == "" {
			break
		}
		for i := 0; i < len(line); i++ {
			if line[i] >= 0x80 {
				t.Fatalf("non-ASCII byte in header line %q", line)
			}
		}
	}
	if !strings.Contains(s, "=?utf-8?q?") && !strings.Contains(s, "=?utf-8?Q?") {
		t.Fatalf("subject was not RFC 2047 encoded:\n%s", s)
	}
}

func TestBuildRFC822AttachmentFilenameIsSanitised(t *testing.T) {
	raw, err := BuildRFC822Strict(Message{From: "a@b.c", To: "d@e.f", Subject: "s", Body: "b"},
		Identity{}, []AttachedFile{{Name: "../../etc/passwd", MIME: "text/plain", Data: []byte("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "../") {
		t.Fatalf("path traversal survived into the filename parameter:\n%s", raw)
	}
	if !strings.Contains(string(raw), "passwd") {
		t.Fatal("base name should be kept")
	}
}

func TestReplyThreadHeaders(t *testing.T) {
	parent := Message{RFCMessageID: "<root@ex>", References: ""}
	irt, refs := replyThreadHeaders(parent)
	if irt != "<root@ex>" || refs != "<root@ex>" {
		t.Fatalf("irt=%q refs=%q", irt, refs)
	}
	child := Message{RFCMessageID: "<child@ex>", References: "<root@ex>"}
	irt, refs = replyThreadHeaders(child)
	if irt != "<child@ex>" {
		t.Fatalf("in-reply-to = %q", irt)
	}
	if refs != "<root@ex> <child@ex>" {
		t.Fatalf("references = %q", refs)
	}
}

// ---------------------------------------------------------------------------
// Path safety
// ---------------------------------------------------------------------------

func TestAccountIDCannotEscapeDataDir(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "share", "uitoolkit", "mail")
	victim := filepath.Join(base, "share", "victim.txt")
	if err := os.MkdirAll(filepath.Dir(victim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvConfig, filepath.Join(base, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, data)
	if err != nil {
		t.Fatal(err)
	}
	acct, err := st.PutAccount(AccountConfig{
		ID: "../../..", Address: "evil@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(acct.ID, "/.\\") {
		t.Fatalf("account id %q was not sanitised", acct.ID)
	}
	p, err := st.rawPath(Message{AccountID: acct.ID, ID: "../../../../etc/shadow"})
	if err == nil && !strings.HasPrefix(p, data) {
		t.Fatalf("rawPath escaped the data dir: %s", p)
	}
	if err := st.DeleteAccount(acct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("DeleteAccount removed a file outside the data dir: %v", err)
	}
}

func TestUnderRootRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := underRoot(root, "..", "escape"); err == nil {
		t.Fatal("../ must be rejected")
	}
	if _, err := underRoot(root, "raw", "../../etc/passwd"); err == nil {
		t.Fatal("nested traversal must be rejected")
	}
	got, err := underRoot(root, "raw", "acct", "m.eml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, root) {
		t.Fatalf("%s is not under %s", got, root)
	}
}

func TestSafeID(t *testing.T) {
	cases := map[string]string{
		"ada@example.com": "ada-example-com",
		"../../..":        "acct",
		"/etc/passwd":     "etc-passwd",
		"":                "acct",
		"Hello World":     "hello-world",
	}
	for in, want := range cases {
		if got := safeID(in); got != want {
			t.Fatalf("safeID(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Atomic store writes and crash recovery
// ---------------------------------------------------------------------------

func TestWriteFileAtomicLeavesNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "messages.json")
	if err := writeFileAtomic(path, []byte(`["good"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	// A failed write (unserialisable payload) must leave the previous
	// contents intact rather than a truncated file.
	if err := writeJSONFileAtomic(path, func() {}); err == nil {
		t.Fatal("expected a marshal error")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `["good"]` {
		t.Fatalf("previous contents were damaged: %q", b)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
	// No temp files left behind.
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

func TestCorruptCacheIsQuarantinedNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	// Simulate a crash mid-write: messages.json is truncated JSON.
	if err := os.WriteFile(filepath.Join(dir, "messages.json"), []byte(`[{"id":"a-1","sub`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tags.json"), []byte(`[{"name":"Work"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "messages.json.corrupt")); err != nil {
		t.Fatalf("corrupt file was not quarantined: %v", err)
	}
	if st.Health() == nil {
		t.Fatal("Health should report the recovery so the user re-syncs")
	}
	// The healthy files still loaded.
	found := false
	for _, tag := range st.ListTags() {
		if tag.Name == "Work" {
			found = true
		}
	}
	if !found {
		t.Fatal("a corrupt messages.json must not discard the other cache files")
	}
}

// ---------------------------------------------------------------------------
// Socket hardening
// ---------------------------------------------------------------------------

func TestDaemonSocketPermissions(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "sub", "mailclientd.sock")
	ctx, cancel := contextForTest(t)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ListenAndServe(ctx, sock, NewMemoryStore(DemoNow)) }()
	waitForSocket(t, sock)

	st, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode = %04o, want 0600 (another local user could talk to the daemon)", perm)
	}
	dst, err := os.Stat(filepath.Dir(sock))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dst.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("socket directory mode = %04o, want owner-only", perm)
	}
	// A client of the same uid still works.
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Ping(); err != nil {
		t.Fatal(err)
	}
	_ = cli.Close()
	cancel()
	<-done
}

func TestSecondDaemonCannotStealTheSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "mailclientd.sock")
	ctx, cancel := contextForTest(t)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ListenAndServe(ctx, sock, NewMemoryStore(DemoNow)) }()
	waitForSocket(t, sock)

	ctx2, cancel2 := contextForTest(t)
	defer cancel2()
	err := ListenAndServe(ctx2, sock, NewMemoryStore(DemoNow))
	if err == nil {
		t.Fatal("a second daemon must not take over a live socket")
	}
	if !strings.Contains(err.Error(), "already serving") {
		t.Fatalf("unexpected error: %v", err)
	}
	// The first daemon still works.
	cli, derr := DialWait(sock, 2*time.Second)
	if derr != nil {
		t.Fatal(derr)
	}
	if err := cli.Ping(); err != nil {
		t.Fatalf("original daemon was disturbed: %v", err)
	}
	_ = cli.Close()
	cancel()
	<-done
}

// ---------------------------------------------------------------------------
// Token store
// ---------------------------------------------------------------------------

func TestTokenStoreRoundTripWithSecretTool(t *testing.T) {
	// With secret-tool on PATH the master key used to be derived one way on
	// write and another on read, so every stored token became garbage.
	bin := t.TempDir()
	store := filepath.Join(bin, "kv")
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"  lookup) cat " + store + " 2>/dev/null ;;\n" +
		"  store) cat > " + store + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(bin, "secret-tool"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ts := NewTokenStore(filepath.Join(bin, "secrets"))
	want := TokenBlob{
		AccessToken: "at", RefreshToken: "rt", Provider: "google",
		ClientID: "cid.apps.googleusercontent.com", ClientSecret: "csec",
		Expiry: time.Now().Add(time.Hour).Round(time.Second),
	}
	if err := ts.Put("ada@example.com", want); err != nil {
		t.Fatal(err)
	}
	got, err := ts.Get("ada@example.com")
	if err != nil {
		t.Fatalf("token could not be decrypted with secret-tool present: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Fatalf("round trip lost the token: %+v", got)
	}
	if got.ClientID != want.ClientID || got.ClientSecret != want.ClientSecret {
		t.Fatalf("client credentials must be stored for refresh: %+v", got)
	}
	// A second store instance (fresh process) reads the same key.
	ts2 := NewTokenStore(filepath.Join(bin, "secrets"))
	if _, err := ts2.Get("ada@example.com"); err != nil {
		t.Fatalf("second open failed: %v", err)
	}
	// File modes.
	for _, name := range []string{"master.key", safeID("ada@example.com") + ".tok"} {
		st, err := os.Stat(filepath.Join(bin, "secrets", name))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %04o, want 0600", name, st.Mode().Perm())
		}
	}
}

func TestTokenStoreWorksWithoutSecretTool(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin) // no secret-tool
	ts := NewTokenStore(filepath.Join(bin, "secrets"))
	if err := ts.Put("k", TokenBlob{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	got, err := ts.Get("k")
	if err != nil || got.AccessToken != "at" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func waitForSocket(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s never appeared", sock)
}

func contextForTest(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
