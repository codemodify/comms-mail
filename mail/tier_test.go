package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGuessMailHosts(t *testing.T) {
	g := GuessMailHosts("ada@gmail.com")
	if g.Provider != "google" || !strings.Contains(g.IMAP, "gmail") {
		t.Fatalf("%+v", g)
	}
	g = GuessMailHosts("ada@outlook.com")
	if g.Provider != "microsoft" {
		t.Fatalf("%+v", g)
	}
	g = GuessMailHosts("ada@example.org")
	if g.IMAP != "imap.example.org:993" || g.SMTP != "smtp.example.org:587" || g.POP != "pop.example.org:995" {
		t.Fatalf("%+v", g)
	}
}

func TestTokenStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewTokenStore(dir)
	tok := TokenBlob{Provider: "google", AccessToken: "acc", RefreshToken: "ref", Expiry: time.Now().Add(time.Hour)}
	if err := s.Put("home", tok); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("home")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "acc" || got.RefreshToken != "ref" {
		t.Fatalf("%+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "home.tok"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "acc") || strings.Contains(string(raw), "ref") {
		t.Fatal("token stored in plaintext")
	}
}

func TestSearchIndexAND(t *testing.T) {
	idx := newSearchIndex()
	idx.add(Message{ID: "1", Subject: "Invoice September", From: "ap@vendor.example", Body: "please pay"})
	idx.add(Message{ID: "2", Subject: "Lunch plans", From: "kai@paintengine.example", Body: "12:30"})
	hit := idx.query("invoice pay")
	if len(hit) != 1 || hit[0] != "1" {
		t.Fatalf("%v", hit)
	}
	if len(idx.query("zzzznope")) != 0 {
		t.Fatal("miss")
	}
}

func TestThreadGroupingAndMute(t *testing.T) {
	s := NewDemoStore()
	var lunch []Message
	for _, m := range s.ListMessages(FolderAdaInbox) {
		if strings.Contains(m.Subject, "lunch plans") || strings.Contains(m.ThreadID, "lunch") {
			lunch = append(lunch, m)
		}
	}
	if len(lunch) < 2 {
		t.Fatalf("expected threaded lunch messages, got %d", len(lunch))
	}
	tid := lunch[0].ThreadID
	if tid == "" {
		t.Fatal("empty thread id")
	}
	if err := s.MuteThread(tid, true); err != nil {
		t.Fatal(err)
	}
	if len(s.MutedThreads()) != 1 {
		t.Fatal(s.MutedThreads())
	}
	unread := s.ListMessages(FolderUnifiedUnread)
	for _, m := range unread {
		if m.ThreadID == tid {
			t.Fatal("muted thread still in unified unread")
		}
	}
}

func TestVIPAndCategories(t *testing.T) {
	s := NewDemoStore()
	vip := s.ListMessages(FolderVIP)
	if len(vip) == 0 {
		t.Fatal("expected VIP hits (Kai / Thunderbird)")
	}
	if _, err := s.PutVIP(VIP{Address: "iris@notes.example", Name: "Iris"}); err != nil {
		t.Fatal(err)
	}
	if len(s.ListVIPs()) < 3 {
		t.Fatal(s.ListVIPs())
	}
	promo := s.ListMessages(FolderCatPromotions)
	if len(promo) == 0 {
		t.Fatal("expected promotions (morgan override or [promo]/unsubscribe)")
	}
	if err := s.SetSenderCategory("kai@paintengine.example", CatOther); err != nil {
		t.Fatal(err)
	}
	other := s.ListMessages(FolderCatOther)
	found := false
	for _, m := range other {
		if canonAddr(m.From) == "kai@paintengine.example" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("recategorize sender")
	}
}

func TestSmartFolderSavedSearch(t *testing.T) {
	s := NewDemoStore()
	sfs := s.ListSmartFolders()
	if len(sfs) < 2 {
		t.Fatal(sfs)
	}
	inv := s.ListMessages(sfs[0].FolderIDFor())
	if len(inv) == 0 {
		t.Fatal("invoices smart folder empty")
	}
	sf, err := s.PutSmartFolder(SmartFolder{Name: "HiDPI", Filter: Filter{Query: "HiDPI", SubjectOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ListMessages(sf.FolderIDFor())) == 0 {
		t.Fatal("new smart folder")
	}
}

func TestOfflineOutboxFlush(t *testing.T) {
	s := NewDemoStore()
	s.SetOnline(false)
	if s.Online() {
		t.Fatal("still online")
	}
	list := s.ListMessages(FolderAdaInbox)
	if len(list) == 0 {
		t.Fatal("empty inbox")
	}
	if err := s.SetFlags(list[0].ID, FlagPatch{Starred: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if len(s.ListOutbox()) == 0 {
		t.Fatal("expected queued flag")
	}
	s.SetOnline(true)
	n, err := s.FlushOutbox()
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal(n)
	}
	if len(s.ListOutbox()) != 0 {
		t.Fatal(s.ListOutbox())
	}
}

func TestNotifyRules(t *testing.T) {
	s := NewDemoStore()
	p := s.NotifyPrefs()
	if !p.Enabled {
		t.Fatal(p)
	}
	p.VIPOnly = true
	s.PutNotifyPrefs(p)
	if !s.NotifyPrefs().VIPOnly {
		t.Fatal("persist")
	}
	vips := map[string]bool{"kai@paintengine.example": true}
	if !shouldNotify(p, Message{From: "Kai <kai@paintengine.example>"}, vips) {
		t.Fatal("vip should notify")
	}
	if shouldNotify(p, Message{From: "rando@example.com"}, vips) {
		t.Fatal("non-vip should not")
	}
}

func TestClassifySender(t *testing.T) {
	if classifySender(Message{Subject: "60% off sale", Body: "unsubscribe"}, nil) != CatPromotions {
		t.Fatal("promo")
	}
	if classifySender(Message{Subject: "Invoice #12", Body: "payment due"}, nil) != CatTransactions {
		t.Fatal("txn")
	}
	if classifySender(Message{Subject: "Weekly status", From: "bot@github.com"}, nil) != CatUpdates {
		t.Fatal("updates")
	}
	if classifySender(Message{Subject: "Hi", From: "kai@paintengine.example"}, nil) != CatPrimary {
		t.Fatal("primary")
	}
}

func TestOAuthLoopbackFakeProvider(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	dir := t.TempDir()
	t.Setenv(EnvData, dir)
	SetDefaultTokenStore(NewTokenStore(filepath.Join(dir, "secrets")))

	var exchanged bool
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		exchanged = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-access", "refresh_token": "tok-refresh",
			"expires_in": 3600, "token_type": "Bearer",
		})
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not used", 404)
	})
	t.Setenv(EnvOAuthGoogleClient, "test-client")
	t.Setenv(EnvOAuthGoogleAuth, srv.URL+"/auth")
	t.Setenv(EnvOAuthGoogleToken, srv.URL+"/token")
	t.Setenv(EnvOAuthGoogleDevice, srv.URL+"/device")

	h := newOAuthHub()
	// Device flow with a fake device endpoint that immediately returns codes,
	// then token endpoint returns tokens.
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dev", "user_code": "ABCD-EFGH",
			"verification_uri": srv.URL + "/verify", "interval": 1,
		})
	})
	st, err := h.start(oauthReq{Provider: "google", Address: "ada@gmail.com", Flow: "device"})
	if err != nil {
		t.Fatal(err)
	}
	if st.UserCode != "ABCD-EFGH" {
		t.Fatalf("%+v", st)
	}
	deadline := time.Now().Add(3 * time.Second)
	var poll OAuthPoll
	for time.Now().Before(deadline) {
		poll, err = h.poll(st.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if poll.Done {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !poll.Done || poll.Error != "" {
		t.Fatalf("poll %+v exchanged=%v", poll, exchanged)
	}
	if poll.Account.Address != "ada@gmail.com" {
		t.Fatalf("%+v", poll.Account)
	}
	tok, err := DefaultTokenStore().Get("ada-gmail-com")
	if err != nil || tok.AccessToken != "tok-access" {
		t.Fatalf("stored %+v %v", tok, err)
	}
}

func TestRPCTIERAB(t *testing.T) {
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

	g, err := cli.GuessHosts("you@gmail.com")
	if err != nil || g.Provider != "google" {
		t.Fatalf("%+v %v", g, err)
	}
	vips, err := cli.VIPs()
	if err != nil || len(vips) < 1 {
		t.Fatalf("%v %v", vips, err)
	}
	sfs, err := cli.SmartFolders()
	if err != nil || len(sfs) < 1 {
		t.Fatalf("%v %v", sfs, err)
	}
	inv, err := cli.ListMessages(sfs[0].FolderIDFor(), Filter{})
	if err != nil || len(inv) < 1 {
		t.Fatalf("smart list %d %v", len(inv), err)
	}
	vipMail, err := cli.ListMessages(FolderVIP, Filter{})
	if err != nil || len(vipMail) < 1 {
		t.Fatalf("vip %d %v", len(vipMail), err)
	}
	if _, err := cli.PutVIP(VIP{Address: "new.vip@example.com", Name: "New"}); err != nil {
		t.Fatal(err)
	}
	sf, err := cli.PutSmartFolder(SmartFolder{Name: "TestQ", Filter: Filter{Query: "Thunderbird"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.ListMessages(sf.FolderIDFor(), Filter{}); err != nil {
		t.Fatal(err)
	}
	n, err := cli.SetOnline(false)
	if err != nil {
		t.Fatal(err)
	}
	_ = n
	st, err := cli.Status()
	if err != nil || st.Online {
		t.Fatalf("offline status %+v %v", st, err)
	}
	if _, err := cli.SetOnline(true); err != nil {
		t.Fatal(err)
	}
	p, err := cli.NotifyPrefs()
	if err != nil {
		t.Fatal(err)
	}
	p.VIPOnly = true
	if _, err := cli.PutNotifyPrefs(p); err != nil {
		t.Fatal(err)
	}
	if err := cli.SetSenderCategory("morgan@lists.example", CatPromotions); err != nil {
		t.Fatal(err)
	}
}

func TestOpenPartWritesCache(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	s := NewDemoStore()
	list := s.ListMessages(FolderAdaInbox)
	var withAtt Message
	for _, m := range list {
		if m.HasAttach && len(m.Attachments) > 0 {
			withAtt = m
			break
		}
	}
	if withAtt.ID == "" {
		t.Fatal("no attachment in demo")
	}
	p, err := s.OpenPart(withAtt.ID, "att-1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Path == "" {
		t.Fatal("no path")
	}
	if _, err := os.Stat(p.Path); err != nil {
		t.Fatal(err)
	}
}

func TestXOAUTH2String(t *testing.T) {
	raw := "user=ada@gmail.com\x01auth=Bearer abc\x01\x01"
	if !strings.Contains(raw, "Bearer abc") {
		t.Fatal(raw)
	}
}
