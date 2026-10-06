package mailcore

import (
	"net"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// The route a message took, from its Received headers: each server that
// passed it on wrote one at the top, so read from the bottom up they are
// its way from the sender's computer to your mailbox. Each says where it
// came from, which server took it, how — the protocol, and TLS where the
// server noted it — and when. Those your provider's servers wrote can be
// believed; those under them came with the message, written by the
// sender's servers or by anyone.

// RouteHop is one Received header: one server taking the message.
type RouteHop struct {
	// From is where it came from as the server saw it — a name, and the
	// address it connected from — and By the server that took it.
	From   string `json:"from,omitempty"`
	FromIP string `json:"fromIp,omitempty"`
	By     string `json:"by,omitempty"`
	// With is the protocol: SMTP, ESMTPS, ESMTPSA, LMTP, HTTP…
	With string `json:"with,omitempty"`
	// TLS is HopTLS…; Version and Cipher what the server noted of it.
	TLS     string `json:"tls"`
	Version string `json:"version,omitempty"`
	Cipher  string `json:"cipher,omitempty"`
	// Auth: the sender signed in to hand it over (its own server taking
	// it from its app).
	Auth bool `json:"auth,omitempty"`
	// At is when it was taken.
	At time.Time `json:"at,omitzero"`
	// Inside: from one server of an organisation to another of its own,
	// or within one server — not across the internet. Yours: your
	// provider's servers wrote it, so it can be believed.
	Inside bool `json:"inside,omitempty"`
	Yours  bool `json:"yours,omitempty"`
}

// A hop's TLS.
const (
	HopTLS     = "tls"     // encrypted
	HopClear   = "clear"   // in the clear
	HopUnknown = "unknown" // the server did not say
)

var (
	// receivedFrom is "from name (rdns [ip])": the name the sender gave,
	// and what the server found.
	receivedFrom = regexp.MustCompile(`(?i)^\s*from\s+(\S+)(?:\s+\(([^()]*(?:\([^()]*\)[^()]*)*)\))?`)
	receivedWith = regexp.MustCompile(`(?i)\bwith\s+([a-z0-9-]+(?:\s+smtp\s+server)?)`)
	bracketIP    = regexp.MustCompile(`\[(?:ipv6:)?([0-9a-fA-F:.]+)\]`)
	tlsVersion   = regexp.MustCompile(`(?i)\btls\s?v?(1)[._ ]?([0-3])\b`)
	tlsCipher    = regexp.MustCompile(`(?i)(?:cipher[= ]|\btls\s+)((?:TLS_|ECDHE|DHE|AES|CHACHA)[A-Z0-9_-]+)`)
	authSender   = regexp.MustCompile(`(?i)authenticated\s+sender|\bauth=pass\b|\(authenticated\b`)
	sslVersion   = regexp.MustCompile(`(?i)\bSSLv3\b`)
)

// uncommented is v without its comments, nested ones too.
func uncommented(v string) string {
	for {
		w := authComment.ReplaceAllString(v, " ")
		if w == v {
			return w
		}
		v = w
	}
}

// isAddr says s is an IP address, or reads as one: Google's servers name
// themselves by their IPv6 address.
func isAddr(s string) bool { return net.ParseIP(s) != nil || strings.Count(s, ":") >= 2 }

// readReceived reads one Received header.
func readReceived(v string) RouteHop {
	v = strings.Join(strings.Fields(strings.ReplaceAll(v, "\r\n", " ")), " ")
	var h RouteHop
	when := ""
	if i := strings.LastIndexByte(v, ';'); i >= 0 {
		v, when = v[:i], strings.TrimSpace(v[i+1:])
	}
	if t, err := mail.ParseDate(strings.TrimSpace(authComment.ReplaceAllString(when, ""))); err == nil {
		h.At = t
	} else if t, err := mail.ParseDate(when); err == nil {
		h.At = t
	}
	if m := receivedFrom.FindStringSubmatch(v); m != nil {
		h.From = strings.ToLower(strings.Trim(m[1], "[]."))
		rdns := strings.Fields(m[2])
		switch ip := bracketIP.FindStringSubmatch(m[0]); {
		case ip != nil:
			h.FromIP = ip[1]
		case len(rdns) > 0 && net.ParseIP(rdns[0]) != nil:
			h.FromIP = rdns[0] // Microsoft's: "(2603:10b6::12)"
		case net.ParseIP(h.From) != nil:
			h.FromIP = h.From
		}
		// A name the sender made up, where the server found a real one:
		// "from [1.2.3.4] (mail.example.com [1.2.3.4])" names the latter.
		// "helo=" is what the sender said of itself.
		if len(rdns) > 0 && strings.Contains(rdns[0], ".") && !strings.Contains(rdns[0], "=") && net.ParseIP(strings.Trim(rdns[0], "[]")) == nil &&
			(net.ParseIP(h.From) != nil || !strings.Contains(h.From, ".")) {
			h.From = strings.ToLower(strings.TrimSuffix(rdns[0], "."))
		}
	}
	plain := uncommented(v)
	if m := receivedBy.FindStringSubmatch(plain); m != nil {
		h.By = strings.ToLower(m[1])
	} else if i := strings.Index(strings.ToLower(v), " by "); i >= 0 || strings.HasPrefix(strings.ToLower(v), "by ") {
		rest := strings.Fields(v[max(i, 0):])
		for k, f := range rest {
			if strings.EqualFold(f, "by") && k+1 < len(rest) {
				h.By = strings.ToLower(strings.Trim(rest[k+1], "[]"))
				break
			}
		}
	}
	if m := receivedWith.FindStringSubmatch(plain); m != nil {
		h.With = m[1]
		if !strings.Contains(strings.ToLower(h.With), " ") {
			h.With = strings.ToUpper(h.With)
		}
	}
	if m := tlsVersion.FindStringSubmatch(v); m != nil {
		h.Version = "TLS " + m[1] + "." + m[2]
	} else if sslVersion.MatchString(v) {
		h.Version = "SSL 3"
	}
	if m := tlsCipher.FindStringSubmatch(v); m != nil {
		h.Cipher = strings.ToUpper(m[1])
	}
	with := strings.ToUpper(h.With)
	h.Auth = strings.HasSuffix(with, "SMTPA") || strings.HasSuffix(with, "SMTPSA") || authSender.MatchString(v)
	switch {
	case h.Version != "" || h.Cipher != "":
		h.TLS = HopTLS
	case with == "ESMTPS" || with == "ESMTPSA" || with == "LMTPS" || with == "UTF8SMTPS" || with == "UTF8SMTPSA" ||
		with == "HTTPS" || with == "LMTPSA" || strings.Contains(strings.ToLower(v), "using tls"):
		h.TLS = HopTLS
	case with == "SMTP" || with == "ESMTP" || with == "ESMTPA" || with == "LMTP" || with == "HTTP" || with == "UTF8SMTP" || with == "UTF8SMTPA":
		h.TLS = HopClear
	default:
		h.TLS = HopUnknown
	}
	return h
}

// isLoopback says a hop's source is the same machine.
func isLoopback(h RouteHop) bool {
	if ip := net.ParseIP(h.FromIP); ip != nil && ip.IsLoopback() {
		return true
	}
	return h.From == "localhost" || h.From == "localhost.localdomain"
}

// routeOf is the message's route, first hop first. provider are your
// provider's organisations (none when there is no account): the hops
// above the first your provider took it in are yours.
func routeOf(h mail.Header, provider map[string]bool) []RouteHop {
	all := h["Received"]
	hops := make([]RouteHop, 0, len(all))
	for _, v := range all {
		hops = append(hops, readReceived(v))
	}
	// Topmost first, as written: your provider's own come first, down to
	// the one that took it in from outside.
	ours := len(provider) > 0
	for i := range hops {
		hop := &hops[i]
		byOrg := registrableDomain(hop.By)
		fromOrg := registrableDomain(hop.From)
		hop.Inside = isLoopback(*hop) || hop.From == "" && hop.FromIP == "" ||
			byOrg != "" && byOrg == fromOrg || strings.HasPrefix(strings.ToUpper(hop.With), "LMTP") ||
			hop.By != "" && isAddr(hop.By) && hop.From == ""
		if !ours {
			continue
		}
		mine := hop.By == "" || provider[byOrg] || isAddr(hop.By)
		if !mine {
			ours = false
			continue
		}
		hop.Yours = true
		if !provider[fromOrg] && !hop.Inside {
			ours = false // it came in from outside here: what is under it is theirs
		}
	}
	for i, j := 0, len(hops)-1; i < j; i, j = i+1, j-1 {
		hops[i], hops[j] = hops[j], hops[i]
	}
	return hops
}
