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

// Importing settings from another mail client saves setting an account up by
// hand. comms-mail reads what it safely can — servers, ports, encryption,
// user names, identities and signatures — and never a password: they live
// encrypted in the other client's own store (Thunderbird's NSS, KMail's
// KWallet) and cannot be read out. The imported account is written with an
// empty password, to be filled in once (or replaced by OAuth).

// ImportedAccount is one account found in another client's configuration,
// ready to be confirmed and saved. Password fields are always empty.
type ImportedAccount struct {
	Source  string        // "Thunderbird", "KMail"
	Account AccountConfig // password blank
}

// prefLine matches a Thunderbird prefs.js line:
//
//	user_pref("key", value);
//
// value is a quoted string, an integer, or true/false. Parsed leniently:
// what does not parse is skipped rather than failing the whole file.
func parseThunderbirdPrefs(r io.Reader) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "user_pref(") {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "user_pref(")), ";")
		inner = strings.TrimSuffix(inner, ")")
		key, rest, ok := cutQuoted(inner)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest = strings.TrimPrefix(rest, ",")
		rest = strings.TrimSpace(rest)
		if val, _, ok := cutQuoted(rest); ok {
			out[key] = val
		} else {
			out[key] = strings.TrimSpace(rest)
		}
	}
	return out
}

// cutQuoted reads a leading JS double-quoted string (with \" and \\ escapes)
// and returns it and the rest. ok is false when s does not start with a
// quote.
func cutQuoted(s string) (val, rest string, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '"' {
		return "", s, false
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		if c == '"' {
			return b.String(), s[i+1:], true
		}
		b.WriteByte(c)
	}
	return "", s, false
}

// tbSocketTLS maps Thunderbird's socketType / try_ssl (0 none, 2 STARTTLS,
// 3 SSL/TLS) to a TLSMode. An unset or unknown value defaults to SSL, which
// is what a modern account almost always is.
func tbSocketTLS(v string) string {
	switch strings.TrimSpace(v) {
	case "0":
		return string(TLSPlain)
	case "2":
		return string(TLSStartTLS)
	default:
		return string(TLSImplicit)
	}
}

func hostPort(host, port string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		return ""
	}
	if port == "" || port == "0" {
		return host
	}
	return host + ":" + port
}

// importThunderbird builds accounts from a parsed prefs.js.
func importThunderbird(p map[string]string) []ImportedAccount {
	var out []ImportedAccount
	accounts := splitList(p["mail.accountmanager.accounts"])
	sort.Strings(accounts)
	for _, acc := range accounts {
		serverKey := p["mail.account."+acc+".server"]
		if serverKey == "" {
			continue
		}
		sp := "mail.server." + serverKey + "."
		typ := strings.ToLower(p[sp+"type"])
		if typ != "imap" && typ != "pop3" {
			continue // "none" is Local Folders; movemail etc. we skip
		}
		host := hostPort(p[sp+"hostname"], p[sp+"port"])
		if host == "" {
			continue
		}
		user := p[sp+"userName"]
		incoming := ServerConfig{Host: host, User: user, TLSMode: tbSocketTLS(p[sp+"socketType"])}

		// Identities → address, name, signature, SMTP.
		var idents []Identity
		var address, smtpKey string
		for _, idKey := range splitList(p["mail.account."+acc+".identities"]) {
			ip := "mail.identity." + idKey + "."
			email := strings.TrimSpace(p[ip+"useremail"])
			if email == "" {
				continue
			}
			if address == "" {
				address = email
				smtpKey = p[ip+"smtpServer"]
			}
			idents = append(idents, Identity{
				ID:        "tb-" + idKey,
				Name:      strings.TrimSpace(p[ip+"fullName"]),
				Address:   email,
				Signature: importedSignature(p, ip),
			})
		}
		if len(idents) > 0 {
			idents[0].Default = true
		}
		if address == "" {
			address = user
		}

		smtp := ServerConfig{}
		if smtpKey == "" {
			smtpKey = p["mail.smtp.defaultserver"]
		}
		if smtpKey != "" {
			mp := "mail.smtpserver." + smtpKey + "."
			smtp = ServerConfig{
				Host:    hostPort(p[mp+"hostname"], p[mp+"port"]),
				User:    p[mp+"username"],
				TLSMode: tbSocketTLS(p[mp+"try_ssl"]),
			}
		}

		acctCfg := AccountConfig{
			Name:       firstNonEmpty(identityName(idents), address),
			Address:    address,
			Protocol:   typ,
			SMTP:       smtp,
			Identities: idents,
		}
		if typ == "pop3" {
			acctCfg.POP = incoming
		} else {
			acctCfg.IMAP = incoming
		}
		out = append(out, ImportedAccount{Source: "Thunderbird", Account: acctCfg})
	}
	return out
}

// importedSignature is the identity's signature: the inline text, or the
// contents of the signature file it points at.
func importedSignature(p map[string]string, ip string) string {
	if sig := strings.TrimSpace(p[ip+"htmlSigText"]); sig != "" {
		return htmlToPlainSignature(sig)
	}
	if p[ip+"sig_file"] != "" {
		if b, err := os.ReadFile(p[ip+"sig_file"]); err == nil {
			return strings.TrimRight(string(b), "\n")
		}
	}
	return ""
}

func htmlToPlainSignature(s string) string {
	if strings.Contains(s, "<") {
		return strings.TrimRight(HTMLToText(s), "\n")
	}
	return strings.TrimRight(s, "\n")
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func identityName(ids []Identity) string {
	for _, id := range ids {
		if id.Name != "" {
			return id.Name
		}
	}
	return ""
}

// ThunderbirdProfiles finds every prefs.js under the user's Thunderbird
// profiles. It reads profiles.ini when present, else globs the profile
// directories.
func ThunderbirdProfiles() []string {
	var roots []string
	home, _ := os.UserHomeDir()
	if home != "" {
		roots = append(roots, filepath.Join(home, ".thunderbird"), filepath.Join(home, ".mozilla-thunderbird"))
	}
	var out []string
	seen := map[string]bool{}
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "*", "prefs.js"))
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ImportThunderbird reads every Thunderbird profile and returns the accounts
// it finds, passwords blank.
func ImportThunderbird() ([]ImportedAccount, error) {
	var out []ImportedAccount
	profiles := ThunderbirdProfiles()
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no Thunderbird profile found")
	}
	for _, path := range profiles {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		accs := importThunderbird(parseThunderbirdPrefs(f))
		f.Close()
		out = append(out, accs...)
	}
	return dedupImported(out), nil
}

// dedupImported drops accounts with the same address (a profile listed
// twice, or the same account in two profiles).
func dedupImported(in []ImportedAccount) []ImportedAccount {
	seen := map[string]bool{}
	var out []ImportedAccount
	for _, a := range in {
		key := strings.ToLower(a.Source + "|" + a.Account.Address)
		if a.Account.Address == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}
