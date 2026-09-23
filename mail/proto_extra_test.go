package mail

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// IMAP literals and untagged responses
// ---------------------------------------------------------------------------

func TestIMAPLiteralBodyIsReadWhole(t *testing.T) {
	body := "From: Bot <bot@example.com>\r\nSubject: literal\r\n\r\n" +
		strings.Repeat("line of body text\r\n", 500)
	srv := newScriptIMAP(t, "IMAP4rev1", func(s *imapSession, tag, cmd, line string) bool {
		up := strings.ToUpper(line)
		switch {
		case cmd == "SELECT" || cmd == "EXAMINE":
			s.send("* 1 EXISTS")
			s.send("* OK [UIDVALIDITY 1]")
			s.send("%s OK selected", tag)
			return true
		case strings.Contains(up, "BODY.PEEK[]"):
			// Untagged noise before the literal must not confuse the reader.
			s.send("* 1 FETCH (FLAGS (\\Seen))")
			s.raw(fmt.Sprintf("* 1 FETCH (UID 7 BODY[] {%d}\r\n", len(body)))
			s.raw(body)
			s.raw(")\r\n")
			s.send("%s OK fetch", tag)
			return true
		}
		return false
	})
	c := newIMAPClient(plainIMAPConfig(srv.addr()), "ada@example.com")
	if err := c.connect(); err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if _, err := c.selectBox("INBOX", true); err != nil {
		t.Fatal(err)
	}
	raw, err := c.uidFetchRFC822(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != len(body) {
		t.Fatalf("literal truncated: got %d bytes, want %d", len(raw), len(body))
	}
	if string(raw) != body {
		t.Fatal("literal content differs")
	}
}

func TestIMAPLiteralSizeIsBounded(t *testing.T) {
	// A server claiming an absurd literal must not make us allocate it.
	if _, ok := trailingLiteralSize("* 1 FETCH (BODY[] {999999999999}"); ok {
		t.Fatal("an over-large literal size must be refused")
	}
	if n, ok := trailingLiteralSize("* 1 FETCH (BODY[] {42}"); !ok || n != 42 {
		t.Fatalf("n=%d ok=%v", n, ok)
	}
	if _, ok := trailingLiteralSize("* 1 FETCH (BODY[] {not-a-number}"); ok {
		t.Fatal("non-numeric literal must be refused")
	}
}

func TestListDecodesUTF7MailboxNames(t *testing.T) {
	srv := newScriptIMAP(t, "IMAP4rev1", func(s *imapSession, tag, cmd, line string) bool {
		switch cmd {
		case "LIST":
			s.send(`* LIST (\HasNoChildren) "/" "INBOX"`)
			s.send(`* LIST (\HasNoChildren \Drafts) "/" "Entw&APw-rfe"`)
			s.send("%s OK list", tag)
			return true
		case "LSUB":
			s.send("%s OK lsub", tag)
			return true
		}
		return false
	})
	c := newIMAPClient(plainIMAPConfig(srv.addr()), "ada@example.com")
	if err := c.connect(); err != nil {
		t.Fatal(err)
	}
	defer c.close()
	boxes, err := c.list()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, b := range boxes {
		if b.Name == "Entw&APw-rfe" {
			found = true
			if b.Display != "Entwürfe" {
				t.Fatalf("display name = %q, want Entwürfe", b.Display)
			}
			if folderKindFromIMAP(b.Display, b.Attrs) != FolderDrafts {
				t.Fatal("special-use detection should work on the decoded name")
			}
		}
	}
	if !found {
		t.Fatalf("mailbox missing from %+v", boxes)
	}
}

// ---------------------------------------------------------------------------
// POP3
// ---------------------------------------------------------------------------

// scriptPOP3 serves a fixed conversation.
func scriptPOP3(t *testing.T, capa []string, msgs map[int]string, uidls []string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				w := bufio.NewWriter(c)
				r := bufio.NewReader(c)
				send := func(f string, a ...any) {
					_, _ = fmt.Fprintf(w, f+"\r\n", a...)
					_ = w.Flush()
				}
				send("+OK script pop3")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					fields := strings.Fields(strings.TrimSpace(line))
					if len(fields) == 0 {
						continue
					}
					switch strings.ToUpper(fields[0]) {
					case "CAPA":
						send("+OK caps")
						for _, c := range capa {
							send("%s", c)
						}
						send(".")
					case "USER", "PASS":
						send("+OK")
					case "STAT":
						send("+OK %d 100", len(msgs))
					case "UIDL":
						send("+OK")
						for i, u := range uidls {
							send("%d %s", i+1, u)
						}
						send(".")
					case "RETR":
						n := 0
						_, _ = fmt.Sscanf(fields[1], "%d", &n)
						body, ok := msgs[n]
						if !ok {
							send("-ERR no such message")
							continue
						}
						send("+OK %d octets", len(body))
						for _, l := range strings.Split(body, "\r\n") {
							if strings.HasPrefix(l, ".") {
								l = "." + l
							}
							send("%s", l)
						}
						send(".")
					case "STLS":
						send("-ERR not supported")
					case "QUIT":
						send("+OK bye")
						return
					default:
						send("-ERR unknown")
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestPOP3RefusesWhenSTLSMissing(t *testing.T) {
	addr := scriptPOP3(t, []string{"TOP", "UIDL"}, map[int]string{}, nil)
	cli := newPOP3Client(ServerConfig{
		Host: addr, User: "ada@example.com", Pass: "secret", TLSMode: string(TLSStartTLS),
	}, "ada@example.com")
	err := cli.connect()
	if err == nil {
		cli.close()
		t.Fatal("STARTTLS mode must fail when the server has no STLS")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("error = %v", err)
	}
}

func TestPOP3TruncatedMessageIsAnError(t *testing.T) {
	// A stream that ends without "." must not be cached as a whole message.
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
		w := bufio.NewWriter(c)
		r := bufio.NewReader(c)
		send := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
		send("+OK hi")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			up := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(up, "RETR"):
				send("+OK 100 octets")
				send("From: a@b.c")
				send("Subject: truncated")
				_ = c.Close() // die mid-message
				return
			default:
				send("+OK")
			}
		}
	}()
	cli := newPOP3Client(ServerConfig{Host: ln.Addr().String(), TLSMode: string(TLSPlain)}, "ada@example.com")
	if err := cli.connect(); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.retr(1); err == nil {
		t.Fatal("a truncated RETR must be reported, not returned as a short message")
	}
}

func TestPOP3DotStuffingIsUndone(t *testing.T) {
	body := "From: a@b.c\r\nSubject: dots\r\n\r\nnormal line\r\n.dot stuffed\r\nlast\r\n"
	addr := scriptPOP3(t, []string{"UIDL"}, map[int]string{1: body}, []string{"uid-1"})
	cli := newPOP3Client(ServerConfig{
		Host: addr, User: "ada@example.com", Pass: "secret", TLSMode: string(TLSPlain),
	}, "ada@example.com")
	if err := cli.connect(); err != nil {
		t.Fatal(err)
	}
	defer cli.close()
	raw, err := cli.retr(1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "..dot") {
		t.Fatalf("dot-stuffing was not undone: %q", raw)
	}
	if !strings.Contains(string(raw), ".dot stuffed") {
		t.Fatalf("body = %q", raw)
	}
}

func TestPOP3MessageIDsAreFilesystemSafe(t *testing.T) {
	id := popMessageID("home", "../../etc/passwd")
	if strings.ContainsAny(string(id), "/\\") || strings.Contains(string(id), "..") {
		t.Fatalf("POP3 UIDL leaked into a path: %q", id)
	}
}

// ---------------------------------------------------------------------------
// Config compatibility
// ---------------------------------------------------------------------------

func TestLegacyConfigStillParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.json")
	t.Setenv(EnvConfig, path)
	legacy := `{"accounts":[{"id":"home","address":"ada@example.com",
	  "imap":{"host":"imap.example.com:993","user":"ada","password":"p","tls":true},
	  "smtp":{"host":"smtp.example.com:587","user":"ada","password":"p","starttls":true}}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(cfg.Accounts))
	}
	a := cfg.Accounts[0]
	if got := a.IMAP.Mode(imapPorts); got != TLSImplicit {
		t.Fatalf("legacy tls:true on 993 = %q, want ssl", got)
	}
	if got := a.SMTP.Mode(smtpPorts); got != TLSStartTLS {
		t.Fatalf("legacy starttls:true = %q", got)
	}
	if a.IMAP.Password() != "p" {
		t.Fatal("inline password must still be read")
	}
}

func TestSaveConfigForcesOwnerOnlyMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.json")
	t.Setenv(EnvConfig, path)
	// A hand-written, world-readable file (the docs tell people to create
	// one): saving must tighten it, not preserve 0644.
	if err := os.WriteFile(path, []byte(`{"accounts":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com", Protocol: ProtoIMAP,
		IMAP: ServerConfig{Host: "imap.example.com:993", User: "ada", Pass: "secret"},
	}}}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mail.json mode = %04o, want 0600 (it holds a plaintext password)", perm)
	}
}

func TestSaveConfigIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.json")
	t.Setenv(EnvConfig, path)
	if err := SaveConfig(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993", User: "ada", Pass: "p"},
	}}}); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	var cfg MailConfig
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Socket peer check
// ---------------------------------------------------------------------------

func TestPeerCheckAllowsOwnUID(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "s.sock")
	ln, lock, err := listenSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		ok, err := peerAllowed(c)
		if err != nil || !ok {
			_ = c.Close()
			return
		}
		_, _ = c.Write([]byte("ok\n"))
		_ = c.Close()
	}()
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	n, err := c.Read(buf)
	if err != nil || string(buf[:n]) != "ok\n" {
		t.Fatalf("same-uid client was rejected: n=%d err=%v", n, err)
	}
}

func TestLockFileIsReleasedOnShutdown(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "mailclientd.sock")
	ctx, cancel := contextForTest(t)
	done := make(chan error, 1)
	go func() { done <- ListenAndServe(ctx, sock, NewMemoryStore(DemoNow)) }()
	waitForSocket(t, sock)
	cancel()
	<-done
	// A fresh daemon can take the socket again.
	ctx2, cancel2 := contextForTest(t)
	defer cancel2()
	done2 := make(chan error, 1)
	go func() { done2 <- ListenAndServe(ctx2, sock, NewMemoryStore(DemoNow)) }()
	waitForSocket(t, sock)
	cancel2()
	<-done2
}
