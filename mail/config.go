package mail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Env extras for the real client.
const (
	EnvConfig   = "UITK_MAIL_CONFIG"
	EnvData     = "UITK_MAIL_DATA"
	EnvSMTPHost = "UITK_MAIL_SMTP"
	EnvPassEnv  = "UITK_MAIL_PASS_ENV" // name of env var that holds the password
	EnvXOAuth   = "UITK_MAIL_XOAUTH2"
)

// Incoming protocols persisted on each account.
const (
	ProtoIMAP = "imap"
	ProtoPOP3 = "pop3"
)

// MailConfig is ~/.config/uitoolkit/mail.json (mode 0600).
// IMAP/POP3/SMTP passwords may be stored in plaintext on each ServerConfig
// for now (temporary; a secret store comes later).
type MailConfig struct {
	Accounts []AccountConfig `json:"accounts"`
}

// AccountConfig is one IMAP or POP3 login plus SMTP submission.
type AccountConfig struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Address    string       `json:"address"`
	Protocol   string       `json:"protocol,omitempty"` // "imap" (default) or "pop3"
	IMAP       ServerConfig `json:"imap"`
	POP        ServerConfig `json:"pop,omitempty"`
	SMTP       ServerConfig `json:"smtp"`
	Identities []Identity   `json:"identities,omitempty"`
	Provider   string       `json:"provider,omitempty"` // google, microsoft, ""
}

// NormalizeProtocol maps pop/pop3 → pop3, everything else → imap.
func NormalizeProtocol(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pop", "pop3":
		return ProtoPOP3
	default:
		return ProtoIMAP
	}
}

// IsPOP3 is true when the account retrieves via POP3.
func (a AccountConfig) IsPOP3() bool {
	return NormalizeProtocol(a.Protocol) == ProtoPOP3
}

// Incoming is the retrieve server (POP when protocol is pop3, else IMAP).
func (a AccountConfig) Incoming() ServerConfig {
	if a.IsPOP3() {
		if strings.TrimSpace(a.POP.Host) != "" {
			return a.POP
		}
	}
	return a.IMAP
}

// ServerConfig is a host + how to get the password.
//
//	"password" is stored in mail.json (mode 0600) — temporary plaintext
//	"passEnv": "UITK_MAIL_PASS"  reads os.Getenv when password is empty
//	"user" may differ from the From address
//
// Connection security is one enum — see TLSMode and ServerConfig.Mode:
//
//	"tlsMode": "ssl"       implicit TLS from the first byte (993 / 995 / 465)
//	"tlsMode": "starttls"  cleartext connect then a mandatory upgrade (143 / 110 / 587)
//	"tlsMode": "plain"     no TLS at all — opt-in, never a fallback
//
// The older "tls" / "starttls" booleans are still read (see Mode) and are
// mapped fail-closed: tls:true on a STARTTLS port now upgrades instead of
// silently producing a cleartext session.
type ServerConfig struct {
	Host     string `json:"host"`
	User     string `json:"user,omitempty"`
	Pass     string `json:"password,omitempty"`
	PassEnv  string `json:"passEnv,omitempty"`
	TLSMode  string `json:"tlsMode,omitempty"`  // ssl | starttls | plain
	TLS      *bool  `json:"tls,omitempty"`      // legacy: implicit TLS
	StartTLS *bool  `json:"starttls,omitempty"` // legacy: upgrade after connect
	Auth     string `json:"auth,omitempty"`     // plain (default), login, xoauth2
	tokenKey string // runtime; account id for the encrypted token store
}

func (s ServerConfig) Username(fallback string) string {
	if strings.TrimSpace(s.User) != "" {
		return s.User
	}
	return fallback
}

func (s ServerConfig) Password() string {
	if p := s.Pass; p != "" {
		return p
	}
	name := strings.TrimSpace(s.PassEnv)
	if name == "" {
		name = EnvPass
	}
	return os.Getenv(name)
}

// ConfigPath is the account file.
func ConfigPath() string {
	if p := os.Getenv(EnvConfig); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "uitoolkit", "mail.json")
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, ".config", "uitoolkit", "mail.json")
	}
	return filepath.Join(os.TempDir(), "uitoolkit-mail.json")
}

// DataDir is the on-disk cache root.
func DataDir() string {
	if p := os.Getenv(EnvData); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "uitoolkit", "mail")
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, ".local", "share", "uitoolkit", "mail")
	}
	return filepath.Join(os.TempDir(), "uitoolkit-mail")
}

// SaveConfig writes mail.json with mode 0600 (owner-only). Inline
// passwords are written as-is until a secret store exists.
func SaveConfig(cfg MailConfig) error {
	for i := range cfg.Accounts {
		a, err := SanitizeAccountConfig(cfg.Accounts[i])
		if err != nil {
			return err
		}
		cfg.Accounts[i] = a
	}
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, b, 0o600); err != nil {
		return err
	}
	// A mail.json the user created by hand keeps its original mode through
	// a rename, so force owner-only every time we touch it.
	return os.Chmod(path, 0o600)
}

// SanitizeAccountConfig fills defaults. Inline passwords are kept; passEnv
// that is not a valid environment variable name is ignored.
func SanitizeAccountConfig(a AccountConfig) (AccountConfig, error) {
	a.Protocol = NormalizeProtocol(a.Protocol)
	a.Address = strings.TrimSpace(a.Address)
	a.Name = strings.TrimSpace(a.Name)
	a.IMAP.Host = strings.TrimSpace(a.IMAP.Host)
	a.POP.Host = strings.TrimSpace(a.POP.Host)
	a.SMTP.Host = strings.TrimSpace(a.SMTP.Host)
	a.IMAP.User = strings.TrimSpace(a.IMAP.User)
	a.POP.User = strings.TrimSpace(a.POP.User)
	a.SMTP.User = strings.TrimSpace(a.SMTP.User)
	if a.IsPOP3() && a.POP.Host == "" && a.IMAP.Host != "" {
		a.POP = a.IMAP
		a.POP.Host = strings.TrimSpace(a.IMAP.Host)
	}
	in := a.Incoming()
	if a.Address == "" && in.User == "" {
		return a, fmt.Errorf("mail: address or incoming user required")
	}
	if a.IsPOP3() {
		if a.POP.Host == "" {
			return a, fmt.Errorf("mail: POP3 host required")
		}
	} else if a.IMAP.Host == "" {
		return a, fmt.Errorf("mail: IMAP host required")
	}
	if a.Address == "" {
		a.Address = in.User
	}
	if a.IsPOP3() {
		if a.POP.User == "" {
			a.POP.User = a.Address
		}
		if a.POP.Pass == "" && a.IMAP.Pass != "" {
			a.POP.Pass = a.IMAP.Pass
		}
		sanitizeServerSecret(&a.POP)
	} else if a.IMAP.User == "" {
		a.IMAP.User = a.Address
	}
	if a.Name == "" {
		a.Name = DisplayName(a.Address)
	}
	// The account id reaches the filesystem (raw/<id>/…, meta/…): it must
	// never contain a separator or a dot-dot segment.
	if strings.TrimSpace(a.ID) == "" {
		a.ID = slug(a.Address)
	}
	a.ID = safeID(a.ID)
	sanitizeServerSecret(&a.IMAP)
	in = a.Incoming()
	if a.SMTP.Host == "" {
		a.SMTP.Host = guessSMTP(in.Host)
	}
	if a.SMTP.User == "" {
		a.SMTP.User = in.User
	}
	if a.SMTP.Pass == "" && in.Pass != "" {
		a.SMTP.Pass = in.Pass
	}
	sanitizeServerSecret(&a.SMTP)
	if a.SMTP.PassEnv == "" && in.PassEnv != "" {
		a.SMTP.PassEnv = in.PassEnv
	}
	if len(a.Identities) == 0 {
		a.Identities = []Identity{{
			ID: a.ID + "-default", AccountID: a.ID, Name: a.Name, Address: a.Address, Default: true,
		}}
	}
	for i := range a.Identities {
		if a.Identities[i].AccountID == "" {
			a.Identities[i].AccountID = a.ID
		}
	}
	return a, nil
}

func sanitizeServerSecret(s *ServerConfig) {
	if s == nil {
		return
	}
	if s.Pass != "" {
		if s.PassEnv == "" || s.PassEnv == EnvPass {
			s.PassEnv = ""
			return
		}
		s.PassEnv = envVarName(s.PassEnv)
		if s.PassEnv == EnvPass {
			s.PassEnv = ""
		}
		return
	}
	s.PassEnv = envVarName(s.PassEnv)
}

func envVarName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return EnvPass
	}
	for i, c := range s {
		ok := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return EnvPass
		}
	}
	return s
}

func keepExistingSecrets(a AccountConfig, existing []AccountConfig) AccountConfig {
	for _, old := range existing {
		if old.ID != a.ID {
			continue
		}
		if a.IMAP.Pass == "" {
			a.IMAP.Pass = old.IMAP.Pass
		}
		if a.POP.Pass == "" {
			a.POP.Pass = old.POP.Pass
		}
		if a.SMTP.Pass == "" {
			a.SMTP.Pass = old.SMTP.Pass
		}
		if a.Protocol == "" {
			a.Protocol = old.Protocol
		}
		break
	}
	return a
}

func upsertAccountConfig(list []AccountConfig, a AccountConfig) []AccountConfig {
	for i, x := range list {
		if x.ID == a.ID {
			list[i] = a
			return list
		}
	}
	return append(list, a)
}

func dropAccountConfig(list []AccountConfig, id string) []AccountConfig {
	out := list[:0]
	for _, x := range list {
		if x.ID == id || (id != "" && slug(x.Address) == id) {
			continue
		}
		out = append(out, x)
	}
	return out
}

// LoadConfig reads the JSON file. Missing file is not an error (empty config).
func LoadConfig() (MailConfig, error) {
	path := ConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return MailConfig{}, nil
		}
		return MailConfig{}, err
	}
	var cfg MailConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return MailConfig{}, fmt.Errorf("mail config %s: %w", path, err)
	}
	return cfg, nil
}

// ConfigFromEnv builds a single-account config from UITK_MAIL_* (no file).
func ConfigFromEnv() (MailConfig, error) {
	host := strings.TrimSpace(os.Getenv(EnvHost))
	user := strings.TrimSpace(os.Getenv(EnvUser))
	if host == "" || user == "" {
		return MailConfig{}, fmt.Errorf("imap: set UITK_MAIL_HOST and UITK_MAIL_USER (or a config file)")
	}
	name := strings.TrimSpace(os.Getenv(EnvName))
	if name == "" {
		name = DisplayName(user)
	}
	smtpHost := strings.TrimSpace(os.Getenv(EnvSMTPHost))
	if smtpHost == "" {
		smtpHost = guessSMTP(host)
	}
	passEnv := strings.TrimSpace(os.Getenv(EnvPassEnv))
	if passEnv == "" {
		passEnv = EnvPass
	}
	id := "imap"
	acc := AccountConfig{
		ID: id, Name: name, Address: user, Protocol: ProtoIMAP,
		IMAP: ServerConfig{Host: host, User: user, PassEnv: passEnv},
		SMTP: ServerConfig{Host: smtpHost, User: user, PassEnv: passEnv},
		Identities: []Identity{{
			ID: id + "-default", AccountID: id, Name: name, Address: user, Default: true,
		}},
	}
	return MailConfig{Accounts: []AccountConfig{acc}}, nil
}

func guessSMTP(incoming string) string {
	host, _, _ := strings.Cut(incoming, ":")
	stripped := host
	for _, p := range []string{"imap.", "pop.", "pop3."} {
		stripped = strings.TrimPrefix(stripped, p)
	}
	if stripped == host || stripped == "" {
		base := strings.TrimPrefix(strings.TrimPrefix(incoming, "imap."), "pop.")
		return "smtp." + base
	}
	return "smtp." + stripped + ":587"
}

func accountFromConfig(a AccountConfig, transport string) Account {
	proto := NormalizeProtocol(a.Protocol)
	if transport == "" || transport == ProtoIMAP {
		transport = proto
	}
	return Account{ID: a.ID, Name: a.Name, Address: a.Address, Transport: transport, Protocol: proto}
}

func boolPtrVal(v bool) *bool { return &v }
