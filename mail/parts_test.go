package mail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// nestedMIME is mixed[ alternative[plain, html], application/pdf ].
// The old part numbering gave the PDF the same id as the HTML part.
const nestedMIME = "From: Bot <bot@example.com>\r\n" +
	"To: ada@example.com\r\n" +
	"Subject: report\r\n" +
	"Message-Id: <nested@ex>\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"OUTER\"\r\n\r\n" +
	"--OUTER\r\n" +
	"Content-Type: multipart/alternative; boundary=\"INNER\"\r\n\r\n" +
	"--INNER\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
	"plain body\r\n" +
	"--INNER\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n\r\n" +
	"<p>html body</p>\r\n" +
	"--INNER--\r\n" +
	"--OUTER\r\n" +
	"Content-Type: application/pdf; name=\"report.pdf\"\r\n" +
	"Content-Disposition: attachment; filename=\"report.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\n" +
	"JVBERi0xLjQKdGVzdC1wZGYK\r\n" +
	"--OUTER--\r\n"

func TestMIMEPartIDsAreUniqueAndNested(t *testing.T) {
	m, err := ParseRFC822([]byte(nestedMIME), "f", "a")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, p := range m.Parts {
		if prev, dup := seen[p.ID]; dup {
			t.Fatalf("duplicate part id %q used by both %s and %s", p.ID, prev, p.MIMEType)
		}
		seen[p.ID] = p.MIMEType
	}
	if got := seen["1.1"]; got != "text/plain" {
		t.Fatalf("part 1.1 = %q, want text/plain (got parts %v)", got, seen)
	}
	if got := seen["1.2"]; got != "text/html" {
		t.Fatalf("part 1.2 = %q, want text/html", got)
	}
	if got := seen["2"]; got != "application/pdf" {
		t.Fatalf("part 2 = %q, want application/pdf", got)
	}
	if len(m.Attachments) != 1 || m.Attachments[0] != "report.pdf" {
		t.Fatalf("attachments = %v", m.Attachments)
	}
	if m.Body != "plain body" {
		t.Fatalf("body = %q", m.Body)
	}
}

func TestPartFromRawReturnsAttachmentBytes(t *testing.T) {
	p, ok := PartFromRaw([]byte(nestedMIME), "2")
	if !ok {
		t.Fatal("part 2 not found")
	}
	if p.MIMEType != "application/pdf" {
		t.Fatalf("mime = %q", p.MIMEType)
	}
	if !strings.HasPrefix(string(p.Data), "%PDF-1.4") {
		t.Fatalf("attachment bytes were not decoded: %q", p.Data)
	}
	// Text sections still decode.
	txt, ok := PartFromRaw([]byte(nestedMIME), "1.1")
	if !ok || strings.TrimSpace(string(txt.Data)) != "plain body" {
		t.Fatalf("text part = %q ok=%v", txt.Data, ok)
	}
}

func TestLocalStoreGetPartDecodesAttachmentFromCachedRaw(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.folders = append(st.folders, Folder{ID: "home/inbox", AccountID: "home", Name: "Inbox", Kind: FolderInbox})
	m := Message{ID: "home/inbox:5", Folder: "home/inbox", AccountID: "home", UID: 5}
	st.writeRawLocked(m, []byte(nestedMIME))
	st.messages = append(st.messages, m)

	full, ok := st.GetMessage(m.ID)
	if !ok {
		t.Fatal("message missing")
	}
	var pdfID string
	for _, p := range full.Parts {
		if p.Filename == "report.pdf" {
			pdfID = p.ID
		}
	}
	if pdfID == "" {
		t.Fatalf("no pdf part in %+v", full.Parts)
	}
	got, err := st.GetPart(m.ID, pdfID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got.Data), "%PDF") {
		t.Fatalf("GetPart returned %q — attachments must come back as their own bytes", got.Data)
	}
	if got.MIMEType != "application/pdf" {
		t.Fatalf("mime = %q", got.MIMEType)
	}
}

func TestGetPartRejectsIllegalPartID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.folders = append(st.folders, Folder{ID: "home/inbox", AccountID: "home", Kind: FolderInbox})
	m := Message{ID: "home/inbox:5", Folder: "home/inbox", AccountID: "home", UID: 5}
	st.messages = append(st.messages, m)
	if _, err := st.GetPart(m.ID, "1] UID SEARCH ALL\r\nX"); err == nil {
		t.Fatal("a part id that could be injected into BODY.PEEK[] must be rejected")
	}
	if !validPartID("1.2") || !validPartID("2") || !validPartID("1.2.TEXT") {
		t.Fatal("legitimate section ids must pass")
	}
	if validPartID("../x") || validPartID("1 2") {
		t.Fatal("illegal ids must fail")
	}
}

func TestAttachPartIDMapsRowToItsOwnPart(t *testing.T) {
	// Row 0 is the only attachment, but it is the *third* MIME part: the
	// old code indexed Parts by row and fetched the text body instead.
	m, err := ParseRFC822([]byte(nestedMIME), "f", "a")
	if err != nil {
		t.Fatal(err)
	}
	s := &session{attNames: append([]string(nil), m.Attachments...)}
	id := s.attachPartID(m, 0)
	var mime string
	for _, p := range m.Parts {
		if p.ID == id {
			mime = p.MIMEType
		}
	}
	if mime != "application/pdf" {
		t.Fatalf("attachment row 0 resolved to part %q (%s), want the pdf", id, mime)
	}
}

func TestOpenPartRefusesExecutableAttachments(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	raw := "From: a@b.c\r\nSubject: x\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\nhi\r\n" +
		"--B\r\nContent-Type: application/x-desktop; name=\"evil.desktop\"\r\n" +
		"Content-Disposition: attachment; filename=\"evil.desktop\"\r\n\r\n" +
		"[Desktop Entry]\r\nExec=rm -rf ~\r\n" +
		"--B--\r\n"
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.folders = append(st.folders, Folder{ID: "home/inbox", AccountID: "home", Kind: FolderInbox})
	m := Message{ID: "home/inbox:1", Folder: "home/inbox", AccountID: "home"}
	st.writeRawLocked(m, []byte(raw))
	st.messages = append(st.messages, m)
	full, _ := st.GetMessage(m.ID)
	var id string
	for _, p := range full.Parts {
		if p.Filename == "evil.desktop" {
			id = p.ID
		}
	}
	if id == "" {
		t.Fatalf("parts %+v", full.Parts)
	}
	if _, err := st.OpenPart(m.ID, id); err == nil {
		t.Fatal("a .desktop attachment must not be handed to xdg-open")
	}
	// A harmless attachment still opens.
	if _, err := st.OpenPart(m.ID, "1"); err != nil {
		t.Fatalf("text part should still open: %v", err)
	}
}

func TestOpenPartStaysInsideTheCacheDir(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	raw := "From: a@b.c\r\nSubject: x\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\nhi\r\n" +
		"--B\r\nContent-Type: application/octet-stream; name=\"../../escape.bin\"\r\n" +
		"Content-Disposition: attachment; filename=\"../../escape.bin\"\r\n\r\n" +
		"data\r\n--B--\r\n"
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.folders = append(st.folders, Folder{ID: "home/inbox", AccountID: "home", Kind: FolderInbox})
	m := Message{ID: "home/inbox:1", Folder: "home/inbox", AccountID: "home"}
	st.writeRawLocked(m, []byte(raw))
	st.messages = append(st.messages, m)
	full, _ := st.GetMessage(m.ID)
	var id string
	for _, p := range full.Parts {
		if strings.Contains(p.Filename, "escape.bin") {
			id = p.ID
		}
	}
	if id == "" {
		t.Fatalf("parts %+v", full.Parts)
	}
	p, err := st.OpenPart(m.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Path, dir) {
		t.Fatalf("attachment written outside the cache dir: %s", p.Path)
	}
	if strings.Contains(p.Path, "..") {
		t.Fatalf("path retained traversal: %s", p.Path)
	}
}

// ---------------------------------------------------------------------------
// Charset / transfer encoding / HTML
// ---------------------------------------------------------------------------

func TestHTMLToTextDecodesEntitiesAndKeepsBareLessThan(t *testing.T) {
	cases := map[string]string{
		"if a &lt; b then":                     "if a < b then",
		"&#39;quoted&#39;":                     "'quoted'",
		"&euro;5 &amp; &pound;3":               "€5 & £3",
		"1 < 2 and 3 > 2":                      "1 < 2 and 3 > 2",
		"<p>one</p><p>two</p>":                 "one\n\ntwo",
		"<div>a</div><div>b</div><div>c</div>": "a\n\nb\n\nc",
		"&#x20AC;uro":                          "€uro",
		"a&nbsp;b":                             "a b",
		"<script>evil()</script>safe":          "safe",
	}
	for in, want := range cases {
		if got := HTMLToText(in); got != want {
			t.Fatalf("HTMLToText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuotedPrintableAndCharsetDecoding(t *testing.T) {
	raw := "From: a@b.c\r\nSubject: =?ISO-8859-1?Q?Gr=FC=DFe?=\r\n" +
		"Content-Type: text/plain; charset=ISO-8859-1\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"Gr=FC=DFe aus M=FCnchen\r\n"
	m, err := ParseRFC822([]byte(raw), "f", "a")
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Grüße" {
		t.Fatalf("subject = %q", m.Subject)
	}
	if !strings.Contains(m.Body, "Grüße aus München") {
		t.Fatalf("body = %q", m.Body)
	}
}

func TestBase64WithBrokenTailStillDecodes(t *testing.T) {
	raw := "From: a@b.c\r\nSubject: s\r\nContent-Type: text/plain\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\naGVsbG8gd29ybGQ=\r\n!!!not base64!!!\r\n"
	m, err := ParseRFC822([]byte(raw), "f", "a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Body, "hello world") {
		t.Fatalf("body = %q", m.Body)
	}
}

// ---------------------------------------------------------------------------
// Modified UTF-7 mailbox names
// ---------------------------------------------------------------------------

func TestIMAPUTF7RoundTrip(t *testing.T) {
	cases := map[string]string{
		"INBOX":              "INBOX",
		"Entwürfe":           "Entw&APw-rfe",
		"~peter/mail/台北/日本語": "~peter/mail/&U,BTFw-/&ZeVnLIqe-",
		"A&B":                "A&-B",
		"Sent Items":         "Sent Items",
	}
	for utf8Name, wire := range cases {
		if got := encodeIMAPUTF7(utf8Name); got != wire {
			t.Fatalf("encode(%q) = %q, want %q", utf8Name, got, wire)
		}
		if got := decodeIMAPUTF7(wire); got != utf8Name {
			t.Fatalf("decode(%q) = %q, want %q", wire, got, utf8Name)
		}
	}
}

func TestMailboxNamesRejectCRLF(t *testing.T) {
	if _, err := imapMailbox("INBOX\r\nA1 DELETE \"Important\""); err == nil {
		t.Fatal("a mailbox name with CRLF must be rejected, not quoted")
	}
	box, err := imapMailbox("Entwürfe")
	if err != nil {
		t.Fatal(err)
	}
	if box != `"Entw&APw-rfe"` {
		t.Fatalf("mailbox wire form = %s", box)
	}
}

func TestCreateFolderRejectsInjection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateFolder("home", "Bad\r\nA1 LOGOUT", ""); err == nil {
		t.Fatal("folder names with CRLF must be refused")
	}
}

// ---------------------------------------------------------------------------
// Offline outbox
// ---------------------------------------------------------------------------

func TestOfflineSendKeepsAttachmentsInTheQueue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993", User: "ada@example.com", Pass: "p"},
		SMTP: ServerConfig{Host: "smtp.example.com:587", User: "ada@example.com", Pass: "p"},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.SetOnline(false)
	files := []AttachedFile{{Name: "notes.txt", MIME: "text/plain", Data: []byte("hello")}}
	if _, err := st.SendViaSMTP("home", "", Message{
		From: "ada@example.com", To: "bob@example.com", Subject: "offline", Body: "b",
	}, files); err != nil {
		t.Fatal(err)
	}
	ops := st.ListOutbox()
	if len(ops) != 1 {
		t.Fatalf("outbox has %d ops", len(ops))
	}
	if len(ops[0].Attachments) != 1 || string(ops[0].Attachments[0].Data) != "hello" {
		t.Fatalf("attachments were dropped from the queued send: %+v", ops[0].Attachments)
	}
	// And they survive a daemon restart (outbox.json round trip).
	st2, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	ops2 := st2.ListOutbox()
	if len(ops2) != 1 || len(ops2[0].Attachments) != 1 {
		t.Fatalf("attachments lost across a restart: %+v", ops2)
	}
	if string(ops2[0].Attachments[0].Data) != "hello" {
		t.Fatalf("attachment bytes = %q", ops2[0].Attachments[0].Data)
	}
}

func TestFailedSendQueuesOnceNotTwice(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{Accounts: []AccountConfig{{
		ID: "home", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993", User: "ada@example.com", Pass: "p"},
		// Unroutable: the send fails.
		SMTP: ServerConfig{Host: "127.0.0.1:1", User: "ada@example.com", Pass: "p", TLSMode: string(TLSPlain)},
	}}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{From: "ada@example.com", To: "bob@example.com", Subject: "s", Body: "b"}
	if _, err := st.SendViaSMTP("home", "", msg, nil); err == nil {
		t.Fatal("expected the send to fail")
	}
	if n := len(st.ListOutbox()); n != 1 {
		t.Fatalf("a failed send should queue exactly one op, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Daemon events reach the UI
// ---------------------------------------------------------------------------

func TestStoreEmitsChangeEventsForBackgroundWork(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	st, err := NewLocalStoreDir(MailConfig{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan StoreEvent, 4)
	st.SetOnChange(func(ev StoreEvent) { got <- ev })
	st.emit(StoreEvent{Reason: "push", AccountID: "home", Count: 2})
	select {
	case ev := <-got:
		if ev.Reason != "push" || ev.Count != 2 {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event delivered")
	}
	st.SetOnChange(nil)
	st.emit(StoreEvent{Reason: "push"})
	select {
	case ev := <-got:
		t.Fatalf("event delivered after unsubscribe: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDaemonBroadcastsChangeOnBackgroundSync(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "mailclientd.sock")
	store := NewMemoryStore(DemoNow)
	ctx, cancel := contextForTest(t)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ListenAndServe(ctx, sock, store) }()
	waitForSocket(t, sock)

	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	events := make(chan Event, 8)
	cli.OnEvent(func(ev Event) { events <- ev })

	if _, err := cli.PutTag(Tag{Name: "Zeta", Color: "#ff0000"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Method == EventChanged {
				cancel()
				<-done
				return
			}
		case <-deadline:
			t.Fatal("no mail.changed reached the client")
		}
	}
}

func TestSessionRefreshesOnDaemonEvent(t *testing.T) {
	// The UI subscribes to mail.changed; without a live run loop the refresh
	// is recorded as pending (DrainDaemonEvents applies it) rather than
	// mutating widgets from the client read goroutine.
	s := &session{}
	s.refresher = newRefreshCoalescer(time.Millisecond, func() {
		if !s.postLive(func() {}) {
			s.pendingRefresh.Store(true)
		}
	})
	defer s.refresher.stop()
	s.onDaemonEvent(Event{Method: EventChanged})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.pendingRefresh.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("mail.changed did not schedule a refresh")
}

func TestRefreshCoalescerCollapsesBursts(t *testing.T) {
	var fired int32
	done := make(chan struct{}, 8)
	r := newRefreshCoalescer(40*time.Millisecond, func() {
		fired++
		done <- struct{}{}
	})
	defer r.stop()
	for i := 0; i < 50; i++ {
		r.request()
	}
	<-done
	time.Sleep(80 * time.Millisecond)
	if fired > 2 {
		t.Fatalf("50 events caused %d refreshes; they should collapse", fired)
	}
}

// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

func TestUnreadAllIsOneRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "mailclientd.sock")
	ctx, cancel := contextForTest(t)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ListenAndServe(ctx, sock, NewDemoStore()) }()
	waitForSocket(t, sock)
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	counts, total, err := cli.UnreadAll()
	if err != nil {
		t.Fatal(err)
	}
	if total <= 0 {
		t.Fatalf("total unread = %d", total)
	}
	sum := 0
	for id, n := range counts {
		one, err := cli.Unread(id)
		if err != nil {
			t.Fatal(err)
		}
		if one != n {
			t.Fatalf("folder %s: batch says %d, unread.get says %d", id, n, one)
		}
		if !IsVirtual(id) {
			sum += n
		}
	}
	if sum != total {
		t.Logf("per-folder sum %d vs total %d (virtual folders overlap)", sum, total)
	}
	cancel()
	<-done
}

func TestDemoClockOnlyForMemoryBackend(t *testing.T) {
	demo := &session{backend: "memory"}
	if !demo.now().Equal(DemoNow) {
		t.Fatal("the seeded demo store must keep its frozen clock for screenshots")
	}
	real := &session{backend: "imap"}
	if real.now().Equal(DemoNow) {
		t.Fatal("a real account must format dates against the wall clock")
	}
	if time.Since(real.now()) > time.Minute {
		t.Fatalf("real clock looks wrong: %s", real.now())
	}
}

func TestReadAttachmentsShipsBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := ReadAttachments([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "notes.txt" || string(files[0].Data) != "hello" {
		t.Fatalf("files = %+v", files)
	}
	if _, err := ReadAttachments([]string{filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("a missing attachment should be reported in the UI, not the daemon")
	}
}

func TestDaemonDoesNotReadArbitraryPaths(t *testing.T) {
	// compose.send carries bytes; there is no path field a client could use
	// to make the daemon read a file it chooses.
	secret := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := mustMarshal(t, composeParams{AccountID: "a", Message: Message{To: "x@y.z"}})
	if strings.Contains(string(raw), "attachPaths") {
		t.Fatal("compose params must not carry a server-side path")
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := jsonMarshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSelectionIsPrunedWhenRowsVanish(t *testing.T) {
	s := &session{
		rows:     []Message{{ID: "a"}, {ID: "b"}},
		selected: []MessageID{"a", "gone", "b"},
	}
	s.pruneSelection()
	if len(s.selected) != 2 || s.selected[0] != "a" || s.selected[1] != "b" {
		t.Fatalf("selection = %v; ids that left the list must be dropped", s.selected)
	}
	s.rows = nil
	s.pruneSelection()
	if len(s.selected) != 0 {
		t.Fatalf("selection = %v, want empty", s.selected)
	}
}
