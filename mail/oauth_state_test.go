package mail

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProvider is a minimal OAuth server: /auth records the request it was
// sent, /token issues a token.
type fakeProvider struct {
	srv       *httptest.Server
	lastAuth  url.Values
	exchanges int
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	p := &fakeProvider{}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		p.lastAuth = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		p.exchanges++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-access", "refresh_token": "tok-refresh",
			"expires_in": 3600, "token_type": "Bearer",
		})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	t.Setenv(EnvOAuthGoogleAuth, p.srv.URL+"/auth")
	t.Setenv(EnvOAuthGoogleToken, p.srv.URL+"/token")
	t.Setenv(EnvOAuthGoogleDevice, p.srv.URL+"/device")
	return p
}

func startLoopbackSession(t *testing.T, h *oauthHub, req oauthReq) (OAuthStart, *oauthSession) {
	t.Helper()
	st, err := h.start(req)
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	sess := h.sess[st.SessionID]
	h.mu.Unlock()
	if sess == nil {
		t.Fatal("session not registered")
	}
	return st, sess
}

func TestOAuthAuthorizeURLCarriesStateAndPKCE(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	dir := t.TempDir()
	t.Setenv(EnvData, dir)
	SetDefaultTokenStore(NewTokenStore(filepath.Join(dir, "secrets")))
	newFakeProvider(t)

	h := newOAuthHub()
	st, sess := startLoopbackSession(t, h, oauthReq{
		Provider: "google", Address: "ada@gmail.com", ClientID: "cid",
	})
	u, err := url.Parse(st.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") == "" {
		t.Fatal("authorize URL must carry a state nonce")
	}
	if q.Get("state") != sess.state {
		t.Fatalf("state %q does not match the session", q.Get("state"))
	}
	if len(q.Get("state")) < 16 {
		t.Fatalf("state is too short to be unguessable: %q", q.Get("state"))
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("PKCE missing: %v", q)
	}
	if q.Get("code_challenge") != pkceChallenge(sess.verifier) {
		t.Fatal("code_challenge does not match the verifier")
	}
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
}

func TestOAuthCallbackRejectsForeignState(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	dir := t.TempDir()
	t.Setenv(EnvData, dir)
	SetDefaultTokenStore(NewTokenStore(filepath.Join(dir, "secrets")))
	p := newFakeProvider(t)

	h := newOAuthHub()
	_, sess := startLoopbackSession(t, h, oauthReq{
		Provider: "google", Address: "ada@gmail.com", ClientID: "cid",
	})

	// Any local process (or a web page the browser is pointed at) can reach
	// the loopback port. Without the state check it could complete the flow
	// with its own code and bind the attacker's mailbox to this account.
	resp, err := http.Get(sess.redirect + "?code=attacker-code")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback without state returned %d: %s", resp.StatusCode, body)
	}
	if p.exchanges != 0 {
		t.Fatal("an attacker's code must never be exchanged")
	}
	select {
	case <-sess.done:
		t.Fatal("the session must stay open")
	default:
	}

	// A wrong state is refused too.
	resp, err = http.Get(sess.redirect + "?code=x&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback with a wrong state returned %d", resp.StatusCode)
	}
	if p.exchanges != 0 {
		t.Fatal("code exchanged despite a state mismatch")
	}
}

func TestOAuthCallbackAcceptsMatchingStateOnce(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	dir := t.TempDir()
	t.Setenv(EnvData, dir)
	SetDefaultTokenStore(NewTokenStore(filepath.Join(dir, "secrets")))
	p := newFakeProvider(t)

	h := newOAuthHub()
	st, sess := startLoopbackSession(t, h, oauthReq{
		Provider: "google", Address: "ada@gmail.com", ClientID: "cid", ClientSecret: "csec",
	})
	resp, err := http.Get(sess.redirect + "?code=good&state=" + url.QueryEscape(sess.state))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

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
		time.Sleep(20 * time.Millisecond)
	}
	if !poll.Done || poll.Error != "" {
		t.Fatalf("poll = %+v", poll)
	}
	if p.exchanges != 1 {
		t.Fatalf("expected one exchange, got %d", p.exchanges)
	}
	// The client id must be stored with the token so a refresh an hour later
	// works even when the env vars are gone.
	tok, err := DefaultTokenStore().Get("ada@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	if tok.ClientID != "cid" || tok.ClientSecret != "csec" {
		t.Fatalf("client credentials were not persisted: %+v", tok)
	}
	if tok.RefreshToken != "tok-refresh" {
		t.Fatalf("refresh token = %q", tok.RefreshToken)
	}
}

func TestOAuthCallbackEscapesProviderError(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	dir := t.TempDir()
	t.Setenv(EnvData, dir)
	SetDefaultTokenStore(NewTokenStore(filepath.Join(dir, "secrets")))
	newFakeProvider(t)

	h := newOAuthHub()
	_, sess := startLoopbackSession(t, h, oauthReq{
		Provider: "google", Address: "ada@gmail.com", ClientID: "cid",
	})
	evil := `<script>alert(1)</script>`
	resp, err := http.Get(sess.redirect + "?error=" + url.QueryEscape(evil) + "&state=" + url.QueryEscape(sess.state))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Contains(string(body), "<script>") {
		t.Fatalf("provider error was echoed unescaped: %s", body)
	}
	if !strings.Contains(string(body), "&lt;script&gt;") {
		t.Fatalf("expected the escaped form, got: %s", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type = %q", ct)
	}
}

func TestRefreshUsesStoredClientID(t *testing.T) {
	p := newFakeProvider(t)
	// No env client id at all: the stored one must carry the refresh.
	t.Setenv(EnvOAuthGoogleClient, "")
	t.Setenv(EnvOAuthGoogleSecret, "")
	fresh, err := refreshOAuthToken(TokenBlob{
		Provider: "google", RefreshToken: "rt", ClientID: "stored-cid", ClientSecret: "stored-secret",
	})
	if err != nil {
		t.Fatalf("refresh failed without env credentials: %v", err)
	}
	if fresh.AccessToken != "tok-access" {
		t.Fatalf("token = %+v", fresh)
	}
	if fresh.ClientID != "stored-cid" {
		t.Fatal("the client id must be carried forward for the next refresh")
	}
	if p.exchanges != 1 {
		t.Fatalf("exchanges = %d", p.exchanges)
	}
	// With neither stored nor env credentials the refresh fails loudly.
	if _, err := refreshOAuthToken(TokenBlob{Provider: "google", RefreshToken: "rt"}); err == nil {
		t.Fatal("refresh without any client id must error")
	}
}
