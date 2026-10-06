package mailcore

import (
	"net"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeResolver answers DNS questions over UDP from answers (name and
// type → the records), with the DNSSEC flag set when signed; a name it
// has nothing for does not exist.
func fakeResolver(t *testing.T, signed bool, answers map[string][]dnsmessage.ResourceBody) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(buf[:n]) != nil || len(q.Questions) != 1 {
				continue
			}
			qq := q.Questions[0]
			key := strings.TrimSuffix(qq.Name.String(), ".") + "/" + qq.Type.String()
			bodies := answers[key]
			h := dnsmessage.Header{ID: q.Header.ID, Response: true, RecursionAvailable: true, AuthenticData: signed}
			if bodies == nil {
				h.RCode = dnsmessage.RCodeNameError
			}
			b := dnsmessage.NewBuilder(nil, h)
			_ = b.StartQuestions()
			_ = b.Question(qq)
			_ = b.StartAnswers()
			for _, body := range bodies {
				rh := dnsmessage.ResourceHeader{Name: qq.Name, Class: dnsmessage.ClassINET, TTL: 60}
				switch v := body.(type) {
				case *dnsmessage.TXTResource:
					_ = b.TXTResource(rh, *v)
				case *dnsmessage.MXResource:
					_ = b.MXResource(rh, *v)
				case *dnsmessage.UnknownResource:
					rh.Type = v.Type
					_ = b.UnknownResource(rh, *v)
				}
			}
			out, err := b.Finish()
			if err == nil {
				_, _ = pc.WriteTo(out, from)
			}
		}
	}()
	return pc.LocalAddr().String()
}

func txtRec(s string) dnsmessage.ResourceBody { return &dnsmessage.TXTResource{TXT: []string{s}} }

// A domain that publishes everything: its mail servers by preference,
// SPF that refuses the rest, DMARC that rejects (found at the
// organisation for a subdomain), MTA-STS, TLS-RPT, a BIMI logo with its
// certificate, DANE for its first mail server — signed, as the resolver
// says.
func TestCheckDomain(t *testing.T) {
	mx := func(host string, pref uint16) dnsmessage.ResourceBody {
		return &dnsmessage.MXResource{Pref: pref, MX: dnsmessage.MustNewName(host + ".")}
	}
	addr := fakeResolver(t, true, map[string][]dnsmessage.ResourceBody{
		"mail.example.com/TypeMX":                {mx("mx2.example.com", 20), mx("mx1.example.com", 10)},
		"mail.example.com/TypeTXT":               {txtRec("google-site-verification=x"), txtRec("v=spf1 include:_spf.example.com -all")},
		"_dmarc.example.com/TypeTXT":             {txtRec("v=DMARC1; p=reject; sp=quarantine; pct=50; rua=mailto:d@example.com")},
		"_mta-sts.mail.example.com/TypeTXT":      {txtRec("v=STSv1; id=20261005")},
		"_smtp._tls.mail.example.com/TypeTXT":    {txtRec("v=TLSRPTv1; rua=mailto:tls@example.com")},
		"default._bimi.mail.example.com/TypeTXT": {txtRec("v=BIMI1; l=https://example.com/logo.svg; a=https://example.com/vmc.pem")},
		"_25._tcp.mx1.example.com/52": {&dnsmessage.UnknownResource{Type: typeTLSA, Data: []byte{3, 1, 1, 0xAB}},
			&dnsmessage.UnknownResource{Type: typeTLSA, Data: []byte{3, 1, 1, 0xCD}}},
		"_25._tcp.mx2.example.com/52": {},
	})
	was := resolverAddr
	resolverAddr = func() string { return addr }
	t.Cleanup(func() { resolverAddr = was })

	r := CheckDomain("mail.example.com")
	if len(r.Errors) > 0 {
		t.Fatalf("errors %v", r.Errors)
	}
	if strings.Join(r.MX, ",") != "mx1.example.com,mx2.example.com" || r.SPFAll != "-all" {
		t.Fatalf("mx %v spf %q", r.MX, r.SPFAll)
	}
	if r.Policy != "reject" || r.SubPolicy != "quarantine" || r.Pct != 50 || !r.Reports {
		t.Fatalf("dmarc %+v", r)
	}
	if r.MTASTS != "20261005" || !r.TLSRPT || r.BIMI != "https://example.com/logo.svg" || !r.BIMICert {
		t.Fatalf("mta-sts %q tls-rpt %v bimi %q %v", r.MTASTS, r.TLSRPT, r.BIMI, r.BIMICert)
	}
	if len(r.DANE) != 2 || r.DANE[0].Records != 2 || r.DANE[1].Records != 0 {
		t.Fatalf("dane %+v", r.DANE)
	}
	if !r.Checked || !r.DNSSEC {
		t.Fatalf("dnssec %v checked %v", r.DNSSEC, r.Checked)
	}
	if c, ok := CachedDomain("MAIL.example.com"); !ok || c.Policy != "reject" {
		t.Fatalf("cache %+v %v", c, ok)
	}
}

// A domain that publishes nothing, unsigned: none of it, and no DNSSEC.
func TestCheckDomainWithNothing(t *testing.T) {
	addr := fakeResolver(t, false, map[string][]dnsmessage.ResourceBody{})
	was := resolverAddr
	resolverAddr = func() string { return addr }
	t.Cleanup(func() { resolverAddr = was })
	r := CheckDomain("bare.example")
	if len(r.MX) != 0 || r.SPF != "" || r.DMARC != "" || r.MTASTS != "" || r.BIMI != "" || r.DNSSEC || len(r.Errors) != 0 {
		t.Fatalf("bare %+v", r)
	}
}

// How an SPF record ends.
func TestSPFAll(t *testing.T) {
	for rec, want := range map[string]string{"v=spf1 mx -all": "-all", "v=spf1 ~all": "~all", "v=spf1 +all": "+all",
		"v=spf1 all": "+all", "v=spf1 redirect=_spf.example.com": "", "v=spf1 ?all": "?all"} {
		if got := spfAll(rec); got != want {
			t.Errorf("%q: %q, want %q", rec, got, want)
		}
	}
}
