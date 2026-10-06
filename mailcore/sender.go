package mailcore

import (
	"bytes"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"
)

// Sender checks: what the receiving server said of the sender's domain —
// SPF, DKIM and DMARC, from its Authentication-Results header — and the
// tricks a forged sender plays on a reader: a name that shows another
// address or domain, a contact's name on a stranger's address, a domain
// that looks like one you write to, letters from another alphabet, replies
// sent elsewhere. They prove the domain at most, never the person: that is
// what signatures are for (protection.go).

// Auth values.
const (
	AuthPass = "pass"
	AuthFail = "fail"
	AuthNone = "none"
)

// authVerdict is the receiving server's verdict on the sender's domain,
// from the topmost Authentication-Results header: the one the person's
// own provider added last. Headers further down came with the message and
// prove nothing — a sender can write any it likes. why names what failed.
//
// DMARC decides when the server reports it. Without it, a DKIM signature
// that passes for the From domain is a pass, a DKIM or SPF failure a fail;
// Microsoft's composite verdict (compauth) counts as its DMARC.
func authVerdict(h mail.Header, from string) (verdict, why string) {
	all := h["Authentication-Results"]
	if len(all) == 0 {
		return AuthNone, ""
	}
	results := parseAuthResults(all[0])
	fromDom := registrableDomain(domainOf(ExtractAddr(from)))
	first := func(method string) string {
		for _, r := range results {
			if r.method == method {
				return r.result
			}
		}
		return ""
	}
	switch first("dmarc") {
	case "pass":
		return AuthPass, ""
	case "fail":
		return AuthFail, "dmarc"
	}
	if first("compauth") == "fail" {
		return AuthFail, "compauth"
	}
	dkimFail := false
	for _, r := range results {
		if r.method != "dkim" {
			continue
		}
		switch r.result {
		case "pass":
			d := r.props["header.d"]
			if d == "" {
				d = domainOf(r.props["header.i"])
			}
			if fromDom != "" && registrableDomain(d) == fromDom {
				return AuthPass, ""
			}
		case "fail":
			dkimFail = true
		}
	}
	if dkimFail {
		return AuthFail, "dkim"
	}
	for _, r := range results {
		if r.method != "spf" {
			continue
		}
		switch r.result {
		case "pass":
			if fromDom != "" && registrableDomain(domainOf(r.props["smtp.mailfrom"])) == fromDom {
				return AuthPass, ""
			}
		case "fail":
			return AuthFail, "spf"
		}
	}
	return AuthNone, ""
}

// authFromRaw is authVerdict for a raw message, with who wrote it
// (authServerOf) and who delivered it (deliveredBy).
func authFromRaw(raw []byte, from string) (a rawAuth, ok bool) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return rawAuth{}, false
	}
	a.verdict, a.why = authVerdict(m.Header, from)
	a.server, a.by = authServerOf(m.Header), deliveredBy(m.Header)
	return a, true
}

// rawAuth is what a message's header says of its sender's check.
type rawAuth struct{ verdict, why, server, by string }

// apply puts it on m.
func (a rawAuth) apply(m *Message) {
	m.Auth, m.AuthWhy, m.AuthServer, m.DeliveredBy = a.verdict, a.why, a.server, a.by
}

type authResult struct {
	method, result string
	props          map[string]string
	comment        string
}

var authComment = regexp.MustCompile(`\([^()]*\)`)

// authMethods are the methods a result is read for.
var authMethods = map[string]bool{"spf": true, "dkim": true, "dmarc": true, "compauth": true, "arc": true, "iprev": true, "auth": true, "bimi": true}

// parseAuthResults reads an Authentication-Results value (RFC 8601):
// "authserv-id; method=result prop=value …; …". Microsoft 365 leaves the
// server's id out and starts with the first result (readAuthHeader).
func parseAuthResults(v string) []authResult { return readAuthHeader(v).results }

func domainOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		addr = addr[i+1:]
	}
	return strings.ToLower(strings.Trim(addr, "<>. "))
}

// registrableDomain is the part of a domain an organisation registers —
// example.com of mail.example.com, example.co.uk of mx.example.co.uk —
// without the public suffix list: two labels, or three under a country
// code's usual second level (co.uk, com.au, …).
func registrableDomain(d string) string {
	d = strings.ToLower(strings.Trim(d, ". "))
	labels := strings.Split(d, ".")
	if len(labels) <= 2 {
		return d
	}
	n := 2
	if last := labels[len(labels)-1]; len(last) == 2 {
		switch labels[len(labels)-2] {
		case "co", "com", "net", "org", "gov", "ac", "edu", "ne", "or", "go", "gv", "mil":
			n = 3
		}
	}
	return strings.Join(labels[len(labels)-n:], ".")
}

// SenderCheck is what the reading pane says of a message's sender.
type SenderCheck struct {
	// Auth is the receiving server's verdict on the sender's domain
	// ("pass", "fail", "none"); AuthWhy what failed. It is none when the
	// header that gave it is not your provider's: AuthServer wrote it,
	// and AuthTrust says whose that is (AuthBy…).
	Auth       string `json:"auth"`
	AuthWhy    string `json:"authWhy,omitempty"`
	AuthServer string `json:"authServer,omitempty"`
	AuthTrust  string `json:"authTrust,omitempty"`
	// Warnings are worth stopping for; Notes worth knowing.
	Warnings []string `json:"warnings,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

// SenderCheck checks message id's sender against the server's verdict and
// the people you write to. It needs only what the cache has of the
// message — it never downloads a body.
func (s *LocalStore) SenderCheck(id MessageID) (SenderCheck, error) {
	s.mu.Lock()
	i, ok := s.indexLocked(s.resolveLocked(id))
	if !ok {
		s.mu.Unlock()
		return SenderCheck{}, errNoMessage(id)
	}
	s.fillAuthLocked(i)
	m := s.Messages[i].Clone()
	trust := s.authTrustLocked(m)
	s.mu.Unlock()
	said := m.Auth
	m.Auth, m.AuthWhy = trustedVerdict(m, trust)
	out := checkSender(m, s.correspondents())
	out.AuthServer, out.AuthTrust = m.AuthServer, trust
	if trust == AuthByOther && said != AuthNone {
		server := m.AuthServer
		if server == "?" {
			server = "a server that does not name itself"
		}
		out.Notes = append(out.Notes, "The sender check in this message was written by "+server+
			", not by your provider's server: it came with the message, and proves nothing.")
	}
	return out, nil
}

// fillAuthLocked reads message i's sender check from its raw message,
// when it was cached before it was read: the verdict, or who wrote it.
func (s *LocalStore) fillAuthLocked(i int) {
	m := &s.Messages[i]
	if m.Auth != "" && (m.Auth == AuthNone || m.AuthServer != "") {
		return
	}
	if raw := s.readRawLocked(*m); len(raw) > 0 {
		if a, ok := authFromRaw(raw, m.From); ok {
			a.apply(m)
		}
	}
}

func errNoMessage(id MessageID) error { return &noMessageError{id} }

type noMessageError struct{ id MessageID }

func (e *noMessageError) Error() string { return "mail: no message " + string(e.id) }

// knownPeople are the people you write to: the recipients of what you
// sent, your saved contacts, and your own addresses. Who has merely
// written to you is not among them — a forger would be.
type knownPeople struct {
	addrs   map[string]bool
	names   map[string][]string // a name, lower case → its addresses
	domains map[string]bool     // registrable domains
}

var knownCache struct {
	mu   sync.Mutex
	at   time.Time
	of   *LocalStore
	have knownPeople
}

// correspondents are the people you write to, worked out at most every
// two minutes.
func (s *LocalStore) correspondents() knownPeople {
	knownCache.mu.Lock()
	defer knownCache.mu.Unlock()
	if knownCache.of == s && time.Since(knownCache.at) < 2*time.Minute {
		return knownCache.have
	}
	k := knownPeople{addrs: map[string]bool{}, names: map[string][]string{}, domains: map[string]bool{}}
	add := func(name, addr string) {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if !strings.Contains(addr, "@") {
			return
		}
		k.addrs[addr] = true
		k.domains[registrableDomain(domainOf(addr))] = true
		if n := strings.ToLower(strings.Join(strings.Fields(name), " ")); len(n) >= 3 && !strings.Contains(n, "@") {
			k.names[n] = append(k.names[n], addr)
		}
	}
	s.mu.Lock()
	sent := map[FolderID]bool{}
	for _, f := range s.Folders {
		if f.Kind == FolderSent {
			sent[f.ID] = true
		}
	}
	for _, m := range s.Messages {
		if !sent[m.Folder] {
			continue
		}
		for _, field := range []string{m.To, m.Cc, m.Bcc} {
			for _, a := range ParseAddrList(field) {
				add(a.Name, a.Address)
			}
		}
	}
	for _, id := range s.identities {
		add("", id.Address)
	}
	for _, a := range s.cfg.Accounts {
		add("", a.Address)
	}
	s.mu.Unlock()
	if s.feat != nil {
		s.feat.mu.Lock()
		for _, c := range s.feat.saved {
			add(c.Name, c.Address)
		}
		s.feat.mu.Unlock()
	}
	knownCache.of, knownCache.at, knownCache.have = s, time.Now(), k
	return k
}

var (
	addrInName   = regexp.MustCompile(`[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	domainInName = regexp.MustCompile(`\b[a-z0-9\-]+(?:\.[a-z0-9\-]+)*\.([a-z]{2,})\b`)
)

// nameTLDs are the endings that make a word in a display name a domain
// ("PayPal.com"), and not an initial or an abbreviation ("St.Mary").
var nameTLDs = map[string]bool{"com": true, "net": true, "org": true, "edu": true, "gov": true, "info": true,
	"biz": true, "io": true, "app": true, "dev": true, "ai": true, "co": true, "me": true, "online": true,
	"shop": true, "site": true, "xyz": true, "top": true, "cloud": true, "email": true, "support": true,
	"help": true, "services": true, "security": true, "bank": true, "finance": true}

// checkSender is the sender check for m, given the people you write to.
func checkSender(m Message, known knownPeople) SenderCheck {
	out := SenderCheck{Auth: m.Auth, AuthWhy: m.AuthWhy}
	if out.Auth == "" {
		out.Auth = AuthNone
	}
	a, err := mail.ParseAddress(m.From)
	if err != nil {
		return out
	}
	addr := strings.ToLower(a.Address)
	dom := domainOf(addr)
	reg := registrableDomain(dom)
	shown := displayDomain(dom)
	warn := func(s string) { out.Warnings = append(out.Warnings, s) }

	if out.Auth == AuthFail {
		lead := "Your mail server could not confirm this is from " + shown
		switch out.AuthWhy {
		case "dkim":
			warn(lead + ": its signature did not hold (DKIM), so it may have been changed or forged.")
		case "spf":
			warn(lead + ": it came from a server " + shown + " does not send from (SPF).")
		case "dmarc":
			warn(lead + ": it failed " + shown + "'s own sender policy (DMARC).")
		default:
			warn(lead + ".")
		}
	}
	name := strings.ToLower(strings.TrimSpace(a.Name))
	if name != "" {
		if other := addrInName.FindString(name); other != "" && other != addr {
			warn("The name shows " + other + ", but it is from " + addr + ".")
		} else {
			for _, sm := range domainInName.FindAllStringSubmatch(name, -1) {
				if nameTLDs[sm[1]] && registrableDomain(sm[0]) != reg {
					warn("The name says " + sm[0] + ", but it is from " + shown + ".")
					break
				}
			}
		}
		if addrs := known.names[strings.Join(strings.Fields(name), " ")]; len(addrs) > 0 && !contains(addrs, addr) {
			warn("The name is that of someone you write to at " + addrs[0] + ", but this is from " + addr + ".")
		}
	}
	if !known.domains[reg] {
		if like := lookAlike(reg, known.domains); like != "" {
			warn("The domain " + shown + " looks like " + like + ", which you write to.")
		} else if shown != dom || !isASCII(dom) {
			warn("The domain " + shown + " uses letters from another alphabet, which can look like familiar ones (it is " + dom + ").")
		}
	}
	if rt, err := mail.ParseAddress(m.ReplyTo); err == nil && rt.Address != "" {
		rtDom := registrableDomain(domainOf(rt.Address))
		if rtDom != reg && !known.domains[rtDom] {
			out.Notes = append(out.Notes, "Replies go to "+strings.ToLower(rt.Address)+", not to "+shown+".")
		}
	}
	return out
}

// displayDomain is a domain as a person reads it: its international
// letters, where it has them.
func displayDomain(d string) string {
	if u, err := idna.ToUnicode(d); err == nil && u != "" {
		return u
	}
	return d
}

// lookAlike is the domain you write to that d imitates: one letter off, or
// the same once look-alike letters are read alike (rn for m, 0 for o, …).
// Short domains are left alone: too many are one letter apart.
func lookAlike(d string, known map[string]bool) string {
	if len(d) < 6 {
		return ""
	}
	sd := skeleton(d)
	for k := range known {
		if k == d || len(k) < 6 {
			continue
		}
		if skeleton(k) == sd || editDistanceOne(d, k) {
			return k
		}
	}
	return ""
}

// skeleton reads look-alike letters alike.
func skeleton(d string) string {
	d = strings.ToLower(d)
	for _, r := range [][2]string{{"rn", "m"}, {"vv", "w"}, {"cl", "d"}, {"0", "o"}, {"1", "l"}, {"i", "l"}, {"5", "s"}, {"-", ""}} {
		d = strings.ReplaceAll(d, r[0], r[1])
	}
	return d
}

// editDistanceOne says a and b differ by one letter added, dropped,
// changed, or two neighbours swapped.
func editDistanceOne(a, b string) bool {
	if a == b {
		return false
	}
	la, lb := len(a), len(b)
	switch {
	case la == lb:
		var diff []int
		for i := 0; i < la; i++ {
			if a[i] != b[i] {
				diff = append(diff, i)
				if len(diff) > 2 {
					return false
				}
			}
		}
		if len(diff) == 1 {
			return true
		}
		return len(diff) == 2 && diff[1] == diff[0]+1 && a[diff[0]] == b[diff[1]] && a[diff[1]] == b[diff[0]]
	case la == lb+1:
		return dropsOne(a, b)
	case lb == la+1:
		return dropsOne(b, a)
	}
	return false
}

// dropsOne says short is long with one letter taken out.
func dropsOne(long, short string) bool {
	for i := 0; i < len(long); i++ {
		if long[:i]+long[i+1:] == short {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
