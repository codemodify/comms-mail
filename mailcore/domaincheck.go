package mailcore

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// What a sender's domain publishes about its mail, looked up in DNS when
// you ask — never by itself, as your DNS resolver sees which domain was
// looked up: its mail servers (MX), who may send for it (SPF), what to do
// with mail that fails (DMARC), whether mail to it must use TLS (MTA-STS,
// and DANE's TLSA records per mail server), where TLS failures are
// reported (TLS-RPT), its logo (BIMI), and whether your resolver checked
// the answers' signatures (DNSSEC). MTA-STS's policy itself is on the
// domain's own web server, which would see you fetch it: only its DNS
// record is read.

// DomainReport is what a domain publishes.
type DomainReport struct {
	Domain string    `json:"domain"`
	At     time.Time `json:"at"`
	// MX are its mail servers, by preference.
	MX []string `json:"mx,omitempty"`
	// SPF is its record; SPFAll how it ends: "-all" (refuse the rest),
	// "~all" (mark them), "?all" (no say), "+all" (anyone), "" (it
	// hands over to another's, or there is none).
	SPF    string `json:"spf,omitempty"`
	SPFAll string `json:"spfAll,omitempty"`
	// DMARC is its record — found at the domain, or at its organisation's
	// — Policy and SubPolicy what to do with failing mail (none,
	// quarantine, reject), Pct of how much, Reports that failures are
	// reported to it.
	DMARC     string `json:"dmarc,omitempty"`
	Policy    string `json:"policy,omitempty"`
	SubPolicy string `json:"subPolicy,omitempty"`
	Pct       int    `json:"pct,omitempty"`
	Reports   bool   `json:"reports,omitempty"`
	// MTASTS is its MTA-STS record's id ("" none); TLSRPT that it asks
	// for reports of TLS failures.
	MTASTS string `json:"mtaSts,omitempty"`
	TLSRPT bool   `json:"tlsRpt,omitempty"`
	// BIMI is its logo's address, BIMICert that a mark certificate
	// vouches for it.
	BIMI     string `json:"bimi,omitempty"`
	BIMICert bool   `json:"bimiCert,omitempty"`
	// DANE are its mail servers' TLSA records, how many each has.
	DANE []DANEHost `json:"dane,omitempty"`
	// DNSSEC: the resolver said it checked the answers' signatures.
	// Checked says the resolver was asked to (no resolver to ask: Go's
	// own lookups, which cannot say).
	DNSSEC  bool     `json:"dnssec,omitempty"`
	Checked bool     `json:"checked,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// DANEHost is one mail server's TLSA records.
type DANEHost struct {
	Host    string `json:"host"`
	Records int    `json:"records"`
}

// domainCache keeps what was looked up for an hour.
var domainCache struct {
	mu sync.Mutex
	of map[string]DomainReport
}

// CachedDomain is what was looked up for domain within the hour.
func CachedDomain(domain string) (DomainReport, bool) {
	domainCache.mu.Lock()
	defer domainCache.mu.Unlock()
	r, ok := domainCache.of[strings.ToLower(domain)]
	if !ok || time.Since(r.At) > time.Hour {
		return DomainReport{}, false
	}
	return r, true
}

// resolverAddr is the DNS resolver to ask: UITK_MAIL_DNS's, else the
// first nameserver in /etc/resolv.conf, with its port ("" none). A
// variable for tests.
var resolverAddr = func() string {
	if v := strings.TrimSpace(os.Getenv(EnvDNS)); v != "" {
		if _, _, err := net.SplitHostPort(v); err != nil {
			v = net.JoinHostPort(v, "53")
		}
		return v
	}
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			return net.JoinHostPort(strings.Split(fields[1], "%")[0], "53")
		}
	}
	return ""
}

// CheckDomain looks domain up, and keeps what it found for an hour.
func CheckDomain(domain string) DomainReport {
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	r := DomainReport{Domain: domain, At: time.Now(), Pct: 100}
	if domain == "" || !strings.Contains(domain, ".") {
		r.Errors = append(r.Errors, "no domain to look up")
		return r
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	q := newDNSAsker(resolverAddr())
	r.Checked = q.server != ""
	secure := true
	note := func(what string, err error) {
		if err != nil && !errors.Is(err, errNoRecord) {
			r.Errors = append(r.Errors, what+": "+err.Error())
		}
	}
	mx, ad, err := q.mx(ctx, domain)
	note("MX", err)
	r.MX, secure = mx, secure && ad
	txt := func(name string) ([]string, error) {
		recs, ad, err := q.txt(ctx, name)
		secure = secure && ad
		return recs, err
	}
	recs, err := txt(domain)
	note("SPF", err)
	for _, t := range recs {
		if strings.HasPrefix(strings.ToLower(t), "v=spf1") {
			r.SPF = t
			r.SPFAll = spfAll(t)
		}
	}
	for _, at := range []string{domain, registrableDomain(domain)} {
		recs, err := txt("_dmarc." + at)
		note("DMARC", err)
		for _, t := range recs {
			if strings.HasPrefix(strings.ToLower(t), "v=dmarc1") {
				r.DMARC = t
				readDMARC(&r, t)
			}
		}
		if r.DMARC != "" || at == registrableDomain(domain) {
			break
		}
	}
	recs, err = txt("_mta-sts." + domain)
	note("MTA-STS", err)
	for _, t := range recs {
		if tags := dkimTags(t); strings.EqualFold(tags["v"], "STSv1") {
			r.MTASTS = tags["id"]
			if r.MTASTS == "" {
				r.MTASTS = "?"
			}
		}
	}
	recs, err = txt("_smtp._tls." + domain)
	note("TLS-RPT", err)
	for _, t := range recs {
		if strings.HasPrefix(strings.ToLower(t), "v=tlsrptv1") {
			r.TLSRPT = true
		}
	}
	recs, err = txt("default._bimi." + domain)
	note("BIMI", err)
	for _, t := range recs {
		if tags := dkimTags(t); strings.EqualFold(tags["v"], "BIMI1") {
			r.BIMI, r.BIMICert = tags["l"], tags["a"] != ""
		}
	}
	if q.server != "" {
		for i, host := range r.MX {
			if i == 3 {
				break
			}
			n, err := q.tlsa(ctx, "_25._tcp."+host)
			note("DANE "+host, err)
			r.DANE = append(r.DANE, DANEHost{Host: host, Records: n})
		}
	}
	r.DNSSEC = r.Checked && secure && len(r.Errors) == 0
	domainCache.mu.Lock()
	if domainCache.of == nil {
		domainCache.of = map[string]DomainReport{}
	}
	domainCache.of[domain] = r
	domainCache.mu.Unlock()
	return r
}

// spfAll is how an SPF record ends: its "all" with its qualifier.
func spfAll(rec string) string {
	for _, f := range strings.Fields(strings.ToLower(rec)) {
		switch f {
		case "all", "+all":
			return "+all"
		case "-all", "~all", "?all":
			return f
		}
	}
	return ""
}

// readDMARC reads a DMARC record's policy, its subdomains', how much it
// covers, and whether failures are reported.
func readDMARC(r *DomainReport, rec string) {
	tags := dkimTags(rec)
	r.Policy, r.SubPolicy = strings.ToLower(tags["p"]), strings.ToLower(tags["sp"])
	if n, err := strconv.Atoi(tags["pct"]); err == nil {
		r.Pct = n
	}
	r.Reports = tags["rua"] != ""
}

// errNoRecord: the name has none of what was asked.
var errNoRecord = errors.New("no record")

// dnsAsker asks one resolver, over UDP and — for a long answer — TCP,
// with the DNSSEC flag set; or, with no resolver, Go's own lookups.
type dnsAsker struct{ server string }

func newDNSAsker(server string) *dnsAsker { return &dnsAsker{server: server} }

func (q *dnsAsker) txt(ctx context.Context, name string) ([]string, bool, error) {
	if q.server == "" {
		recs, err := net.DefaultResolver.LookupTXT(ctx, name)
		return recs, false, notFound(err)
	}
	m, err := q.ask(ctx, name, dnsmessage.TypeTXT)
	if err != nil {
		return nil, false, err
	}
	var out []string
	for _, a := range m.Answers {
		if t, ok := a.Body.(*dnsmessage.TXTResource); ok {
			out = append(out, strings.Join(t.TXT, ""))
		}
	}
	return out, m.Header.AuthenticData, nil
}

func (q *dnsAsker) mx(ctx context.Context, name string) ([]string, bool, error) {
	if q.server == "" {
		mxs, err := net.DefaultResolver.LookupMX(ctx, name)
		var out []string
		for _, m := range mxs {
			out = append(out, strings.TrimSuffix(m.Host, "."))
		}
		return out, false, notFound(err)
	}
	m, err := q.ask(ctx, name, dnsmessage.TypeMX)
	if err != nil {
		return nil, false, err
	}
	type pref struct {
		host string
		p    uint16
	}
	var all []pref
	for _, a := range m.Answers {
		if mx, ok := a.Body.(*dnsmessage.MXResource); ok {
			all = append(all, pref{strings.TrimSuffix(mx.MX.String(), "."), mx.Pref})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].p < all[j].p })
	var out []string
	for _, p := range all {
		out = append(out, p.host)
	}
	return out, m.Header.AuthenticData, nil
}

// typeTLSA is DANE's record type (RFC 6698).
const typeTLSA dnsmessage.Type = 52

func (q *dnsAsker) tlsa(ctx context.Context, name string) (int, error) {
	m, err := q.ask(ctx, name, typeTLSA)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range m.Answers {
		if a.Header.Type == typeTLSA {
			n++
		}
	}
	return n, nil
}

func notFound(err error) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return errNoRecord
	}
	return err
}

// ask sends one question and reads the answer.
func (q *dnsAsker) ask(ctx context.Context, name string, t dnsmessage.Type) (*dnsmessage.Message, error) {
	fqdn, err := dnsmessage.NewName(strings.TrimSuffix(name, ".") + ".")
	if err != nil {
		return nil, err
	}
	var idb [2]byte
	_, _ = rand.Read(idb[:])
	id := binary.BigEndian.Uint16(idb[:])
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, RecursionDesired: true, AuthenticData: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{Name: fqdn, Type: t, Class: dnsmessage.ClassINET}); err != nil {
		return nil, err
	}
	if err := b.StartAdditionals(); err != nil {
		return nil, err
	}
	var opt dnsmessage.ResourceHeader
	if err := opt.SetEDNS0(4096, dnsmessage.RCodeSuccess, true); err != nil {
		return nil, err
	}
	if err := b.OPTResource(opt, dnsmessage.OPTResource{}); err != nil {
		return nil, err
	}
	query, err := b.Finish()
	if err != nil {
		return nil, err
	}
	answer, err := q.exchange(ctx, "udp", query)
	if err != nil {
		return nil, err
	}
	var m dnsmessage.Message
	if err := m.Unpack(answer); err != nil {
		return nil, err
	}
	if m.Header.Truncated {
		if answer, err = q.exchange(ctx, "tcp", query); err != nil {
			return nil, err
		}
		if err := m.Unpack(answer); err != nil {
			return nil, err
		}
	}
	if m.Header.ID != id {
		return nil, errors.New("the answer is not to the question")
	}
	switch m.Header.RCode {
	case dnsmessage.RCodeSuccess:
		return &m, nil
	case dnsmessage.RCodeNameError:
		return &dnsmessage.Message{Header: m.Header}, nil
	}
	return nil, fmt.Errorf("the resolver answered %v", m.Header.RCode)
}

func (q *dnsAsker) exchange(ctx context.Context, network string, query []byte) ([]byte, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, network, q.server)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	if network == "tcp" {
		msg := make([]byte, 2+len(query))
		binary.BigEndian.PutUint16(msg, uint16(len(query)))
		copy(msg[2:], query)
		if _, err := c.Write(msg); err != nil {
			return nil, err
		}
		var l [2]byte
		if _, err := io.ReadFull(c, l[:]); err != nil {
			return nil, err
		}
		out := make([]byte, binary.BigEndian.Uint16(l[:]))
		_, err := io.ReadFull(c, out)
		return out, err
	}
	if _, err := c.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	return buf[:n], err
}
