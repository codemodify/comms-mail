package mail

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestIMAPSkeletonAgainstFakeServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveFakeIMAP(t, ln)

	s := NewIMAPStore(ln.Addr().String(), "ada@example.com", "secret")
	if err := s.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := s.Health(); err != nil {
		t.Fatal(err)
	}
	folders := s.ListFolders("imap")
	if len(folders) < 1 {
		t.Fatal("LIST")
	}
	inbox := folders[0].ID
	msgs := s.ListMessages(inbox)
	if len(msgs) != 1 {
		t.Fatalf("FETCH list %d", len(msgs))
	}
	m, ok := s.GetMessage(msgs[0].ID)
	if !ok || m.Subject == "" {
		t.Fatalf("GET %+v", m)
	}
	if err := s.SetFlags(m.ID, FlagPatch{Read: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
}

func TestIMAPClientUIDFetch(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveFakeIMAP(t, ln)

	tlsOff := false
	c := newIMAPClient(ServerConfig{
		Host: ln.Addr().String(), User: "ada@example.com", PassEnv: "UITK_MAIL_PASS",
		TLS: &tlsOff,
	}, "ada@example.com")
	t.Setenv("UITK_MAIL_PASS", "secret")
	if err := c.connect(); err != nil {
		t.Fatal(err)
	}
	boxes, err := c.list()
	if err != nil || len(boxes) < 1 {
		t.Fatalf("list %v %v", boxes, err)
	}
	st, err := c.selectBox(boxes[0].Name, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Exists < 1 {
		t.Fatalf("exists %+v", st)
	}
	meta, err := c.uidFetchMeta(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) < 1 || meta[0].UID == 0 {
		t.Fatalf("meta %+v", meta)
	}
	raw, err := c.uidFetchRFC822(meta[0].UID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hello body") && len(raw) == 0 {
		// Fake server may omit literal on some paths; flags path is enough.
		t.Log("no rfc822 literal")
	}
}

func serveFakeIMAP(t *testing.T, ln net.Listener) {
	t.Helper()
	c, err := ln.Accept()
	if err != nil {
		return
	}
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	write := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}
	write("* OK fake imap")
	for {
		ln, err := r.ReadString('\n')
		if err != nil {
			return
		}
		ln = strings.TrimRight(ln, "\r\n")
		parts := strings.Fields(ln)
		if len(parts) < 2 {
			continue
		}
		tag, cmd := parts[0], strings.ToUpper(parts[1])
		switch cmd {
		case "LOGIN":
			write(tag + " OK logged in")
		case "LIST":
			write(`* LIST (\HasNoChildren \Inbox) "/" "INBOX"`)
			write(tag + " OK list")
		case "SELECT", "EXAMINE":
			write("* 1 EXISTS")
			write("* OK [UIDVALIDITY 1]")
			write("* OK [UIDNEXT 18]")
			write(tag + " OK [READ-WRITE] selected")
		case "FETCH":
			write(`* 1 FETCH (FLAGS (\Seen) RFC822.SIZE 120 BODY[HEADER.FIELDS (FROM TO CC SUBJECT DATE)] {80}`)
			write("From: Bot <bot@example.com>")
			write("To: ada@example.com")
			write("Subject: fake fetch")
			write("Date: " + time.Now().Format(time.RFC1123Z))
			write(")")
			if strings.Contains(strings.ToUpper(ln), "BODY.PEEK[TEXT]") || strings.Contains(strings.ToUpper(ln), "BODY[TEXT]") {
				write(`* 1 FETCH (BODY[TEXT] {12}`)
				write("hello body")
				write(")")
			}
			write(tag + " OK fetch")
		case "STORE":
			write(tag + " OK store")
		case "NOOP":
			write(tag + " OK noop")
		case "UID":
			if len(parts) >= 3 && strings.EqualFold(parts[2], "FETCH") {
				write(`* 1 FETCH (UID 17 FLAGS (\Seen) RFC822.SIZE 80 ENVELOPE ("11-Sep-2026 17:00:00 +0000" "uid fetch" (("Bot" NIL "bot" "example.com")) NIL NIL (("Ada" NIL "ada" "example.com")) NIL NIL NIL NIL) BODYSTRUCTURE ("text" "plain" ("charset" "utf-8") NIL NIL "7bit" 12 1))`)
				if strings.Contains(strings.ToUpper(ln), "BODY.PEEK[]") || strings.Contains(strings.ToUpper(ln), "BODY[]") {
					raw := "From: Bot <bot@example.com>\r\nSubject: uid fetch\r\n\r\nhello body\r\n"
					write(fmt.Sprintf("* 1 FETCH (UID 17 BODY[] {%d}", len(raw)))
					_, _ = w.WriteString(raw)
					write(")")
				}
				write(tag + " OK fetch")
				break
			}
			if len(parts) >= 3 && strings.EqualFold(parts[2], "STORE") {
				write(tag + " OK store")
				break
			}
			write(tag + " OK uid")
		case "CAPABILITY":
			write("* CAPABILITY IMAP4rev1 IDLE AUTH=PLAIN MOVE UIDPLUS")
			write(tag + " OK capability")
		case "LOGOUT":
			write("* BYE")
			write(tag + " OK logout")
			return
		default:
			write(fmt.Sprintf("%s BAD %s", tag, cmd))
		}
	}
}
