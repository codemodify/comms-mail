package mailcore

import (
	"bytes"
	"net"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The security report: what the reading pane's Security tab says of a
// message that needs no key. What the receiving server checked of the
// sender — SPF, DKIM, DMARC, ARC and the rest, each with the domain it
// checked and whether that is From's — and whether that server is your
// provider's; what the servers that passed it on saw (ARC); the domains
// that signed it (DKIM-Signature); and the sender check (sender.go).
// Signatures and encryption are MessageSecurity's (protection.go): they
// may need keys, and a prompt.

// Who wrote a message's Authentication-Results.
const (
	// AuthByProvider: your provider's server; what it says counts.
	AuthByProvider = "provider"
	// AuthByOther: a server that is not your provider's — the header came
	// with the message, and proves nothing.
	AuthByOther = "other"
	// AuthByUnknown: there is no provider to tell by (mail imported from
	// files); what it says counts, as it did before it could be told.
	AuthByUnknown = "unknown"
)

// SecurityReport is the Security tab's say on a message, besides its
// signatures and encryption.
type SecurityReport struct {
	Sender SenderCheck `json:"sender"`
	// Server is who wrote the topmost Authentication-Results ("" none,
	// "?" one that names no server), Trust whether that is your
	// provider's (AuthBy…), Provider the domains your provider's servers
	// go by.
	Server   string   `json:"server,omitempty"`
	Trust    string   `json:"trust,omitempty"`
	Provider []string `json:"provider,omitempty"`
	// Checks are what it found, in its order.
	Checks []AuthCheck `json:"checks,omitempty"`
	// ARC are what each server that passed the message on saw, first
	// first.
	ARC []ARCSet `json:"arc,omitempty"`
	// DKIM are the message's domain signatures, as they read — the
	// checks say whether they hold.
	DKIM []DKIMSignature `json:"dkim,omitempty"`
	// Autocrypt: the message carries its sender's OpenPGP key.
	Autocrypt bool `json:"autocrypt,omitempty"`
	// Route is the way it came, first hop first (route.go).
	Route []RouteHop `json:"route,omitempty"`
}

// AuthCheck is one result of an Authentication-Results header.
type AuthCheck struct {
	// Method is spf, dkim, dmarc, arc, compauth, iprev, bimi or auth;
	// Result pass, fail, softfail, neutral, none, temperror, permerror,
	// policy….
	Method string `json:"method"`
	Result string `json:"result"`
	// Domain is what it checked: SPF the envelope sender's, DKIM the
	// signer's, DMARC and BIMI From's; Aligned that it is From's
	// organisation.
	Domain  string `json:"domain,omitempty"`
	Aligned bool   `json:"aligned,omitempty"`
	// Selector is a DKIM key's; Policy DMARC's for the domain (none,
	// quarantine, reject) and Disposition what the server did by it.
	Selector    string `json:"selector,omitempty"`
	Policy      string `json:"policy,omitempty"`
	Disposition string `json:"disposition,omitempty"`
	// Reason is the server's own, where it gives one; Address the IP
	// address iprev checked, User who signed in to send (auth).
	Reason  string `json:"reason,omitempty"`
	Address string `json:"address,omitempty"`
	User    string `json:"user,omitempty"`
}

// ARCSet is one server's ARC set: what it saw as it passed the message
// on, and whether the chain held when it got it (Seal: none, pass, fail).
type ARCSet struct {
	Instance int         `json:"i"`
	Server   string      `json:"server,omitempty"`
	Signer   string      `json:"signer,omitempty"`
	Seal     string      `json:"seal,omitempty"`
	Checks   []AuthCheck `json:"checks,omitempty"`
}

// DKIMSignature is one DKIM-Signature header.
type DKIMSignature struct {
	Domain    string `json:"domain"`
	Selector  string `json:"selector,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	// Aligned: the signer is From's organisation. Result is what the
	// receiving server found ("" when it said nothing of this one).
	Aligned bool   `json:"aligned,omitempty"`
	Result  string `json:"result,omitempty"`
	// Signed is when it was made; Expires when it stops holding.
	Signed  time.Time `json:"signed,omitzero"`
	Expires time.Time `json:"expires,omitzero"`
	// Length is l=: only that much of the body is signed — more can be
	// added after it and the signature still holds. Weak says why the
	// algorithm cannot be trusted.
	Length int    `json:"length,omitempty"`
	Weak   string `json:"weak,omitempty"`
}

// authHeader is one Authentication-Results header, read in full.
type authHeader struct {
	// server is its authserv-id; "" when it names none (Microsoft 365
	// starts with the first result). instance is ARC's i=.
	server   string
	instance int
	results  []authResult
}

// readAuthHeader reads an Authentication-Results value (RFC 8601),
// "authserv-id; method=result prop=value (comment) …; …", or an
// ARC-Authentication-Results one, which starts with "i=n;".
func readAuthHeader(v string) authHeader {
	var h authHeader
	for k, seg := range splitAuth(strings.ReplaceAll(v, "\r\n", " ")) {
		fields := strings.Fields(seg.text)
		if len(fields) == 0 {
			continue
		}
		key, val, isResult := strings.Cut(fields[0], "=")
		if k <= 1 && isResult && strings.EqualFold(key, "i") {
			h.instance, _ = strconv.Atoi(val)
			continue
		}
		if !isResult {
			if h.server == "" && len(h.results) == 0 {
				h.server = strings.ToLower(fields[0])
			}
			continue
		}
		if !authMethods[strings.ToLower(key)] {
			continue
		}
		r := authResult{method: strings.ToLower(key), result: strings.ToLower(val), props: map[string]string{}, comment: seg.comment}
		for _, f := range fields[1:] {
			if pk, pv, ok := strings.Cut(f, "="); ok {
				r.props[strings.ToLower(pk)] = strings.Trim(strings.ToLower(pv), `"`)
			}
		}
		h.results = append(h.results, r)
	}
	return h
}

type authSegment struct{ text, comment string }

// splitAuth splits a header value at its semicolons — not those in a
// comment or a quoted string — and takes each part's comments out.
func splitAuth(v string) []authSegment {
	var out []authSegment
	var text, comment strings.Builder
	depth, quoted := 0, false
	for _, r := range v {
		switch {
		case quoted:
			text.WriteRune(r)
			if r == '"' {
				quoted = false
			}
		case depth > 0:
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					comment.WriteByte(' ')
					continue
				}
			}
			comment.WriteRune(r)
		case r == '(':
			depth = 1
			text.WriteByte(' ')
		case r == '"':
			quoted = true
			text.WriteRune(r)
		case r == ';':
			out = append(out, authSegment{text.String(), strings.TrimSpace(comment.String())})
			text.Reset()
			comment.Reset()
		default:
			text.WriteRune(r)
		}
	}
	return append(out, authSegment{text.String(), strings.TrimSpace(comment.String())})
}

// authCheck is r as the report says it, From's organisation fromOrg.
func authCheck(r authResult, fromOrg string) AuthCheck {
	c := AuthCheck{Method: r.method, Result: r.result, Reason: r.props["reason"]}
	switch r.method {
	case "spf":
		c.Domain = domainOf(r.props["smtp.mailfrom"])
		if c.Domain == "" {
			c.Domain = domainOf(r.props["smtp.helo"])
		}
	case "dkim":
		c.Domain = r.props["header.d"]
		if c.Domain == "" {
			c.Domain = domainOf(r.props["header.i"])
		}
		c.Selector = r.props["header.s"]
	case "dmarc":
		c.Domain = r.props["header.from"]
		// Google and others say the policy in a comment, "p=REJECT
		// sp=REJECT dis=NONE"; Microsoft says what it did, "action=none".
		for _, f := range strings.Fields(strings.ToLower(r.comment)) {
			if k, v, ok := strings.Cut(f, "="); ok {
				switch k {
				case "p":
					c.Policy = v
				case "dis":
					c.Disposition = v
				}
			}
		}
		if a := r.props["action"]; a != "" && c.Disposition == "" {
			c.Disposition = a
		}
	case "bimi":
		c.Domain = r.props["header.d"]
	case "iprev":
		c.Address = r.props["policy.iprev"]
		if c.Address == "" {
			c.Address = r.props["smtp.remote-ip"]
		}
	case "auth":
		c.User = r.props["smtp.auth"]
		if c.User == "" {
			c.User = r.props["smtp.mailfrom"]
		}
	}
	c.Domain = strings.Trim(c.Domain, "<>. ")
	if c.Domain != "" && fromOrg != "" {
		c.Aligned = registrableDomain(c.Domain) == fromOrg
	}
	return c
}

// dkimTags reads a DKIM-Signature's (or ARC-Seal's) tags.
func dkimTags(v string) map[string]string {
	out := map[string]string{}
	for _, t := range strings.Split(v, ";") {
		k, val, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.Join(strings.Fields(val), "")
	}
	return out
}

// dkimSignature reads one DKIM-Signature header, and finds what the
// receiving server said of it among checks.
func dkimSignature(v string, fromOrg string, checks []AuthCheck) DKIMSignature {
	t := dkimTags(v)
	d := DKIMSignature{Domain: strings.ToLower(t["d"]), Selector: strings.ToLower(t["s"]), Algorithm: strings.ToLower(t["a"])}
	d.Aligned = d.Domain != "" && registrableDomain(d.Domain) == fromOrg
	if n, err := strconv.ParseInt(t["t"], 10, 64); err == nil {
		d.Signed = time.Unix(n, 0).UTC()
	}
	if n, err := strconv.ParseInt(t["x"], 10, 64); err == nil {
		d.Expires = time.Unix(n, 0).UTC()
	}
	if n, err := strconv.Atoi(t["l"]); err == nil {
		d.Length = n
	}
	if strings.HasSuffix(d.Algorithm, "-sha1") {
		d.Weak = "SHA-1, which can be forged"
	}
	for _, c := range checks {
		if c.Method == "dkim" && c.Domain == d.Domain && (c.Selector == "" || c.Selector == d.Selector) {
			d.Result = c.Result
			break
		}
	}
	return d
}

// receivedBy is the domain of the server that wrote a Received header:
// its "by", when that is a name and not an address.
var receivedBy = regexp.MustCompile(`(?i)\bby\s+([a-z0-9][a-z0-9.-]*\.[a-z]{2,})\b`)

// deliveredBy is the organisation of the server that put the message in
// the mailbox — the topmost Received header's — which no sender can
// write.
func deliveredBy(h mail.Header) string {
	all := h["Received"]
	if len(all) == 0 {
		return ""
	}
	v := authComment.ReplaceAllString(strings.ReplaceAll(all[0], "\r\n", " "), " ")
	m := receivedBy.FindStringSubmatch(v)
	if m == nil || net.ParseIP(m[1]) != nil {
		return ""
	}
	return registrableDomain(m[1])
}

// authServerOf is the server named in the topmost Authentication-Results:
// its authserv-id, "?" when it names none, "" when there is none.
func authServerOf(h mail.Header) string {
	all := h["Authentication-Results"]
	if len(all) == 0 {
		return ""
	}
	if s := readAuthHeader(all[0]).server; s != "" {
		return s
	}
	return "?"
}

// providerAliases are the domains a provider's servers go by, for the
// domain of its mail servers or addresses: Gmail's are Google's.
var providerAliases = map[string][]string{
	"gmail.com":      {"google.com"},
	"googlemail.com": {"google.com"},
	"office365.com":  {"outlook.com", "microsoft.com"},
	"outlook.com":    {"office365.com", "microsoft.com"},
	"hotmail.com":    {"outlook.com", "office365.com", "microsoft.com"},
	"live.com":       {"outlook.com", "office365.com", "microsoft.com"},
	"msn.com":        {"outlook.com", "office365.com", "microsoft.com"},
	"yahoo.com":      {"yahoo.com", "yahoodns.net", "aol.com"},
	"ymail.com":      {"yahoo.com", "yahoodns.net"},
	"aol.com":        {"yahoo.com", "yahoodns.net"},
	"me.com":         {"apple.com", "icloud.com"},
	"mac.com":        {"apple.com", "icloud.com"},
	"icloud.com":     {"apple.com", "me.com"},
	"fastmail.com":   {"messagingengine.com"},
	"fastmail.fm":    {"messagingengine.com"},
	"proton.me":      {"protonmail.ch", "protonmail.com"},
	"protonmail.com": {"protonmail.ch", "proton.me"},
	"protonmail.ch":  {"proton.me", "protonmail.com"},
	"zoho.com":       {"zohomail.com"},
	"zohomail.com":   {"zoho.com"},
	"gmx.net":        {"gmx.com", "web.de", "mail.com"},
	"gmx.com":        {"gmx.net", "web.de", "mail.com"},
	"web.de":         {"gmx.net", "gmx.com"},
	"yandex.ru":      {"yandex.net", "yandex.com"},
	"yandex.com":     {"yandex.net", "yandex.ru"},
	"mail.ru":        {"mail.ru"},
	"mailbox.org":    {"mailbox.org"},
	"posteo.de":      {"posteo.de"},
	"tutanota.com":   {"tutanota.de"},
}

// microsoftDomains are the domains Microsoft's servers go by: theirs
// write an Authentication-Results that names no server.
var microsoftDomains = map[string]bool{"outlook.com": true, "office365.com": true, "microsoft.com": true}

// providerDomainsLocked are the organisations your provider's servers go
// by, for an account: its own servers' and address's, their aliases, and
// the one that checks most of its mail. None for mail with no account to
// tell by.
func (s *LocalStore) providerDomainsLocked(accountID string) map[string]bool {
	var a AccountConfig
	ok := false
	for _, c := range s.cfg.Accounts {
		if id := c.ID; id == accountID || id == "" && slug(c.Address) == accountID {
			a, ok = c, true
		}
	}
	if !ok {
		return nil
	}
	out := map[string]bool{}
	add := func(d string) {
		if d = registrableDomain(d); d == "" || net.ParseIP(d) != nil || !strings.Contains(d, ".") {
			return
		}
		out[d] = true
		for _, al := range providerAliases[d] {
			out[al] = true
		}
	}
	for _, host := range []string{a.IMAP.Host, a.POP.Host, a.SMTP.Host} {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		add(host)
	}
	add(domainOf(a.Address))
	if a.Provider == "microsoft" {
		add("office365.com")
	}
	if usual := s.usualAuthServerLocked(accountID); usual != "" {
		out[usual] = true
	}
	return out
}

// usual is, for each account, the organisation that wrote the
// Authentication-Results of most of its mail — its provider, whatever it
// is called: a forger writes few of them.
var usual struct {
	mu sync.Mutex
	at map[string]time.Time
	of map[string]string
}

// usualAuthServerLocked is the organisation that checked at least 80% of
// an account's mail, of 10 or more checked; "" when none did.
func (s *LocalStore) usualAuthServerLocked(accountID string) string {
	usual.mu.Lock()
	defer usual.mu.Unlock()
	if at, ok := usual.at[accountID]; ok && time.Since(at) < 2*time.Minute {
		return usual.of[accountID]
	}
	count, total := map[string]int{}, 0
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.AccountID != accountID || m.AuthServer == "" || m.AuthServer == "?" {
			continue
		}
		count[registrableDomain(m.AuthServer)]++
		total++
	}
	best := ""
	for d, n := range count {
		if total >= 10 && n*5 >= total*4 {
			best = d
		}
	}
	if usual.at == nil {
		usual.at, usual.of = map[string]time.Time{}, map[string]string{}
	}
	usual.at[accountID], usual.of[accountID] = time.Now(), best
	return best
}

// authTrustLocked says who wrote m's Authentication-Results (AuthBy…), ""
// when it has none.
func (s *LocalStore) authTrustLocked(m Message) string {
	if m.AuthServer == "" {
		return ""
	}
	doms := s.providerDomainsLocked(m.AccountID)
	if len(doms) == 0 {
		return AuthByUnknown
	}
	if m.AuthServer == "?" {
		if microsoftDomains[m.DeliveredBy] {
			return AuthByProvider
		}
		for d := range doms {
			if microsoftDomains[d] {
				return AuthByProvider
			}
		}
		return AuthByOther
	}
	org := registrableDomain(m.AuthServer)
	if doms[org] || org == m.DeliveredBy {
		return AuthByProvider
	}
	return AuthByOther
}

// trustedVerdict is m's verdict on its sender as far as it counts: none
// when the header that gave it is not your provider's.
func trustedVerdict(m Message, trust string) (auth, why string) {
	if trust == AuthByOther {
		return AuthNone, ""
	}
	return m.Auth, m.AuthWhy
}

// SecurityReport is message id's report. It reads the raw message, which
// the reading pane has fetched by the time it asks.
func (s *LocalStore) SecurityReport(id MessageID) (SecurityReport, error) {
	sc, err := s.SenderCheck(id)
	if err != nil {
		return SecurityReport{}, err
	}
	raw, err := s.GetRaw(id)
	if err != nil {
		return SecurityReport{}, err
	}
	var provider map[string]bool
	s.mu.Lock()
	if i, ok := s.indexLocked(s.resolveLocked(id)); ok {
		provider = s.providerDomainsLocked(s.Messages[i].AccountID)
	}
	s.mu.Unlock()
	r := securityReportFor(raw, provider)
	r.Sender, r.Trust = sc, sc.AuthTrust
	for d := range provider {
		r.Provider = append(r.Provider, d)
	}
	sort.Strings(r.Provider)
	return r, nil
}

// SecurityReportOf is a raw message's report, with no account to tell its
// provider by: its sender is checked against nobody.
func SecurityReportOf(raw []byte) SecurityReport { return securityReportFor(raw, nil) }

// securityReportFor is a raw message's report, provider the organisations
// your provider's servers go by (none: there is no account to tell by).
func securityReportFor(raw []byte, provider map[string]bool) SecurityReport {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return SecurityReport{Sender: SenderCheck{Auth: AuthNone}}
	}
	h := msg.Header
	if len(provider) > 0 {
		// The server that put it in the mailbox is your provider's too.
		mine := map[string]bool{}
		for d := range provider {
			mine[d] = true
		}
		if by := deliveredBy(h); by != "" {
			mine[by] = true
		}
		provider = mine
	}
	from := decodeRFC2047(h.Get("From"))
	fromOrg := registrableDomain(domainOf(ExtractAddr(from)))
	r := SecurityReport{Server: authServerOf(h), Autocrypt: h.Get("Autocrypt") != ""}
	r.Sender.Auth, r.Sender.AuthWhy = authVerdict(h, from)
	if r.Server != "" {
		r.Trust = AuthByUnknown
		for _, res := range readAuthHeader(h["Authentication-Results"][0]).results {
			r.Checks = append(r.Checks, authCheck(res, fromOrg))
		}
	}
	sets := map[int]*ARCSet{}
	set := func(i int) *ARCSet {
		if sets[i] == nil {
			sets[i] = &ARCSet{Instance: i}
		}
		return sets[i]
	}
	for _, v := range h["Arc-Authentication-Results"] {
		ah := readAuthHeader(v)
		if ah.instance == 0 {
			continue
		}
		a := set(ah.instance)
		a.Server = ah.server
		for _, res := range ah.results {
			a.Checks = append(a.Checks, authCheck(res, fromOrg))
		}
	}
	for _, v := range h["Arc-Seal"] {
		t := dkimTags(v)
		i, err := strconv.Atoi(t["i"])
		if err != nil || i == 0 {
			continue
		}
		a := set(i)
		a.Signer, a.Seal = strings.ToLower(t["d"]), strings.ToLower(t["cv"])
	}
	for _, a := range sets {
		r.ARC = append(r.ARC, *a)
	}
	sort.Slice(r.ARC, func(i, j int) bool { return r.ARC[i].Instance < r.ARC[j].Instance })
	for _, v := range h["Dkim-Signature"] {
		r.DKIM = append(r.DKIM, dkimSignature(v, fromOrg, r.Checks))
	}
	r.Route = routeOf(h, provider)
	return r
}
