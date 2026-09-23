package mail

import (
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

// SendSMTP submits RFC822 on the account's SMTP server.
func SendSMTP(cfg ServerConfig, from string, to []string, raw []byte) error {
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("smtp: empty host")
	}
	host := cfg.HostPort(smtpPorts)
	mode := cfg.Mode(smtpPorts)
	user := cfg.Username(from)
	pass := cfg.Password()
	authName := strings.ToLower(strings.TrimSpace(cfg.Auth))
	name := serverName(host)

	conn, err := dialMode(host, mode, 20*time.Second)
	if err != nil {
		return fmt.Errorf("smtp: connect %s: %w", host, err)
	}
	client, err := smtp.NewClient(conn, name)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()

	if mode == TLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			// Fail closed: a stripped 250-STARTTLS used to fall through to
			// a cleartext AUTH, which handed the password to the attacker.
			return errNoSTARTTLS("smtp", host)
		}
		if err := client.StartTLS(tlsClientConfig(host)); err != nil {
			return fmt.Errorf("smtp: STARTTLS: %w", err)
		}
	}
	encrypted := mode.Encrypted()

	if authName == "xoauth2" {
		if !encrypted && !isLoopbackHost(host) {
			return fmt.Errorf("smtp: refusing to send a bearer token to %s in the clear", host)
		}
		token, err := resolveAccessToken(cfg, user)
		if err != nil {
			return err
		}
		if err := client.Auth(smtpXOAuth2{user: user, token: token}); err != nil {
			return fmt.Errorf("smtp: AUTH XOAUTH2: %w", err)
		}
	} else if user != "" && pass != "" {
		if !encrypted && !isLoopbackHost(host) {
			return fmt.Errorf("smtp: refusing to send credentials to %s over an unencrypted connection "+
				`(use "tlsMode":"ssl" or "starttls")`, host)
		}
		if err := client.Auth(smtp.PlainAuth("", user, pass, name)); err != nil {
			// AUTH LOGIN is only tried on a connection we know is encrypted
			// (or loopback); it has no TLS check of its own.
			if err2 := client.Auth(smtpLogin{user, pass}); err2 != nil {
				return fmt.Errorf("smtp: AUTH: %v / %v", err, err2)
			}
		}
	}
	if err := client.Mail(extractAddr(from)); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	sent := 0
	for _, rcpt := range to {
		rcpt = extractAddr(rcpt)
		if rcpt == "" {
			continue
		}
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp: RCPT %s: %w", rcpt, err)
		}
		sent++
	}
	if sent == 0 {
		return fmt.Errorf("smtp: no usable recipient in To/Cc/Bcc")
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

type smtpXOAuth2 struct{ user, token string }

func (a smtpXOAuth2) Start(server *smtp.ServerInfo) (string, []byte, error) {
	raw := "user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"
	return "XOAUTH2", []byte(raw), nil
}

func (a smtpXOAuth2) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return []byte{}, fmt.Errorf("smtp: XOAUTH2 rejected: %s", string(fromServer))
	}
	return nil, nil
}

type smtpLogin struct{ user, pass string }

func (a smtpLogin) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte(a.user), nil
}

func (a smtpLogin) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	s := strings.ToLower(string(fromServer))
	if strings.Contains(s, "user") {
		return []byte(a.user), nil
	}
	return []byte(a.pass), nil
}

func extractAddr(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			return strings.TrimSpace(s[i+1 : i+j])
		}
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func splitAddrs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if a := extractAddr(p); a != "" {
			out = append(out, a)
		}
	}
	return out
}
