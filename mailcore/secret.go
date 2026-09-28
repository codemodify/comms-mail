package mailcore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TokenBlob is a persisted OAuth token (never written to mail.json).
type TokenBlob struct {
	Provider     string    `json:"provider"`
	AccountKey   string    `json:"accountKey"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	TokenType    string    `json:"tokenType,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	// ClientID / ClientSecret are stored with the token so a refresh an hour
	// later works without the env vars being set again.
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

// TokenStore keeps OAuth tokens. Once the vault (vault.go) exists they are
// entries in it, encrypted under the owner's passphrase, and need it
// unlocked. Before that — an install that has not yet set a passphrase —
// they are AES-GCM files under DataDir()/secrets with the key in
// master.key beside them, which protects nothing from a program that can
// read the directory; setting the passphrase moves them into the vault
// and deletes those files (LocalStore.CreateVault).
type TokenStore struct {
	dir string
	mu  sync.Mutex
}

// defaultTokenStore is an atomic pointer so tests (and the isolation helper)
// can redirect the store without racing the lazy initialiser.
var defaultTokenStore atomic.Pointer[TokenStore]

// DefaultTokenStore is the process-wide encrypted token file store.
// DefaultTokenStore is the process-wide encrypted token file store.
// DefaultTokenStore is the process-wide encrypted token file store.
func DefaultTokenStore() *TokenStore {
	if s := defaultTokenStore.Load(); s != nil {
		return s
	}
	fresh := NewTokenStore(filepath.Join(DataDir(), "secrets"))
	if defaultTokenStore.CompareAndSwap(nil, fresh) {
		return fresh
	}
	return defaultTokenStore.Load()
}

// SetDefaultTokenStore redirects the process-wide store (tests, isolation).
func SetDefaultTokenStore(s *TokenStore) {
	defaultTokenStore.Store(s)
}
func NewTokenStore(dir string) *TokenStore {
	return &TokenStore{dir: dir}
}

// vault is the vault beside the token files (DefaultVault for the
// default store).
func (s *TokenStore) vault() *Vault { return OpenVault(filepath.Join(s.dir, "vault.json")) }

func (s *TokenStore) Put(key string, tok TokenBlob) error {
	if s == nil || key == "" {
		return fmt.Errorf("mail: token key required")
	}
	tok.AccountKey = key
	raw, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	if v := s.vault(); v.Exists() {
		return v.Update(map[string]string{tokenSecret(key): string(raw)})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keyb, err := s.masterKey()
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(keyb)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nonce, nonce, raw, nil)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(s.dir, safeID(key)+".tok")
	return WriteFileAtomic(path, sealed, 0o600)
}

func (s *TokenStore) Get(key string) (TokenBlob, error) {
	if s == nil || key == "" {
		return TokenBlob{}, fmt.Errorf("mail: token key required")
	}
	if v := s.vault(); v.Exists() {
		if !v.Unlocked() {
			return TokenBlob{}, ErrLocked
		}
		raw, ok := v.Get(tokenSecret(key))
		if !ok {
			return TokenBlob{}, fmt.Errorf("mail: no stored token for %s", key)
		}
		var tok TokenBlob
		if err := json.Unmarshal([]byte(raw), &tok); err != nil {
			return TokenBlob{}, err
		}
		return tok, nil
	}
	return s.legacyGet(key)
}

// legacyGet reads a token file of an install without a vault.
func (s *TokenStore) legacyGet(key string) (TokenBlob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, safeID(key)+".tok")
	sealed, err := os.ReadFile(path)
	if err != nil {
		return TokenBlob{}, err
	}
	keyb, err := s.masterKey()
	if err != nil {
		return TokenBlob{}, err
	}
	block, err := aes.NewCipher(keyb)
	if err != nil {
		return TokenBlob{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return TokenBlob{}, err
	}
	ns := gcm.NonceSize()
	if len(sealed) < ns {
		return TokenBlob{}, fmt.Errorf("mail: token blob too short")
	}
	plain, err := gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
	if err != nil {
		return TokenBlob{}, fmt.Errorf("mail: decrypt token: %w", err)
	}
	var tok TokenBlob
	if err := json.Unmarshal(plain, &tok); err != nil {
		return TokenBlob{}, err
	}
	return tok, nil
}

func (s *TokenStore) Delete(key string) error {
	if s == nil || key == "" {
		return nil
	}
	if v := s.vault(); v.Exists() {
		return v.Update(nil, tokenSecret(key))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(filepath.Join(s.dir, safeID(key)+".tok"))
}

// legacyTokens reads every token file of an install without a vault, for
// moving them into one; files that do not open are skipped.
func (s *TokenStore) legacyTokens() map[string]TokenBlob {
	out := map[string]TokenBlob{}
	files, _ := filepath.Glob(filepath.Join(s.dir, "*.tok"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".tok")
		tok, err := s.legacyGet(name)
		if err != nil {
			continue
		}
		key := tok.AccountKey
		if key == "" {
			key = name
		}
		out[key] = tok
	}
	return out
}

// removeLegacyFiles deletes the token files and master.key once their
// contents are in the vault, and the copy of the key older builds put in
// the desktop keyring through secret-tool.
func (s *TokenStore) removeLegacyFiles() {
	files, _ := filepath.Glob(filepath.Join(s.dir, "*.tok"))
	for _, f := range append(files, filepath.Join(s.dir, "master.key")) {
		_ = os.Remove(f)
	}
	if _, err := exec.LookPath("secret-tool"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "secret-tool", "clear", "service", "uitoolkit-mail", "attribute", "master").Run()
	}
}

// masterKey returns the 32-byte AES key of the token files of an install
// without a vault: master.key, a hex string (older builds wrote the raw
// bytes). Builds that also stored it through secret-tool always wrote this
// file too, and it is the copy read.
func (s *TokenStore) masterKey() ([]byte, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, "master.key")
	if b, err := os.ReadFile(path); err == nil {
		if k, kerr := decodeMasterKey(strings.TrimSpace(string(b))); kerr == nil {
			return k, nil
		}
		if len(b) == 32 {
			// Keys written by older builds were raw bytes.
			return b, nil
		}
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	hexKey := hex.EncodeToString(key)
	if err := WriteFileAtomic(path, []byte(hexKey), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// decodeMasterKey accepts the 64-char hex form written by this package.
func decodeMasterKey(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	if len(v) != 64 {
		return nil, fmt.Errorf("mail: master key is not 32 hex bytes")
	}
	return hex.DecodeString(v)
}
func resolveAccessToken(cfg ServerConfig, address string) (string, error) {
	if t := strings.TrimSpace(os.Getenv(EnvXOAuth)); t != "" {
		return t, nil
	}
	key := tokenKey(cfg, address)
	tok, err := DefaultTokenStore().Get(key)
	if err != nil {
		return "", fmt.Errorf("imap: AUTH=XOAUTH2 needs %s or a stored refresh token (see docs/mail.md): %w", EnvXOAuth, err)
	}
	if tok.AccessToken != "" && (tok.Expiry.IsZero() || time.Now().Before(tok.Expiry.Add(-60*time.Second))) {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		if tok.AccessToken != "" {
			return tok.AccessToken, nil
		}
		return "", fmt.Errorf("imap: stored OAuth token expired and has no refresh token")
	}
	fresh, err := refreshOAuthToken(tok)
	if err != nil {
		if tok.AccessToken != "" {
			return tok.AccessToken, nil
		}
		return "", err
	}
	_ = DefaultTokenStore().Put(key, fresh)
	return fresh.AccessToken, nil
}

func tokenKey(cfg ServerConfig, address string) string {
	if cfg.tokenKey != "" {
		return cfg.tokenKey
	}
	if u := cfg.Username(address); u != "" {
		return u
	}
	return address
}
