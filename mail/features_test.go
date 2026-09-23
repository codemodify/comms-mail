package mail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

func TestUnifiedInboxAggregates(t *testing.T) {
	s := NewDemoStore()
	ada := s.ListMessages(FolderAdaInbox)
	work := s.ListMessages(FolderWorkInbox)
	uni := s.ListMessages(FolderUnifiedInbox)
	if len(uni) != len(ada)+len(work) {
		t.Fatalf("unified %d want %d+%d", len(uni), len(ada), len(work))
	}
	unread := s.ListMessages(FolderUnifiedUnread)
	if len(unread) == 0 {
		t.Fatal("unified unread")
	}
	star := s.ListMessages(FolderUnifiedStarred)
	if len(star) == 0 {
		t.Fatal("unified starred")
	}
	imp := s.ListMessages(TagFolderID("Important"))
	if len(imp) == 0 {
		t.Fatal("tag Important")
	}
	f, ok := s.GetFolder(FolderUnifiedInbox)
	if !ok || !f.Virtual {
		t.Fatal("virtual get")
	}
	if s.Unread(FolderUnifiedInbox) < 1 {
		t.Fatal("unified unread count")
	}
}

func TestIdentitiesAndTags(t *testing.T) {
	s := NewDemoStore()
	ids := s.Identities("")
	if len(ids) < 3 {
		t.Fatalf("idents %d", len(ids))
	}
	ada := s.Identities(AcctAda)
	if len(ada) < 2 {
		t.Fatal("ada identities")
	}
	tags := s.ListTags()
	if len(tags) < 5 {
		t.Fatal(tags)
	}
	if _, err := s.PutTag(Tag{Name: "Later", Color: "#111111"}); err != nil {
		t.Fatal(err)
	}
	vfs := s.VirtualFolders()
	if len(vfs) < 4 {
		t.Fatalf("virtual %d", len(vfs))
	}
}

func TestFilterRulesApply(t *testing.T) {
	s := NewDemoStore()
	n, err := s.ApplyRules("")
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal("expected some rule hits")
	}
	found := false
	for _, m := range s.ListMessages(FolderAdaInbox) {
		if strings.Contains(m.Subject, "Invoice") && hasTag(m.Tags, "Work") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("invoice not tagged Work")
	}
}

func TestRPCNewMethods(t *testing.T) {
	sock, stop, err := StartDemo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cli, err := DialWait(sock, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	ids, err := cli.Identities("")
	if err != nil || len(ids) < 2 {
		t.Fatalf("idents %v %v", ids, err)
	}
	tags, err := cli.Tags()
	if err != nil || len(tags) < 3 {
		t.Fatalf("tags %v %v", tags, err)
	}
	vf, err := cli.VirtualFolders()
	if err != nil || len(vf) < 3 {
		t.Fatalf("virtual %v %v", vf, err)
	}
	uni, err := cli.ListMessages(FolderUnifiedInbox, Filter{})
	if err != nil || len(uni) < 10 {
		t.Fatalf("unified list %d %v", len(uni), err)
	}
	rules, err := cli.Rules()
	if err != nil || len(rules) < 1 {
		t.Fatalf("rules %v %v", rules, err)
	}
	n, err := cli.ApplyRules("")
	if err != nil {
		t.Fatal(err)
	}
	if n < 0 {
		t.Fatal(n)
	}
	part, err := cli.GetPart(uni[0].ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(part.Data) == 0 && part.Path == "" {
		t.Log("empty part ok for header-only")
	}
	res, err := cli.Sync(AcctAda)
	if err != nil {
		t.Fatal(err)
	}
	if res.New < 0 {
		t.Fatal(res)
	}
}

func TestMIMEParseAndSanitize(t *testing.T) {
	raw := []byte("From: Ada <ada@example.com>\r\n" +
		"To: Kai <kai@example.com>\r\n" +
		"Subject: =?UTF-8?Q?Caf=C3=A9?=\r\n" +
		"Date: Fri, 11 Sep 2026 17:00:00 +0000\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"hello body\r\n")
	m, err := ParseRFC822(raw, FolderAdaInbox, AcctAda)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Subject, "Caf") {
		t.Fatalf("subject %q", m.Subject)
	}
	if !strings.Contains(m.Body, "hello") {
		t.Fatalf("body %q", m.Body)
	}
	html := `<p>Hi</p><script>alert(1)</script><a href="x" onclick="evil()">link</a>`
	if strings.Contains(SanitizeHTML(html), "script") || strings.Contains(SanitizeHTML(html), "onclick") {
		t.Fatalf("sanitize %q", SanitizeHTML(html))
	}
	if !strings.Contains(HTMLToText(html), "Hi") {
		t.Fatal(HTMLToText(html))
	}
	built := BuildRFC822(Message{To: "kai@example.com", Subject: "x", Body: "y"}, Identity{Name: "Ada", Address: "ada@example.com", Signature: "sig"}, nil)
	if !strings.Contains(string(built), "sig") {
		t.Fatal(string(built))
	}
}

func TestGetMessageUsesDiskRawWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	cfg := MailConfig{Accounts: []AccountConfig{{
		ID: "home", Name: "Ada", Address: "ada@example.com",
		Identities: []Identity{{ID: "home-1", Name: "Ada", Address: "ada@example.com", Default: true}},
	}}}
	s, err := NewLocalStoreDir(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := s.CreateFolder("home", "Inbox", "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Append(inbox.ID, Message{Subject: "Cached", From: "kai@example.com", To: "ada@example.com", Body: "hello from disk raw"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.messages {
		if s.messages[i].ID == id {
			s.messages[i].Body = ""
			s.messages[i].HTML = ""
		}
	}
	m, ok := s.GetMessage(id)
	if !ok {
		t.Fatal("missing")
	}
	if !strings.Contains(m.Body, "hello from disk raw") {
		t.Fatalf("body from disk %q", m.Body)
	}
	m2, ok := s.GetMessage(id)
	if !ok || !strings.Contains(m2.Body, "hello from disk raw") {
		t.Fatalf("second get %q ok=%v", m2.Body, ok)
	}
}

func TestLocalStoreDiskRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := MailConfig{Accounts: []AccountConfig{{
		ID: "home", Name: "Ada", Address: "ada@example.com",
		Identities: []Identity{{ID: "home-1", Name: "Ada", Address: "ada@example.com", Default: true}},
	}}}
	s, err := NewLocalStoreDir(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Accounts()) != 1 {
		t.Fatal(s.Accounts())
	}
	if len(s.Identities("")) != 1 {
		t.Fatal(s.Identities(""))
	}
	_, err = s.PutTag(Tag{Name: "Work", Color: "#d35400"})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewLocalStoreDir(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.ListTags()) < 1 {
		t.Fatal("tags persist")
	}
}

func TestDisplayBodyPrefersPlainAndStripsHTML(t *testing.T) {
	plain := DisplayBody(Message{Body: "hello", HTML: "<b>nope</b>"})
	if plain != "hello" {
		t.Fatalf("prefer plain %q", plain)
	}
	only := DisplayBody(Message{HTML: `<p>Hi</p><script>alert(1)</script>`})
	if strings.Contains(only, "<") || strings.Contains(only, "script") || !strings.Contains(only, "Hi") {
		t.Fatalf("strip html %q", only)
	}
	tagged := DisplayBody(Message{Body: "<div>Only HTML</div>"})
	if strings.Contains(tagged, "<div") || !strings.Contains(tagged, "Only HTML") {
		t.Fatalf("body-as-html %q", tagged)
	}
}

func TestOpenStoreEmptyByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv(EnvMail, "")
	t.Setenv(EnvHost, "")
	t.Setenv(EnvUser, "")
	t.Setenv(EnvConfig, filepath.Join(dir, "no-such-mail.json"))
	t.Setenv(EnvData, filepath.Join(dir, "data"))
	s, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend() != "imap" {
		t.Fatalf("backend %s", s.Backend())
	}
	if n := len(s.Accounts()); n != 0 {
		t.Fatalf("expected no accounts, got %d %+v", n, s.Accounts())
	}
}

func TestOpenStoreMemoryStillDemo(t *testing.T) {
	t.Setenv(EnvMail, "memory")
	s, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend() != "memory" || len(s.Accounts()) < 2 {
		t.Fatalf("demo %+v %s", s.Accounts(), s.Backend())
	}
}

func TestPutAccountWritesPasswordMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.json")
	t.Setenv(EnvConfig, path)
	t.Setenv(EnvData, filepath.Join(dir, "data"))
	s, err := NewLocalStoreDir(MailConfig{}, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	acct, err := s.PutAccount(AccountConfig{
		Name: "Ada", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993", User: "ada@example.com", Pass: "super secret password!!"},
		SMTP: ServerConfig{Host: "smtp.example.com:587"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if acct.Address != "ada@example.com" {
		t.Fatal(acct)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mail.json mode %o want 0600", st.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "super secret password!!") || !strings.Contains(string(raw), `"password"`) {
		t.Fatalf("password missing: %s", raw)
	}
	file, err := LoadConfig()
	if err != nil || len(file.Accounts) != 1 {
		t.Fatal(err, file)
	}
	if file.Accounts[0].IMAP.Password() != "super secret password!!" {
		t.Fatalf("IMAP password %q", file.Accounts[0].IMAP.Password())
	}
	if file.Accounts[0].SMTP.Password() != "super secret password!!" {
		t.Fatalf("SMTP should copy IMAP password %q", file.Accounts[0].SMTP.Password())
	}
	if file.Accounts[0].Protocol != ProtoIMAP {
		t.Fatalf("default protocol %q", file.Accounts[0].Protocol)
	}
	if !strings.Contains(string(raw), `"protocol": "imap"`) {
		t.Fatalf("protocol missing: %s", raw)
	}
	rawDir := filepath.Join(dir, "data", "raw", acct.ID)
	_ = os.MkdirAll(rawDir, 0o700)
	_ = os.WriteFile(filepath.Join(rawDir, "x.eml"), []byte("From: x\r\n\r\n"), 0o600)
	if err := s.DeleteAccount(acct.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Accounts()); n != 0 {
		t.Fatalf("store still has %d accounts", n)
	}
	file, err = LoadConfig()
	if err != nil || len(file.Accounts) != 0 {
		t.Fatalf("mail.json after delete %+v %v", file, err)
	}
	if _, err := os.Stat(rawDir); !os.IsNotExist(err) {
		t.Fatalf("raw cache still present: %v", err)
	}
}

func TestServerConfigPasswordPrefersInline(t *testing.T) {
	t.Setenv(EnvPass, "from-env")
	if got := (ServerConfig{}).Password(); got != "from-env" {
		t.Fatalf("default env %q", got)
	}
	if got := (ServerConfig{Pass: "inline"}).Password(); got != "inline" {
		t.Fatalf("inline %q", got)
	}
	t.Setenv("UITK_MAIL_CUSTOM_PASS", "custom")
	if got := (ServerConfig{PassEnv: "UITK_MAIL_CUSTOM_PASS"}).Password(); got != "custom" {
		t.Fatalf("passEnv %q", got)
	}
	a := keepExistingSecrets(
		AccountConfig{ID: "home", IMAP: ServerConfig{Host: "imap.example.com:993"}},
		[]AccountConfig{{ID: "home", IMAP: ServerConfig{Pass: "kept"}, SMTP: ServerConfig{Pass: "kept-smtp"}}},
	)
	if a.IMAP.Pass != "kept" || a.SMTP.Pass != "kept-smtp" {
		t.Fatalf("keep %+v", a)
	}
}

func TestRPCPutAccountAndEmptyFirstRun(t *testing.T) {
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
	accts, err := cli.Accounts()
	if err != nil || len(accts) != 0 {
		t.Fatalf("empty start %v %v", accts, err)
	}
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	got, err := cli.PutAccount(AccountConfig{
		Name: "Ada", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993"},
	})
	if err != nil || got.ID == "" {
		t.Fatal(err, got)
	}
	accts, err = cli.Accounts()
	if err != nil || len(accts) != 1 {
		t.Fatalf("after put %v %v", accts, err)
	}
	if err := cli.DeleteAccount(got.ID); err != nil {
		t.Fatal(err)
	}
	accts, err = cli.Accounts()
	if err != nil || len(accts) != 0 {
		t.Fatalf("after delete %v %v", accts, err)
	}
	file, err := LoadConfig()
	if err != nil || len(file.Accounts) != 0 {
		t.Fatalf("config after delete %+v %v", file, err)
	}
}

func TestFirstRunDialogYesNo(t *testing.T) {
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

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(Open(a, w, cli, AppOptions{ShowFilter: true}))
	a.PumpOnce()
	ov := w.Overlay()
	if ov == nil {
		t.Fatal("expected first-run Yes/No overlay")
	}
	if FirstRunPrompt != "There are no accounts, want to add one?" {
		t.Fatalf("FirstRunPrompt %q", FirstRunPrompt)
	}
	var yes, no bool
	var prompt bool
	widget.Walk(ov, func(c widget.Component) {
		switch x := c.(type) {
		case *widgets.Button:
			if x.Text == "Yes" {
				yes = true
			}
			if x.Text == "No" {
				no = true
			}
		case *widgets.Label:
			if strings.Contains(x.Text, "mail.json") || strings.Contains(x.Text, "0600") || strings.Contains(x.Text, "password") {
				t.Fatalf("first-run dialog must not mention password/storage: %q", x.Text)
			}
			if x.Text == FirstRunPrompt {
				prompt = true
			}
		}
	})
	if !yes || !no || !prompt {
		t.Fatalf("dialog yes=%v no=%v prompt=%v", yes, no, prompt)
	}

	widget.Walk(ov, func(c widget.Component) {
		if b, ok := c.(*widgets.Button); ok && b.Text == "Yes" && b.OnClick != nil {
			b.OnClick()
		}
	})
	a.PumpOnce()
	foundAdd := false
	for _, win := range a.Windows() {
		if strings.Contains(win.Title(), "Add account") {
			foundAdd = true
			win.Close()
		}
	}
	if !foundAdd {
		t.Fatal("Yes did not open add-account")
	}
	w.Close()
}

func TestAddAccountProtocolAndTestButton(t *testing.T) {
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

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := OpenAddAccount(a, cli, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.PumpOnce()
	var imap, pop, test bool
	widget.Walk(mailTree(w), func(c widget.Component) {
		switch x := c.(type) {
		case *widgets.RadioButton:
			if x.Text == "IMAP" {
				imap = true
			}
			if x.Text == "POP3" {
				pop = true
			}
		case *widgets.Button:
			if x.Text == "Test connection" {
				test = true
			}
		}
	})
	if !imap || !pop || !test {
		t.Fatalf("add-account imap=%v pop=%v test=%v", imap, pop, test)
	}
	w.Close()
}

func TestRemoveAccountMenuAndFirstRunAgain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "mail.json"))
	t.Setenv(EnvData, filepath.Join(dir, "data"))
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
	if _, err := cli.PutAccount(AccountConfig{
		Name: "Ada", Address: "ada@example.com",
		IMAP: ServerConfig{Host: "imap.example.com:993"},
	}); err != nil {
		t.Fatal(err)
	}

	a := uitoolkit.New(uitoolkit.Options{Look: style.DarkLook(), Headless: true})
	w, err := a.NewWindow(platform.WindowOptions{
		Title: "Mail", Width: 1280, Height: 800, Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(Open(a, w, cli, AppOptions{ShowFilter: true}))
	a.PumpOnce()
	if w.Overlay() != nil {
		t.Fatal("first-run should not open when an account exists")
	}
	var prefs *widgets.MenuItem
	widget.Walk(mailTree(w), func(c widget.Component) {
		mb, ok := c.(*widgets.MenuBar)
		if !ok {
			return
		}
		for _, m := range mb.Menus() {
			for _, it := range m.Items {
				label, _, _ := widgets.ParseMnemonic(it.Text)
				if label == "Preferences" {
					prefs = it
				}
			}
		}
	})
	if prefs == nil || prefs.OnClick == nil {
		t.Fatal("M → Preferences missing")
	}
	prefs.OnClick()
	a.PumpOnce()
	var pw *app.Window
	for _, win := range a.Windows() {
		if win != w && win.Title() == "Preferences" {
			pw = win
			break
		}
	}
	if pw == nil {
		t.Fatal("Preferences window")
	}
	var remove *widgets.Button
	widget.Walk(pw.Content(), func(c widget.Component) {
		if b, ok := c.(*widgets.Button); ok && strings.Contains(b.Text, "Remove account") && b.OnClick != nil {
			remove = b
		}
	})
	if remove == nil {
		t.Fatal("Preferences missing Remove account")
	}
	remove.OnClick()
	a.PumpOnce()
	ov := pw.Overlay()
	if ov == nil {
		t.Fatal("expected Remove account confirm")
	}
	widget.Walk(ov, func(c widget.Component) {
		if b, ok := c.(*widgets.Button); ok && b.Text == "Yes" && b.OnClick != nil {
			b.OnClick()
		}
	})
	a.PumpOnce()
	accts, err := cli.Accounts()
	if err != nil || len(accts) != 0 {
		t.Fatalf("after remove %v %v", accts, err)
	}
	ov = w.Overlay()
	if ov == nil {
		t.Fatal("expected first-run prompt after last account")
	}
	prompt := false
	widget.Walk(ov, func(c widget.Component) {
		if x, ok := c.(*widgets.Label); ok && strings.Contains(x.Text, "There are no accounts") {
			prompt = true
		}
	})
	if !prompt {
		t.Fatal("first-run copy missing after delete")
	}
	pw.Close()
	w.Close()
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv(EnvHost, "imap.example.com:993")
	t.Setenv(EnvUser, "you@example.com")
	t.Setenv(EnvName, "You")
	cfg, err := ConfigFromEnv()
	if err != nil || len(cfg.Accounts) != 1 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if cfg.Accounts[0].IMAP.Host == "" || cfg.Accounts[0].SMTP.Host == "" {
		t.Fatal(cfg.Accounts[0])
	}
}

func TestChromePrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveChromePrefs(ChromePrefs{CardView: true, Density: "compact", ShowFilter: true})
	p := loadChromePrefs()
	if !p.CardView || p.density() != style.DensityCompact || !p.ShowFilter {
		t.Fatalf("%+v", p)
	}
	if _, err := os.Stat(filepath.Join(dir, "uitoolkit", "mailui.json")); err != nil {
		t.Fatal(err)
	}
}
