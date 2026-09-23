package mail

import "strings"

// GuessedHosts is IMAP/POP3/SMTP guessed from an email domain.
type GuessedHosts struct {
	IMAP     string `json:"imap"`
	POP      string `json:"pop,omitempty"`
	SMTP     string `json:"smtp"`
	Protocol string `json:"protocol,omitempty"` // default "imap"
	Provider string `json:"provider,omitempty"` // google, microsoft, yahoo, icloud, fastmail, proton, ""
	AuthHint string `json:"authHint,omitempty"`
}

// GuessMailHosts fills incoming/SMTP from the address domain (first-run polish).
// Protocol defaults to IMAP; POP is still filled so the user can switch.
func GuessMailHosts(address string) GuessedHosts {
	addr := canonAddr(address)
	_, domain, ok := strings.Cut(addr, "@")
	if !ok || domain == "" {
		return GuessedHosts{Protocol: ProtoIMAP}
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	switch domain {
	case "gmail.com", "googlemail.com":
		return GuessedHosts{
			IMAP: "imap.gmail.com:993", POP: "pop.gmail.com:995", SMTP: "smtp.gmail.com:465",
			Protocol: ProtoIMAP, Provider: "google", AuthHint: "Google OAuth or an App Password",
		}
	case "outlook.com", "hotmail.com", "live.com", "msn.com", "office365.com":
		return GuessedHosts{
			IMAP: "outlook.office365.com:993", POP: "outlook.office365.com:995", SMTP: "smtp.office365.com:587",
			Protocol: ProtoIMAP, Provider: "microsoft", AuthHint: "Microsoft OAuth or an App Password",
		}
	case "yahoo.com", "ymail.com":
		return GuessedHosts{
			IMAP: "imap.mail.yahoo.com:993", POP: "pop.mail.yahoo.com:995", SMTP: "smtp.mail.yahoo.com:587",
			Protocol: ProtoIMAP, Provider: "yahoo", AuthHint: "Yahoo App Password",
		}
	case "icloud.com", "me.com", "mac.com":
		return GuessedHosts{
			IMAP: "imap.mail.me.com:993", POP: "pop.mail.me.com:995", SMTP: "smtp.mail.me.com:587",
			Protocol: ProtoIMAP, Provider: "icloud", AuthHint: "Apple App-Specific Password",
		}
	case "fastmail.com", "fastmail.fm":
		return GuessedHosts{
			IMAP: "imap.fastmail.com:993", POP: "pop.fastmail.com:995", SMTP: "smtp.fastmail.com:465",
			Protocol: ProtoIMAP, Provider: "fastmail", AuthHint: "Fastmail App Password",
		}
	case "proton.me", "protonmail.com":
		return GuessedHosts{
			IMAP: "imap.proton.me:993", POP: "pop.proton.me:995", SMTP: "smtp.proton.me:587",
			Protocol: ProtoIMAP, Provider: "proton", AuthHint: "Proton Bridge / app password (IMAP Bridge)",
		}
	}
	// Microsoft 365 custom domains often use outlook.office365.com; we cannot
	// know without MX. Fall back to imap./pop./smtp. of the domain.
	return GuessedHosts{
		IMAP:     "imap." + domain + ":993",
		POP:      "pop." + domain + ":995",
		SMTP:     "smtp." + domain + ":587",
		Protocol: ProtoIMAP,
		AuthHint: "Type an app password, or OAuth if the host is Google/Microsoft",
	}
}

func providerForAddress(address string) string {
	return GuessMailHosts(address).Provider
}

func domainOfAddress(address string) string {
	_, domain, ok := strings.Cut(canonAddr(address), "@")
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(domain))
}
