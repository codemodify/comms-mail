package mailcore

import (
	"bufio"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codemodify/comms-mail/internal/svtest"
)

// fakeSMTP takes mail on loopback, without authentication, and keeps
// what each DATA delivered.
type fakeSMTP struct {
	addr string
	mu   sync.Mutex
	got  []string
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().String()}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	say("220 fake")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250 fake")
		case strings.HasPrefix(cmd, "DATA"):
			say("354 go on")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.got = append(f.got, b.String())
			f.mu.Unlock()
			say("250 queued")
		case strings.HasPrefix(cmd, "QUIT"):
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (f *fakeSMTP) delivered() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

// sendingStore is an account that sends through f, its secrets in a
// stand-in secretvault, with Sent and Drafts folders; its IMAP server is
// unreachable, so what would go to the server waits in the Outbox.
func sendingStore(t *testing.T, f *fakeSMTP) (*LocalStore, *svtest.Vault) {
	t.Helper()
	sv := startFakeSecretVault(t, false)
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{ID: "w", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "127.0.0.1:1", TLSMode: string(TLSPlain)},
		SMTP: ServerConfig{Host: f.addr, TLSMode: string(TLSPlain)}}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UseStore(StoreSecretVault, ""); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.Folders = append(st.Folders,
		Folder{ID: "w/sent", AccountID: "w", Name: "Sent", Kind: FolderSent, Remote: "Sent"},
		Folder{ID: "w/drafts", AccountID: "w", Name: "Drafts", Kind: FolderDrafts, Remote: "Drafts"})
	st.mu.Unlock()
	return st, sv
}

func outgoing(p *Protection) Message {
	return Message{From: "Ada <ada@example.com>", To: "bob@example.org", Subject: "The plan",
		Body: "Meet at the blue door.\n", Protect: p}
}

func sentCopy(t *testing.T, st *LocalStore) Message {
	t.Helper()
	ms := st.ListMessages("w/sent")
	if len(ms) != 1 {
		t.Fatalf("Sent holds %d", len(ms))
	}
	m, _ := st.GetMessage(ms[0].ID)
	return m
}

// Signed and encrypted, the message that goes out — and the copy in Sent
// — is what secretvault made of it, never the plain text.
func TestSendEncryptedThroughSecretVault(t *testing.T) {
	smtp := startFakeSMTP(t)
	st, sv := sendingStore(t, smtp)
	sv.OwnKeys("ada@example.com")
	if k := st.SigningKeys("Ada <ada@example.com>"); !k.Available || !k.CanSign || k.Locked {
		t.Fatalf("keys %+v", k)
	}
	if k := st.SigningKeys("someone@else.example"); k.CanSign {
		t.Fatalf("a key for an address it does not hold: %+v", k)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil); err != nil {
		t.Fatal(err)
	}
	got := smtp.delivered()
	if len(got) != 1 || !strings.Contains(got[0], "multipart/encrypted") || strings.Contains(got[0], "blue door") {
		t.Fatalf("delivered %q", got)
	}
	c := sv.Composes()
	if len(c) != 1 || !c[0].Sign || !c[0].Encrypt || !strings.Contains(string(c[0].Message), "blue door") {
		t.Fatalf("secretvault was asked %+v", c)
	}
	if m := sentCopy(t, st); !m.Encrypted || m.Body != "" || m.Protect != nil {
		t.Fatalf("Sent keeps %+v", m)
	}
}

// Signing whenever there is a key: signed when secretvault holds one for
// From, sent as it is when it does not.
func TestSendSignedIfThereIsAKey(t *testing.T) {
	smtp := startFakeSMTP(t)
	st, sv := sendingStore(t, smtp)
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{SignIfKey: true}), nil); err != nil {
		t.Fatal(err)
	}
	if len(sv.Composes()) != 0 || !strings.Contains(smtp.delivered()[0], "blue door") {
		t.Fatal("no key: it should go as written")
	}
	sv.OwnKeys("ada@example.com")
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{SignIfKey: true}), nil); err != nil {
		t.Fatal(err)
	}
	if c := sv.Composes(); len(c) != 1 || !c[0].Sign || c[0].Encrypt {
		t.Fatalf("secretvault was asked %+v", c)
	}
	if got := smtp.delivered(); !strings.Contains(got[1], "multipart/signed") {
		t.Fatalf("delivered %q", got[1])
	}
}

// While secretvault is locked the message waits in the Outbox, and goes,
// protected, once it unlocks.
func TestSendWaitsWhileSecretVaultIsLocked(t *testing.T) {
	smtp := startFakeSMTP(t)
	st, sv := sendingStore(t, smtp)
	sv.OwnKeys("ada@example.com")
	sv.Lock()
	waitFor(t, "the lock was not noticed", func() bool { return st.SecretsStatus().Locked })
	if k := st.SigningKeys("ada@example.com"); !k.Locked {
		t.Fatalf("locked: %+v", k)
	}
	_, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true, Encrypt: true}), nil)
	var q *QueuedError
	if !errors.As(err, &q) || len(smtp.delivered()) != 0 || len(sv.Composes()) != 0 || sv.Unlocks() != 0 {
		t.Fatalf("locked: %v, delivered %d", err, len(smtp.delivered()))
	}
	sv.Unlock()
	waitFor(t, "the unlock was not noticed", func() bool { return st.SecretsStatus().Ready })
	if _, err := st.FlushOutbox(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the waiting message did not go", func() bool { return len(smtp.delivered()) == 1 })
	if got := smtp.delivered()[0]; !strings.Contains(got, "multipart/encrypted") || strings.Contains(got, "blue door") {
		t.Fatalf("delivered %q", got)
	}
}

// What cannot be sent as asked is the writer's to see, and nothing goes:
// Bcc with encryption, a refusal, signing without secretvault.
func TestSendRefusedNotSent(t *testing.T) {
	smtp := startFakeSMTP(t)
	st, sv := sendingStore(t, smtp)
	m := outgoing(&Protection{Encrypt: true})
	m.Bcc = "carol@example.net"
	if _, err := st.SendViaSMTP("w", "", m, nil); err == nil || !strings.Contains(err.Error(), "Bcc") {
		t.Fatalf("Bcc: %v", err)
	}
	sv.RefuseCompose("no key for bob@example.org")
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Encrypt: true}), nil); err == nil || !strings.Contains(err.Error(), "bob@example.org") {
		t.Fatalf("refused: %v", err)
	}
	sv.RefuseCompose("")
	if err := st.UseStore(StoreEncrypted, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SendViaSMTP("w", "", outgoing(&Protection{Sign: true}), nil); err == nil || !strings.Contains(err.Error(), "secretvault") {
		t.Fatalf("without secretvault: %v", err)
	}
	if k := st.SigningKeys("ada@example.com"); k.Available {
		t.Fatalf("keys without secretvault %+v", k)
	}
	if len(smtp.delivered()) != 0 || len(st.ListOutbox()) != 0 {
		t.Fatalf("something went: %d delivered, %d waiting", len(smtp.delivered()), len(st.ListOutbox()))
	}
}

// A draft of a message to be encrypted stays on this machine: it is not
// written to the server's Drafts, and a copy saved there before Encrypt
// was chosen is taken off it.
func TestEncryptedDraftsStayHere(t *testing.T) {
	smtp := startFakeSMTP(t)
	st, _ := sendingStore(t, smtp)
	appends := func() int {
		n := 0
		for _, op := range st.ListOutbox() {
			if op.Kind == "append" {
				n++
			}
		}
		return n
	}
	plain := outgoing(nil)
	if _, err := st.Append("w/drafts", plain); err != nil {
		t.Fatal(err)
	}
	if appends() != 1 {
		t.Fatal("a plain draft is not on its way to the server") // the server is unreachable: it waits
	}
	id, err := st.Append("w/drafts", outgoing(&Protection{Encrypt: true}))
	if err != nil {
		t.Fatal(err)
	}
	if appends() != 1 {
		t.Fatal("a draft to be encrypted was queued for the server")
	}
	// One that was on the server, saved again to be encrypted.
	st.mu.Lock()
	i, _ := st.indexLocked(id)
	st.Messages[i].UID = 42
	st.mu.Unlock()
	if _, err := st.update(id, outgoing(&Protection{Encrypt: true})); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.GetMessage(id); m.UID != 0 || m.Protect == nil || !m.Protect.Encrypt {
		t.Fatalf("after saving to be encrypted: %+v", m)
	}
	if _, err := st.FlushOutbox(); err != nil && !strings.Contains(err.Error(), "connect") {
		t.Logf("flush: %v", err)
	}
}
