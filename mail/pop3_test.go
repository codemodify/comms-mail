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

func TestGuessMailHostsIncludesPOP(t *testing.T) {
	g := GuessMailHosts("ada@gmail.com")
	if g.Protocol != ProtoIMAP || g.POP != "pop.gmail.com:995" {
		t.Fatalf("%+v", g)
	}
	g = GuessMailHosts("ada@example.org")
	if g.IMAP != "imap.example.org:993" || g.POP != "pop.example.org:995" {
		t.Fatalf("%+v", g)
	}
	cands := probeCandidates(ProbeRequest{Address: "ada@example.org", Protocol: ProtoIMAP, Auto: true})
	var sawIMAP, sawPOP bool
	for _, c := range cands {
		if c.Protocol == ProtoIMAP && strings.Contains(c.Host, "imap.example.org") {
			sawIMAP = true
		}
		if c.Protocol == ProtoPOP3 && strings.Contains(c.Host, "pop.example.org") {
			sawPOP = true
		}
	}
	if !sawIMAP || !sawPOP {
		t.Fatalf("candidates %+v", cands)
	}
	typed := probeCandidates(ProbeRequest{Host: "127.0.0.1:1995", Protocol: ProtoPOP3, Auto: false})
	if len(typed) != 1 || typed[0].Host != "127.0.0.1:1995" || typed[0].Protocol != ProtoPOP3 {
		t.Fatalf("typed %+v", typed)
	}
}

func TestSanitizeAccountConfigPOP3(t *testing.T) {
	a, err := SanitizeAccountConfig(AccountConfig{
		Address: "ada@example.com", Protocol: "POP",
		POP: ServerConfig{Host: "pop.example.com:995", Pass: "s3cret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Protocol != ProtoPOP3 || a.POP.User != "ada@example.com" {
		t.Fatalf("%+v", a)
	}
	if a.SMTP.Host == "" || a.SMTP.Pass != "s3cret" {
		t.Fatalf("smtp %+v", a.SMTP)
	}
	// Incoming host on the IMAP field is accepted for POP3.
	b, err := SanitizeAccountConfig(AccountConfig{
		Address: "ada@example.com", Protocol: "pop3",
		IMAP: ServerConfig{Host: "mail.example.com:110", Pass: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.POP.Host != "mail.example.com:110" {
		t.Fatalf("copy incoming %+v", b)
	}
}

func TestProbeIMAPAndPOPAgainstFakeServers(t *testing.T) {
	imapLN, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer imapLN.Close()
	go serveFakeIMAP(t, imapLN)

	popLN, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer popLN.Close()
	go serveFakePOP3(t, popLN, "ada@example.com", "secret", [][]byte{
		[]byte("From: Bot <bot@example.com>\r\nSubject: one\r\nMessage-Id: <1@ex>\r\n\r\nhello\r\n"),
	})

	imap := ProbeAccount(ProbeRequest{
		Address: "ada@example.com", User: "ada@example.com", Password: "secret",
		Protocol: ProtoIMAP, Host: imapLN.Addr().String(), TimeoutMS: 4000,
	})
	if !imap.OK || imap.Protocol != ProtoIMAP || imap.TLSMode != "plain" {
		t.Fatalf("imap probe %+v", imap)
	}
	pop := ProbeAccount(ProbeRequest{
		Address: "ada@example.com", User: "ada@example.com", Password: "secret",
		Protocol: ProtoPOP3, Host: popLN.Addr().String(), TimeoutMS: 4000,
	})
	if !pop.OK || pop.Protocol != ProtoPOP3 || pop.Host != popLN.Addr().String() {
		t.Fatalf("pop probe %+v", pop)
	}
	bad := ProbeAccount(ProbeRequest{
		Address: "ada@example.com", User: "ada@example.com", Password: "wrong",
		Protocol: ProtoPOP3, Host: popLN.Addr().String(), TimeoutMS: 4000,
	})
	if bad.OK {
		t.Fatalf("expected auth fail %+v", bad)
	}
}

func TestPOP3RetrieveIntoLocalStore(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	raw1 := []byte("From: Bot <bot@example.com>\r\nTo: ada@example.com\r\nSubject: first pop\r\nMessage-Id: <pop-1@ex>\r\nDate: Fri, 11 Sep 2026 12:00:00 +0000\r\n\r\nhello pop\r\n")
	raw2 := []byte("From: Kai <kai@example.com>\r\nTo: ada@example.com\r\nSubject: second pop\r\nMessage-Id: <pop-2@ex>\r\nDate: Fri, 11 Sep 2026 12:01:00 +0000\r\n\r\nmore\r\n")
	go serveFakePOP3(t, ln, "ada@example.com", "secret", [][]byte{raw1, raw2})

	dir := t.TempDir()
	tlsOff := false
	cfg := MailConfig{Accounts: []AccountConfig{{
		ID: "home", Name: "Ada", Address: "ada@example.com", Protocol: ProtoPOP3,
		POP:  ServerConfig{Host: ln.Addr().String(), User: "ada@example.com", Pass: "secret", TLS: &tlsOff},
		SMTP: ServerConfig{Host: "smtp.example.com:587"},
	}}}
	s, err := NewLocalStoreDir(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	accts := s.Accounts()
	if len(accts) != 1 || accts[0].Protocol != ProtoPOP3 || ProtocolLabel(accts[0]) != "POP3" {
		t.Fatalf("account %+v", accts)
	}
	res, err := s.Sync("home")
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 2 {
		t.Fatalf("first sync new=%d err=%q", res.New, res.Error)
	}
	inbox := FolderID("home/inbox")
	list := s.ListMessages(inbox)
	if len(list) != 2 {
		t.Fatalf("inbox %d", len(list))
	}
	res, err = s.Sync("home")
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 0 {
		t.Fatalf("uidl dedup new=%d", res.New)
	}
	if len(s.ListMessages(inbox)) != 2 {
		t.Fatal("dedup grew inbox")
	}
}

func TestPutAccountPersistsProtocol(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.json")
	t.Setenv(EnvConfig, path)
	s, err := NewLocalStoreDir(MailConfig{}, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	acct, err := s.PutAccount(AccountConfig{
		Name: "Ada", Address: "ada@example.com", Protocol: ProtoPOP3,
		POP: ServerConfig{Host: "pop.example.com:995", Pass: "pw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if acct.Protocol != ProtoPOP3 || ProtocolLabel(acct) != "POP3" {
		t.Fatalf("%+v", acct)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"protocol": "pop3"`) || !strings.Contains(string(raw), `"pop"`) {
		t.Fatalf("%s", raw)
	}
	file, err := LoadConfig()
	if err != nil || !file.Accounts[0].IsPOP3() {
		t.Fatal(err, file)
	}
	if file.Accounts[0].POP.Password() != "pw" {
		t.Fatalf("pop password %q", file.Accounts[0].POP.Password())
	}
}

func TestRPCAccountsTestAndGuessPOP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveFakeIMAP(t, ln)

	sock, stop, err := StartEmpty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	g, err := cli.GuessHosts("you@yahoo.com")
	if err != nil || g.POP == "" || !strings.Contains(g.POP, "yahoo") {
		t.Fatalf("%+v %v", g, err)
	}
	res, err := cli.TestAccount(ProbeRequest{
		Address: "ada@example.com", Password: "secret",
		Protocol: ProtoIMAP, Host: ln.Addr().String(), TimeoutMS: 4000,
	})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
}

func serveFakePOP3(t *testing.T, ln net.Listener, user, pass string, msgs [][]byte) {
	t.Helper()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go handleFakePOP3(c, user, pass, msgs)
	}
}

func handleFakePOP3(c net.Conn, user, pass string, msgs [][]byte) {
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	write := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}
	write("+OK fake pop3")
	authed := false
	expectUser := user
	for {
		ln, err := r.ReadString('\n')
		if err != nil {
			return
		}
		ln = strings.TrimRight(ln, "\r\n")
		cmd, arg, _ := strings.Cut(ln, " ")
		switch strings.ToUpper(cmd) {
		case "CAPA":
			write("+OK")
			write("UIDL")
			write("USER")
			write(".")
		case "USER":
			if strings.TrimSpace(arg) != expectUser {
				write("-ERR no such user")
				continue
			}
			write("+OK")
		case "PASS":
			if strings.TrimSpace(arg) != pass {
				write("-ERR auth")
				continue
			}
			authed = true
			write("+OK logged in")
		case "STAT":
			if !authed {
				write("-ERR")
				continue
			}
			write(fmt.Sprintf("+OK %d 100", len(msgs)))
		case "UIDL":
			if !authed {
				write("-ERR")
				continue
			}
			write("+OK")
			for i := range msgs {
				write(fmt.Sprintf("%d uid-%d", i+1, i+1))
			}
			write(".")
		case "RETR":
			if !authed {
				write("-ERR")
				continue
			}
			n := 0
			fmt.Sscanf(arg, "%d", &n)
			if n < 1 || n > len(msgs) {
				write("-ERR no msg")
				continue
			}
			write("+OK")
			_, _ = w.Write(msgs[n-1])
			if !strings.HasSuffix(string(msgs[n-1]), "\r\n") {
				_, _ = w.WriteString("\r\n")
			}
			write(".")
		case "QUIT":
			write("+OK bye")
			return
		default:
			write("-ERR " + cmd)
		}
	}
}
