package mail

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// OAuth client IDs are supplied by the user (env or wizard). uitoolkit does
// not ship Google/Microsoft app credentials — see docs/mail.md.

const (
	EnvOAuthGoogleClient = "UITK_MAIL_OAUTH_GOOGLE_CLIENT_ID"
	EnvOAuthGoogleSecret = "UITK_MAIL_OAUTH_GOOGLE_CLIENT_SECRET"
	EnvOAuthMSClient     = "UITK_MAIL_OAUTH_MS_CLIENT_ID"
	EnvOAuthMSSecret     = "UITK_MAIL_OAUTH_MS_CLIENT_SECRET"
	EnvOAuthGoogleAuth   = "UITK_MAIL_OAUTH_GOOGLE_AUTH_URL"
	EnvOAuthGoogleToken  = "UITK_MAIL_OAUTH_GOOGLE_TOKEN_URL"
	EnvOAuthGoogleDevice = "UITK_MAIL_OAUTH_GOOGLE_DEVICE_URL"
	EnvOAuthMSAuth       = "UITK_MAIL_OAUTH_MS_AUTH_URL"
	EnvOAuthMSToken      = "UITK_MAIL_OAUTH_MS_TOKEN_URL"
	EnvOAuthMSDevice     = "UITK_MAIL_OAUTH_MS_DEVICE_URL"
)

// OAuthStart is oauth.start result (browser or device flow).
type OAuthStart struct {
	SessionID       string `json:"sessionId"`
	Provider        string `json:"provider"`
	Flow            string `json:"flow"` // loopback | device
	AuthURL         string `json:"authUrl,omitempty"`
	DeviceCode      string `json:"deviceCode,omitempty"`
	UserCode        string `json:"userCode,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	Message         string `json:"message"`
}

// OAuthPoll is oauth.poll result.
type OAuthPoll struct {
	Done    bool    `json:"done"`
	Pending bool    `json:"pending"`
	Error   string  `json:"error,omitempty"`
	Account Account `json:"account,omitempty"`
}

type oauthReq struct {
	Provider     string `json:"provider"`
	Address      string `json:"address"`
	Name         string `json:"name,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	Flow         string `json:"flow,omitempty"` // loopback (default), device
}

type oauthSession struct {
	id       string
	req      oauthReq
	flow     string
	verifier string
	state    string
	redirect string
	authURL  string
	device   string
	userCode string
	verify   string
	interval time.Duration
	mu       sync.Mutex
	done     chan struct{}
	err      error
	account  Account
	cfg      AccountConfig
	started  time.Time
}

type oauthHub struct {
	mu   sync.Mutex
	sess map[string]*oauthSession
}

func newOAuthHub() *oauthHub {
	return &oauthHub{sess: map[string]*oauthSession{}}
}

func (h *oauthHub) start(req oauthReq) (OAuthStart, error) {
	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	if req.Provider == "" {
		req.Provider = providerForAddress(req.Address)
	}
	if req.Provider != "google" && req.Provider != "microsoft" {
		return OAuthStart{}, fmt.Errorf("oauth: provider must be google or microsoft (got %q)", req.Provider)
	}
	fillOAuthClient(&req)
	if req.ClientID == "" {
		return OAuthStart{}, fmt.Errorf("oauth: missing client id — set UITK_MAIL_OAUTH_%s_CLIENT_ID or pass clientId (register an app; see docs/mail.md)", strings.ToUpper(req.Provider))
	}
	flow := strings.ToLower(strings.TrimSpace(req.Flow))
	if flow == "" {
		flow = "loopback"
	}
	s := &oauthSession{
		id: randID(12), req: req, flow: flow,
		done: make(chan struct{}), started: time.Now(),
	}
	s.verifier = pkceVerifier()
	s.state = randID(24)
	h.mu.Lock()
	h.sess[s.id] = s
	h.mu.Unlock()

	switch flow {
	case "device":
		if err := s.startDevice(); err != nil {
			return OAuthStart{}, err
		}
		go s.pollDevice()
	default:
		if err := s.startLoopback(); err != nil {
			s.flow = "device"
			if err2 := s.startDevice(); err2 != nil {
				return OAuthStart{}, fmt.Errorf("oauth: loopback: %v; device: %w", err, err2)
			}
			go s.pollDevice()
		}
	}
	openAuthURL(s.authURL)
	return s.toStart(), nil
}

func (h *oauthHub) poll(id string) (OAuthPoll, error) {
	h.mu.Lock()
	s := h.sess[id]
	h.mu.Unlock()
	if s == nil {
		return OAuthPoll{}, fmt.Errorf("oauth: unknown session")
	}
	select {
	case <-s.done:
		acct, ferr := s.result()
		if ferr != nil {
			return OAuthPoll{Done: true, Error: ferr.Error()}, nil
		}
		return OAuthPoll{Done: true, Account: acct}, nil
	default:
		if time.Since(s.started) > 15*time.Minute {
			return OAuthPoll{Done: true, Error: "oauth: timed out"}, nil
		}
		return OAuthPoll{Pending: true}, nil
	}
}

func (h *oauthHub) takeConfig(id string) (AccountConfig, bool) {
	h.mu.Lock()
	s := h.sess[id]
	h.mu.Unlock()
	if s == nil {
		return AccountConfig{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.ID == "" {
		return AccountConfig{}, false
	}
	return s.cfg, true
}

func (h *oauthHub) cancel(id string) {
	h.mu.Lock()
	delete(h.sess, id)
	h.mu.Unlock()
}

func (s *oauthSession) toStart() OAuthStart {
	msg := "Open the URL in a browser and approve access."
	if s.flow == "device" && s.userCode != "" {
		msg = "Visit " + s.verify + " and enter code " + s.userCode
	}
	return OAuthStart{
		SessionID: s.id, Provider: s.req.Provider, Flow: s.flow,
		AuthURL: s.authURL, DeviceCode: s.device, UserCode: s.userCode,
		VerificationURL: s.verify, Message: msg,
	}
}

func (s *oauthSession) finishOK(tok TokenBlob) {
	addr := strings.TrimSpace(s.req.Address)
	if addr == "" {
		addr = "user@" + s.req.Provider + ".invalid"
	}
	name := strings.TrimSpace(s.req.Name)
	if name == "" {
		name = DisplayName(addr)
	}
	id := safeID(addr)
	hosts := GuessMailHosts(addr)
	if hosts.IMAP == "" {
		if s.req.Provider == "google" {
			hosts.IMAP, hosts.SMTP = "imap.gmail.com:993", "smtp.gmail.com:465"
		} else {
			hosts.IMAP, hosts.SMTP = "outlook.office365.com:993", "smtp.office365.com:587"
		}
	}
	cfg := AccountConfig{
		ID: id, Name: name, Address: addr, Provider: s.req.Provider, Protocol: ProtoIMAP,
		IMAP: ServerConfig{Host: hosts.IMAP, User: addr, Auth: "xoauth2", TLSMode: string(TLSImplicit)},
		SMTP: ServerConfig{Host: hosts.SMTP, User: addr, Auth: "xoauth2"},
	}
	tok.Provider = s.req.Provider
	// The client id is needed an hour later to refresh. A wizard-supplied id
	// used to be forgotten, so refresh failed unless the env var was also set.
	tok.ClientID = s.req.ClientID
	tok.ClientSecret = s.req.ClientSecret
	_ = DefaultTokenStore().Put(id, tok)
	_ = DefaultTokenStore().Put(addr, tok)
	s.mu.Lock()
	s.account = accountFromConfig(cfg, ProtoIMAP)
	s.cfg = cfg
	s.mu.Unlock()
	s.closeDone()
}
func (s *oauthSession) fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
	s.closeDone()
}

func (s *oauthSession) result() (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account, s.err
}
func (s *oauthSession) closeDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func (s *oauthSession) startLoopback() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	s.redirect = fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", port)
	s.authURL = authorizeURL(s.req, s.redirect, pkceChallenge(s.verifier), s.state)
	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	var once sync.Once
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// The loopback port is reachable by any local process and by any web
		// page the browser is told to load, so the state nonce — not the
		// port — is what proves this callback belongs to our flow.
		if got := r.URL.Query().Get("state"); got != s.state {
			http.Error(w, "state mismatch — this callback did not come from the sign-in that started here.", http.StatusBadRequest)
			return
		}
		handled := false
		once.Do(func() { handled = true })
		if !handled {
			_, _ = io.WriteString(w, "This sign-in was already completed. You can close this tab.")
			return
		}
		if errStr := r.URL.Query().Get("error"); errStr != "" {
			// Escaped: the value is attacker-controlled and is echoed back.
			_, _ = io.WriteString(w, "Mail OAuth error: "+sanitizeForDisplay(errStr)+" — you can close this tab.")
			s.fail(fmt.Errorf("oauth: %s", sanitizeForDisplay(errStr)))
			go gracefulClose(srv)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		tok, err := exchangeCode(s.req, code, s.redirect, s.verifier)
		if err != nil {
			_, _ = io.WriteString(w, "Token exchange failed. You can close this tab.")
			s.fail(err)
			go gracefulClose(srv)
			return
		}
		_, _ = io.WriteString(w, "Mail is signed in. You can close this tab.")
		s.finishOK(tok)
		go gracefulClose(srv)
	})
	go func() {
		_ = srv.Serve(ln)
	}()
	go func() {
		select {
		case <-s.done:
		case <-time.After(15 * time.Minute):
		}
		gracefulClose(srv)
	}()
	return nil
}

// gracefulClose lets the browser receive the "you can close this tab" page
// before the loopback listener goes away. Closing the server straight from
// the handler truncated the response.
func gracefulClose(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = srv.Close()
}

// sanitizeForDisplay strips control characters and markup from a value we
// echo back into the browser tab.
func sanitizeForDisplay(v string) string {
	v = html.EscapeString(v)
	var b strings.Builder
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return truncate(b.String(), 200)
}
func (s *oauthSession) startDevice() error {
	ep := deviceURL(s.req.Provider)
	form := url.Values{
		"client_id": {s.req.ClientID},
		"scope":     {oauthScope(s.req.Provider)},
	}
	if s.req.ClientSecret != "" {
		form.Set("client_secret", s.req.ClientSecret)
	}
	var out struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
		ErrorDesc               string `json:"error_description"`
	}
	if err := postForm(ep, form, &out); err != nil {
		return err
	}
	if out.Error != "" {
		return fmt.Errorf("oauth device: %s %s", out.Error, out.ErrorDesc)
	}
	s.device = out.DeviceCode
	s.userCode = out.UserCode
	s.verify = out.VerificationURI
	if out.VerificationURIComplete != "" {
		s.authURL = out.VerificationURIComplete
	} else {
		s.authURL = out.VerificationURI
	}
	s.interval = time.Duration(out.Interval) * time.Second
	if out.Interval <= 0 {
		s.interval = 5 * time.Second
	}
	if s.interval < 200*time.Millisecond {
		s.interval = 200 * time.Millisecond
	}
	return nil
}

func (s *oauthSession) pollDevice() {
	ep := tokenURL(s.req.Provider)
	for time.Since(s.started) < 15*time.Minute {
		time.Sleep(s.interval)
		form := url.Values{
			"client_id":   {s.req.ClientID},
			"device_code": {s.device},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		if s.req.ClientSecret != "" {
			form.Set("client_secret", s.req.ClientSecret)
		}
		tok, err := postToken(ep, form)
		if err != nil {
			if strings.Contains(err.Error(), "authorization_pending") || strings.Contains(err.Error(), "slow_down") {
				continue
			}
			s.fail(err)
			return
		}
		s.finishOK(tok)
		return
	}
	s.fail(fmt.Errorf("oauth: device flow timed out"))
}

func fillOAuthClient(req *oauthReq) {
	if req.ClientID != "" {
		return
	}
	switch req.Provider {
	case "google":
		req.ClientID = strings.TrimSpace(os.Getenv(EnvOAuthGoogleClient))
		if req.ClientSecret == "" {
			req.ClientSecret = strings.TrimSpace(os.Getenv(EnvOAuthGoogleSecret))
		}
	case "microsoft":
		req.ClientID = strings.TrimSpace(os.Getenv(EnvOAuthMSClient))
		if req.ClientSecret == "" {
			req.ClientSecret = strings.TrimSpace(os.Getenv(EnvOAuthMSSecret))
		}
	}
}

func authorizeURL(req oauthReq, redirect, challenge, state string) string {
	u, _ := url.Parse(authURL(req.Provider))
	q := u.Query()
	q.Set("client_id", req.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("response_type", "code")
	q.Set("scope", oauthScope(req.Provider))
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if req.Provider == "google" {
		q.Set("access_type", "offline")
		q.Set("prompt", "consent")
	} else {
		q.Set("prompt", "select_account")
	}
	if req.Address != "" {
		q.Set("login_hint", req.Address)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func exchangeCode(req oauthReq, code, redirect, verifier string) (TokenBlob, error) {
	form := url.Values{
		"client_id":     {req.ClientID},
		"code":          {code},
		"redirect_uri":  {redirect},
		"grant_type":    {"authorization_code"},
		"code_verifier": {verifier},
	}
	if req.ClientSecret != "" {
		form.Set("client_secret", req.ClientSecret)
	}
	return postToken(tokenURL(req.Provider), form)
}

func refreshOAuthToken(tok TokenBlob) (TokenBlob, error) {
	req := oauthReq{Provider: tok.Provider, ClientID: tok.ClientID, ClientSecret: tok.ClientSecret}
	fillOAuthClient(&req)
	if req.ClientID == "" {
		return TokenBlob{}, fmt.Errorf("oauth: cannot refresh without client id")
	}
	form := url.Values{
		"client_id":     {req.ClientID},
		"refresh_token": {tok.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	if req.ClientSecret != "" {
		form.Set("client_secret", req.ClientSecret)
	}
	fresh, err := postToken(tokenURL(req.Provider), form)
	if err != nil {
		return TokenBlob{}, err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	fresh.Provider = tok.Provider
	fresh.ClientID = req.ClientID
	fresh.ClientSecret = req.ClientSecret
	return fresh, nil
}
func postToken(ep string, form url.Values) (TokenBlob, error) {
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := postForm(ep, form, &raw); err != nil {
		return TokenBlob{}, err
	}
	if raw.Error != "" {
		return TokenBlob{}, fmt.Errorf("oauth: %s %s", raw.Error, raw.ErrorDesc)
	}
	if raw.AccessToken == "" {
		return TokenBlob{}, fmt.Errorf("oauth: empty access_token")
	}
	tok := TokenBlob{
		AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken,
		TokenType: raw.TokenType, Scope: raw.Scope,
	}
	if raw.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}
	return tok, nil
}

// oauthHTTP is the client used for every token endpoint call: bounded, and
// never following a redirect to a non-HTTPS location.
var oauthHTTP = &http.Client{
	Timeout: 60 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" && !isLoopbackHost(req.URL.Host) {
			return fmt.Errorf("oauth: refusing redirect to %s", req.URL.Scheme)
		}
		if len(via) >= 5 {
			return fmt.Errorf("oauth: too many redirects")
		}
		return nil
	},
}

func postForm(ep string, form url.Values, dest any) error {
	resp, err := oauthHTTP.PostForm(ep, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dest); err != nil {
		return fmt.Errorf("oauth: decode %s: %w (%s)", ep, err, truncate(string(b), 200))
	}
	return nil
}
func authURL(provider string) string {
	if provider == "microsoft" {
		if u := os.Getenv(EnvOAuthMSAuth); u != "" {
			return u
		}
		return "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
	}
	if u := os.Getenv(EnvOAuthGoogleAuth); u != "" {
		return u
	}
	return "https://accounts.google.com/o/oauth2/v2/auth"
}

func tokenURL(provider string) string {
	if provider == "microsoft" {
		if u := os.Getenv(EnvOAuthMSToken); u != "" {
			return u
		}
		return "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	}
	if u := os.Getenv(EnvOAuthGoogleToken); u != "" {
		return u
	}
	return "https://oauth2.googleapis.com/token"
}

func deviceURL(provider string) string {
	if provider == "microsoft" {
		if u := os.Getenv(EnvOAuthMSDevice); u != "" {
			return u
		}
		return "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode"
	}
	if u := os.Getenv(EnvOAuthGoogleDevice); u != "" {
		return u
	}
	return "https://oauth2.googleapis.com/device/code"
}

func oauthScope(provider string) string {
	if provider == "microsoft" {
		return "offline_access https://outlook.office.com/IMAP.AccessAsUser.All https://outlook.office.com/SMTP.Send"
	}
	return "https://mail.google.com/"
}

func pkceVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func openAuthURL(u string) {
	if u == "" || os.Getenv("UITK_MAIL_NO_OPEN") != "" {
		return
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return
	}
	if _, err := exec.LookPath("xdg-open"); err != nil {
		return
	}
	_ = exec.Command("xdg-open", u).Start()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
