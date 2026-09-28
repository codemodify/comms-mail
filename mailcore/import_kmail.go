package mailcore

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// KMail keeps its configuration in several KConfig (INI) files, and the
// layout has shifted across versions, so this import is best-effort: it
// reads the incoming servers from the Akonadi IMAP/POP resources, the
// outgoing server from mailtransports, and names and signatures from
// emailidentities, and pairs them by e-mail address where it can. What it
// cannot place it still offers, so an imported account may need a look
// before it is saved. Passwords live in KWallet and are never read.

// iniFile is a parsed KConfig/INI file: section → key → value.
type iniFile map[string]map[string]string

func parseINI(r io.Reader) iniFile {
	out := iniFile{}
	section := ""
	out[section] = map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[section][strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// get returns the first non-empty value for any of keys in section, matching
// key names case-insensitively (KConfig keys vary in case across versions).
func (f iniFile) get(section string, keys ...string) string {
	sec := f[section]
	if sec == nil {
		return ""
	}
	for _, want := range keys {
		for k, v := range sec {
			if strings.EqualFold(k, want) && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// kmailSafetyTLS maps a KMail encryption/safety word to a TLSMode.
func kmailSafetyTLS(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "SSL", "SSL/TLS":
		return string(TLSImplicit)
	case "TLS", "STARTTLS":
		return string(TLSStartTLS)
	case "NONE", "":
		return string(TLSPlain)
	default:
		return string(TLSImplicit)
	}
}

// kmailIdentities reads names, addresses and signatures from emailidentities.
func kmailIdentities(f iniFile) map[string]Identity {
	byEmail := map[string]Identity{}
	for section, kv := range f {
		if !strings.HasPrefix(section, "Identity") {
			continue
		}
		email := f.get(section, "Email Address", "EmailAddress", "Email")
		if email == "" {
			continue
		}
		sig := kv["Signature"]
		if sig == "" {
			// Some versions nest the text in an [Identity #N][Signature]
			// group with an "Inline Text" key.
			sig = f.get(section+"][Signature", "Inline Text", "Text")
		}
		byEmail[strings.ToLower(email)] = Identity{
			ID:        "kmail-" + strings.ToLower(email),
			Name:      f.get(section, "Name", "Full Name"),
			Address:   email,
			Signature: htmlToPlainSignature(sig),
			Default:   strings.EqualFold(kv["Default Identity"], "true"),
		}
	}
	return byEmail
}

// kmailDefaultSMTP reads the default outgoing server from mailtransports.
func kmailDefaultSMTP(f iniFile) ServerConfig {
	def := f.get("General", "default-transport", "DefaultTransport")
	var best ServerConfig
	for section := range f {
		if !strings.HasPrefix(section, "Transport") {
			continue
		}
		host := f.get(section, "host", "Host")
		if host == "" {
			continue
		}
		cfg := ServerConfig{
			Host:    hostPort(host, f.get(section, "port", "Port")),
			User:    f.get(section, "user", "User", "userName"),
			TLSMode: kmailSafetyTLS(f.get(section, "encryption", "Encryption", "safety")),
		}
		if best.Host == "" {
			best = cfg
		}
		// Prefer the one whose id matches default-transport (the section is
		// "Transport <id>").
		if def != "" && strings.TrimSpace(strings.TrimPrefix(section, "Transport")) == def {
			return cfg
		}
	}
	return best
}

// kmailIncoming reads one incoming account from an Akonadi resource file.
func kmailIncoming(f iniFile, pop bool) (ServerConfig, string, bool) {
	host := f.get("network", "ImapServer", "Server", "Host", "host")
	if host == "" {
		host = f.get("General", "host", "Host", "Server")
	}
	if host == "" {
		return ServerConfig{}, "", false
	}
	portKeys := []string{"ImapPort", "Port", "port"}
	if pop {
		portKeys = []string{"Port", "port"}
	}
	safety := f.get("network", "Safety", "Encryption", "safety")
	if safety == "" && (f.get("network", "UseSSL", "useSSL") != "" || f.get("General", "UseSSL") != "") {
		safety = "SSL"
	}
	port := firstNonEmpty(f.get("network", portKeys...), f.get("General", portKeys...))
	user := firstNonEmpty(
		f.get("network", "UserName", "User", "Login"),
		f.get("General", "login", "Login", "UserName"))
	cfg := ServerConfig{
		Host:    hostPort(host, port),
		User:    user,
		TLSMode: kmailSafetyTLS(safety),
	}
	return cfg, cfg.User, true
}

// KMailConfigDir is the directory KMail's config files live in.
func KMailConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config")
}

func readINIFile(path string) (iniFile, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	return parseINI(f), true
}

// ImportKMail reads KMail's configuration from dir (KMailConfigDir when
// empty) and returns the accounts it can piece together, passwords blank.
func ImportKMail() ([]ImportedAccount, error) {
	dir := KMailConfigDir()
	if dir == "" {
		return nil, fmt.Errorf("no config directory")
	}
	idents := map[string]Identity{}
	if f, ok := readINIFile(filepath.Join(dir, "emailidentities")); ok {
		idents = kmailIdentities(f)
	}
	var smtp ServerConfig
	if f, ok := readINIFile(filepath.Join(dir, "mailtransports")); ok {
		smtp = kmailDefaultSMTP(f)
	}

	var out []ImportedAccount
	add := func(glob string, pop bool) {
		matches, _ := filepath.Glob(filepath.Join(dir, glob))
		sort.Strings(matches)
		for _, path := range matches {
			f, ok := readINIFile(path)
			if !ok {
				continue
			}
			in, user, ok := kmailIncoming(f, pop)
			if !ok {
				continue
			}
			address := user
			ident, hasIdent := idents[strings.ToLower(user)]
			name := ""
			if hasIdent {
				address = ident.Address
				name = ident.Name
			}
			acct := AccountConfig{
				Name:    firstNonEmpty(name, address),
				Address: address,
				SMTP:    smtp,
			}
			if hasIdent {
				acct.Identities = []Identity{ident}
			}
			if pop {
				acct.Protocol, acct.POP = "pop3", in
			} else {
				acct.Protocol, acct.IMAP = "imap", in
			}
			out = append(out, ImportedAccount{Source: "KMail", Account: acct})
		}
	}
	add("akonadi_imap_resource_*rc", false)
	add("akonadi_pop3_resource_*rc", true)
	if len(out) == 0 {
		return nil, fmt.Errorf("no KMail accounts found")
	}
	return dedupImported(out), nil
}
