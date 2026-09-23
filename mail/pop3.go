package mail

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// pop3Client is a small RFC 1939 session (mailclientd only).
// Enough for inbox retrieve (USER/PASS, UIDL, RETR). Gaps: no TOP-only
// preview, no server-side folders/flags, leave-on-server (no DELE).
type pop3Client struct {
	cfg         ServerConfig
	user        string
	timeout     time.Duration
	conn        net.Conn
	r           *bufio.Reader
	host        string
	mustEncrypt bool
}

type popUIDL struct {
	N    int
	UIDL string
}

func newPOP3Client(cfg ServerConfig, address string) *pop3Client {
	return &pop3Client{
		cfg:     cfg,
		user:    cfg.Username(address),
		timeout: 20 * time.Second,
	}
}

func (c *pop3Client) mode() TLSMode { return c.cfg.Mode(popPorts) }

func (c *pop3Client) connect() error {
	if strings.TrimSpace(c.cfg.Host) == "" {
		return fmt.Errorf("pop3: empty host")
	}
	host := c.cfg.HostPort(popPorts)
	to := c.timeout
	if to <= 0 {
		to = 20 * time.Second
	}
	mode := c.mode()
	conn, err := dialMode(host, mode, to)
	if err != nil {
		return fmt.Errorf("pop3: connect %s: %w", host, err)
	}
	c.conn = conn
	c.r = bufio.NewReaderSize(conn, 256*1024)
	c.host = host
	c.mustEncrypt = mode.Encrypted()
	c.touch()
	greet, err := c.readLine()
	if err != nil {
		c.drop()
		return fmt.Errorf("pop3: greeting: %w", err)
	}
	if !strings.HasPrefix(greet, "+OK") {
		c.drop()
		return fmt.Errorf("pop3: greeting %s", greet)
	}
	if mode == TLSStartTLS {
		if !c.serverHasSTLS() {
			c.drop()
			return errNoSTARTTLS("pop3", host)
		}
		if _, err := c.cmd("STLS"); err != nil {
			c.drop()
			return fmt.Errorf("pop3: STLS: %w", err)
		}
		tlsConn, err := upgradeToTLS(c.conn, host)
		if err != nil {
			c.drop()
			return fmt.Errorf("pop3: TLS handshake: %w", err)
		}
		c.conn = tlsConn
		c.r = bufio.NewReaderSize(tlsConn, 256*1024)
		c.touch()
	}
	if err := c.login(); err != nil {
		c.drop()
		return err
	}
	return nil
}

// serverHasSTLS asks CAPA whether the upgrade is offered. A server that does
// not implement CAPA at all is given the benefit of the doubt (the STLS
// command itself then fails closed).
func (c *pop3Client) serverHasSTLS() bool {
	if _, err := c.cmd("CAPA"); err != nil {
		return true
	}
	lines, err := c.readDot()
	if err != nil {
		return true
	}
	for _, ln := range lines {
		if strings.EqualFold(strings.TrimSpace(ln), "STLS") {
			return true
		}
	}
	return false
}

// touch re-arms the idle deadline. POP3 uses a per-command deadline rather
// than one deadline for the whole session, so retrieving a large mailbox
// cannot time out halfway through.
func (c *pop3Client) touch() {
	if c.conn == nil {
		return
	}
	to := c.timeout
	if to <= 0 {
		to = 20 * time.Second
	}
	_ = c.conn.SetDeadline(time.Now().Add(to))
}

func (c *pop3Client) drop() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.r = nil
}

func (c *pop3Client) login() error {
	pass := c.cfg.Password()
	if pass != "" && !c.mustEncrypt && !isLoopbackHost(c.host) {
		return fmt.Errorf("pop3: refusing to send credentials to %s over an unencrypted connection "+
			`(use "tlsMode":"ssl" or "starttls")`, c.host)
	}
	if pass == "" && c.cfg.Pass == "" && c.cfg.PassEnv == "" {
		// Greeting-only probe (Test connection before a password is typed).
		return nil
	}
	if pass == "" {
		return fmt.Errorf("pop3: empty password (set pop.password in mail.json, or %s / passEnv)", EnvPass)
	}
	if _, err := c.cmd("USER " + c.user); err != nil {
		return fmt.Errorf("pop3 USER: %w", err)
	}
	if _, err := c.cmd("PASS " + pass); err != nil {
		return fmt.Errorf("pop3 PASS: %w", err)
	}
	return nil
}

func (c *pop3Client) uidl() ([]popUIDL, error) {
	if _, err := c.cmd("UIDL"); err != nil {
		return nil, err
	}
	lines, err := c.readDot()
	if err != nil {
		return nil, err
	}
	var out []popUIDL
	for _, ln := range lines {
		fields := strings.Fields(ln)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		out = append(out, popUIDL{N: n, UIDL: fields[1]})
	}
	return out, nil
}

func (c *pop3Client) stat() (int, error) {
	ln, err := c.cmd("STAT")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(strings.TrimPrefix(ln, "+OK"))
	if len(fields) < 1 {
		return 0, fmt.Errorf("pop3 STAT: %s", ln)
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, fmt.Errorf("pop3 STAT: %s", ln)
	}
	return n, nil
}

func (c *pop3Client) retr(n int) ([]byte, error) {
	if _, err := c.cmd(fmt.Sprintf("RETR %d", n)); err != nil {
		return nil, err
	}
	return c.readDotRaw()
}

func (c *pop3Client) cmd(s string) (string, error) {
	c.touch()
	if err := writeCRLF(c.conn, s); err != nil {
		return "", err
	}
	ln, err := c.readLine()
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(ln, "+OK") {
		return ln, nil
	}
	return ln, fmt.Errorf("%s", ln)
}

func (c *pop3Client) readLine() (string, error) {
	c.touch()
	ln, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(ln, "\r\n"), nil
}

func (c *pop3Client) readDot() ([]string, error) {
	var lines []string
	for {
		ln, err := c.readLine()
		if err != nil {
			return nil, err
		}
		if ln == "." {
			return lines, nil
		}
		if strings.HasPrefix(ln, "..") {
			ln = ln[1:]
		}
		lines = append(lines, ln)
	}
}

func (c *pop3Client) readDotRaw() ([]byte, error) {
	var b strings.Builder
	first := true
	for {
		ln, err := c.readLine()
		if err != nil {
			// A stream that ends without the "." terminator is truncated:
			// returning it would cache a half message as if it were whole.
			if err == io.EOF {
				return nil, fmt.Errorf("pop3: connection closed before end of message")
			}
			return nil, err
		}
		if ln == "." {
			break
		}
		if strings.HasPrefix(ln, "..") {
			ln = ln[1:]
		}
		if !first {
			b.WriteString("\r\n")
		}
		first = false
		b.WriteString(ln)
	}
	return []byte(b.String()), nil
}

func (c *pop3Client) close() {
	if c.conn == nil {
		return
	}
	_, _ = c.cmd("QUIT")
	c.drop()
}

func popMessageID(accountID, uidl string) MessageID {
	safe := safeID(uidl)
	if safe == "" || safe == "acct" {
		safe = "msg"
	}
	return MessageID(safeID(accountID) + "-pop-" + safe)
}
