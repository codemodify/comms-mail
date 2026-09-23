package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"
)

// TLSMode is how one mail connection is secured. It is resolved once from
// ServerConfig (see ServerConfig.Mode) and every transport — IMAP, POP3,
// SMTP and the Add Account probe — obeys it verbatim.
//
// Semantics are fail-closed: TLSStartTLS aborts the connection when the
// server does not offer STARTTLS (no silent downgrade), and TLSPlain is
// only ever reached when the account explicitly asks for it.
type TLSMode string

const (
	// TLSImplicit wraps the socket in TLS from the first byte (993/995/465).
	TLSImplicit TLSMode = "ssl"
	// TLSStartTLS connects in the clear then requires an upgrade (143/110/587).
	TLSStartTLS TLSMode = "starttls"
	// TLSPlain never encrypts. Opt-in only; never a fallback.
	TLSPlain TLSMode = "plain"
)

// Valid reports whether m is one of the three known modes.
func (m TLSMode) Valid() bool {
	switch m {
	case TLSImplicit, TLSStartTLS, TLSPlain:
		return true
	}
	return false
}

// Encrypted is true for every mode that puts TLS on the wire.
func (m TLSMode) Encrypted() bool { return m == TLSImplicit || m == TLSStartTLS }

func (m TLSMode) String() string { return string(m) }

// parseTLSMode accepts the spellings used in mail.json and the probe.
func parseTLSMode(s string) (TLSMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ssl", "tls", "implicit", "implicit-tls", "smtps", "imaps", "pop3s":
		return TLSImplicit, true
	case "starttls", "stls", "start-tls", "upgrade":
		return TLSStartTLS, true
	case "plain", "none", "insecure", "cleartext":
		return TLSPlain, true
	}
	return "", false
}

// defaultPorts are the implicit-TLS and STARTTLS ports per protocol.
type defaultPorts struct {
	implicit string
	startTLS string
}

var (
	imapPorts = defaultPorts{implicit: "993", startTLS: "143"}
	popPorts  = defaultPorts{implicit: "995", startTLS: "110"}
	smtpPorts = defaultPorts{implicit: "465", startTLS: "587"}
)

func portOf(host string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(host))
	if err != nil {
		return ""
	}
	return port
}

// modeForPort maps a well-known port to its mode. ok is false for ports
// that carry no convention (custom / test servers).
func modeForPort(port string) (TLSMode, bool) {
	switch port {
	case "993", "995", "465":
		return TLSImplicit, true
	case "143", "110", "587", "25":
		return TLSStartTLS, true
	}
	return "", false
}

// Mode resolves the connection security for this server once, merging the
// explicit tlsMode field with the legacy tls / starttls booleans.
//
// Precedence (first match wins):
//
//  1. tlsMode: "ssl" | "starttls" | "plain"
//  2. starttls: true                      → STARTTLS
//  3. tls: true                           → implicit TLS, unless the port is a
//     known STARTTLS port, in which case STARTTLS (the old code silently
//     produced a cleartext session for this combination)
//  4. tls: false                          → port convention, else plain
//  5. nothing set                         → port convention, else the
//     protocol default (implicit TLS for IMAP/POP3, STARTTLS for SMTP)
func (s ServerConfig) Mode(def defaultPorts) TLSMode {
	if m, ok := parseTLSMode(s.TLSMode); ok {
		return m
	}
	port := portOf(s.Host)
	byPort, portKnown := modeForPort(port)
	if s.StartTLS != nil && *s.StartTLS {
		return TLSStartTLS
	}
	if s.TLS != nil && *s.TLS {
		// tls:true on 143/110/587 means "secure me", not "speak TLS to a
		// cleartext port": upgrade instead of falling through to plaintext.
		if portKnown && byPort == TLSStartTLS {
			return TLSStartTLS
		}
		return TLSImplicit
	}
	if s.StartTLS != nil && !*s.StartTLS && s.TLS != nil && !*s.TLS {
		if portKnown {
			return byPort
		}
		return TLSPlain
	}
	if s.TLS != nil && !*s.TLS {
		if portKnown {
			return byPort
		}
		return TLSPlain
	}
	if portKnown {
		return byPort
	}
	if def.implicit == smtpPorts.implicit {
		return TLSStartTLS
	}
	return TLSImplicit
}

// HostPort fills in the default port for the resolved mode.
func (s ServerConfig) HostPort(def defaultPorts) string {
	host := strings.TrimSpace(s.Host)
	if host == "" || strings.Contains(host, ":") {
		return host
	}
	if s.Mode(def) == TLSImplicit {
		return host + ":" + def.implicit
	}
	return host + ":" + def.startTLS
}

// tlsClientConfig is the single place TLS parameters are chosen. Certificate
// verification is always on — there is no opt-out anywhere in this package.
func tlsClientConfig(hostport string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: serverName(hostport),
	}
}

// dialMode connects according to mode. STARTTLS and plain both return a
// cleartext conn — the caller performs the protocol-specific upgrade.
func dialMode(hostport string, mode TLSMode, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	if mode == TLSImplicit {
		return tls.DialWithDialer(&d, "tcp", hostport, tlsClientConfig(hostport))
	}
	return d.Dial("tcp", hostport)
}

// upgradeToTLS performs the client half of a STARTTLS/STLS upgrade.
func upgradeToTLS(conn net.Conn, hostport string) (*tls.Conn, error) {
	t := tls.Client(conn, tlsClientConfig(hostport))
	if err := t.Handshake(); err != nil {
		return nil, err
	}
	return t, nil
}

// errNoSTARTTLS is the fail-closed error for a stripped upgrade.
func errNoSTARTTLS(proto, host string) error {
	return fmt.Errorf("%s: %s does not advertise STARTTLS — refusing to continue in the clear "+
		`(set "tlsMode":"plain" on this server in mail.json if that is really what you want)`, proto, host)
}

// isLoopbackHost is used to allow cleartext credentials against a local
// test/dev server while refusing them to a remote host.
func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(hostport))
	if err != nil {
		host = strings.TrimSpace(hostport)
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
