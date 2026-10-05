package mailcore

import (
	"errors"
	"fmt"
	"strings"
)

// Sending signed and encrypted mail. comms-mail builds the message as it
// always does; then the engine chosen for a format (keychoice.go) signs it
// with the writer's key for the From address and encrypts it to each
// recipient's key, and to the writer's own so Sent stays readable:
// secretvault (mail.compose), or comms-mail's own (pgpown.go,
// smimeown.go). OpenPGP is tried first, then S/MIME — the first that can
// do what was asked. With secretvault doing both, it picks the format
// itself, as it always has. What goes out — and what is filed in Sent —
// is what the engine returned.
//
// While the keys cannot be reached — secretvault locked, or where
// comms-mail keeps its own keys — a message waits in the Outbox, as one
// does for a locked password store, and is protected when it goes. A draft
// of a message to be encrypted is kept on this machine only: it is never
// written to the server's Drafts.

// Protection is how the writer asked a message to go.
type Protection struct {
	// Sign signs it. SignIfKey signs it if there is a key for the From
	// address: the Write window's default when it could not ask (the keys
	// locked), since signing is on whenever there is a key.
	Sign      bool `json:"sign,omitempty"`
	SignIfKey bool `json:"signIfKey,omitempty"`
	Encrypt   bool `json:"encrypt,omitempty"`
}

func (p *Protection) any() bool { return p != nil && (p.Sign || p.SignIfKey || p.Encrypt) }

// encrypts says p encrypts.
func (p *Protection) encrypts() bool { return p != nil && p.Encrypt }

// SigningKeys is what the Write window needs to know of a From address.
type SigningKeys struct {
	// Available says messages can be signed and encrypted at all: an
	// engine is there for a format.
	Available bool `json:"available"`
	// Locked: the keys cannot be reached now, so whether there is one is
	// not known (the window signs if there is: SignIfKey).
	Locked bool `json:"locked,omitempty"`
	// CanSign: there is a key for the address — an OpenPGP key or an
	// S/MIME certificate.
	CanSign bool   `json:"canSign,omitempty"`
	Why     string `json:"why,omitempty"`
}

// SigningKeys says whether either format can sign as from. It never asks
// anything to unlock.
func (s *LocalStore) SigningKeys(from string) SigningKeys {
	var out SigningKeys
	var why []string
	for _, f := range keyFormats {
		can, err := s.canSignIn(f, ExtractAddr(from))
		switch {
		case err == nil:
			out.Available = true
			out.CanSign = out.CanSign || can
		case errors.Is(err, ErrLocked):
			out.Available, out.Locked = true, true
		default:
			why = append(why, err.Error())
		}
	}
	if out.CanSign {
		out.Locked = false
	}
	if !out.Available {
		out.Why = strings.Join(why, "; ")
	}
	return out
}

// canSignIn says whether format's engine can sign as addr: comms-mail's
// own when it has a key for it, secretvault when it holds one. An error
// says the engine cannot be asked: ErrLocked, or why not.
func (s *LocalStore) canSignIn(format, addr string) (bool, error) {
	if s.engineOf(format) == EngineOwn {
		switch format {
		case FormatOpenPGP:
			return s.pgpCanSign(addr), nil
		case FormatSMIME:
			return s.smimeCanSign(addr), nil
		}
		return false, nil
	}
	if err := theSecretVault.available(); err != nil {
		return false, fmt.Errorf("secretvault, which signs %s here, is not running", formatName(format))
	}
	vault := s.svVaultOf(format)
	if err := svVaultReady(vault); err != nil {
		return false, err
	}
	return hasKeyIn(format, addr, vault)
}

// formatName is a format as people write it.
func formatName(format string) string {
	if format == FormatSMIME {
		return "S/MIME"
	}
	return "OpenPGP"
}

// hasKeyFor asks secretvault whether it holds a key that signs as addr,
// in vault: an OpenPGP key with the address, or an S/MIME certificate for
// it.
func hasKeyFor(addr, vault string) (bool, error) {
	for _, f := range keyFormats {
		if ok, err := hasKeyIn(f, addr, vault); ok || err != nil {
			return ok, err
		}
	}
	return false, nil
}

// hasKeyIn asks secretvault whether it holds a key of format that signs
// as addr, in vault ("" its default).
func hasKeyIn(format, addr, vault string) (bool, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false, nil
	}
	if format == FormatOpenPGP {
		var key struct {
			Fingerprint string `json:"fingerprint"`
		}
		err := theSecretVault.call("pgp.public", withVault(map[string]any{"key": addr}, vault), &key)
		switch {
		case err == nil:
			return true, nil
		case svCode(err) == -32602: // several keys have the address: one is chosen when signing
			return true, nil
		case svCode(err) == svCodeNotFound:
			return false, nil
		case svCode(err) == svCodeLocked:
			return false, ErrLocked
		}
		return false, err
	}
	var certs []struct {
		Emails []string `json:"emails"`
	}
	if err := theSecretVault.call("smime.list", withVault(map[string]any{}, vault), &certs); err != nil {
		switch svCode(err) {
		case svCodeNotFound:
			return false, nil
		case svCodeLocked:
			return false, ErrLocked
		}
		return false, err
	}
	for _, c := range certs {
		for _, e := range c.Emails {
			if strings.EqualFold(e, addr) {
				return true, nil
			}
		}
	}
	return false, nil
}

// protect signs and encrypts raw as msg.Protect asks, and returns what is
// to be sent. ErrLocked means the keys cannot be reached now: the message
// waits in the Outbox. Any other error is the writer's to see — no key for
// From, a recipient without one. With comms-mail's own OpenPGP and a key
// for From, the message offers it (Autocrypt) whether protected or not.
func (s *LocalStore) protect(raw []byte, msg Message) ([]byte, error) {
	from := fromAddress(msg.From)
	raw = s.withAutocrypt(raw, from)
	p := msg.Protect
	if !p.any() {
		return raw, nil
	}
	if p.Encrypt && strings.TrimSpace(msg.Bcc) != "" {
		// A message is encrypted to the recipients it names, and Bcc is
		// not among them (it would tell everyone who else got it).
		return nil, errors.New("an encrypted message cannot have Bcc recipients: they could not read it. Move them to To or Cc, or send it without encryption")
	}
	if s.engineOf(FormatOpenPGP) == EngineSecretVault && s.engineOf(FormatSMIME) == EngineSecretVault &&
		s.svVaultOf(FormatOpenPGP) == s.svVaultOf(FormatSMIME) {
		return s.svProtect(raw, msg, "")
	}
	rcpts := recipientAddrs(msg)
	var why []string
	locked := false
	for _, f := range keyFormats {
		can, err := s.canSignIn(f, from)
		if errors.Is(err, ErrLocked) {
			locked = true
			continue
		}
		if err != nil {
			why = append(why, err.Error())
			continue
		}
		if p.Sign && !can {
			why = append(why, "no "+formatName(f)+" key for "+from)
			continue
		}
		sign := p.Sign || p.SignIfKey && can
		if p.Encrypt {
			if missing := s.missingRecipients(f, rcpts); len(missing) > 0 {
				why = append(why, "no "+formatName(f)+" key for "+strings.Join(missing, ", "))
				continue
			}
		} else if !sign {
			continue
		}
		out, err := s.composeIn(f, raw, msg, from, rcpts, sign, p.Encrypt)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	if !p.Sign && !p.Encrypt {
		return raw, nil // signing only if there is a key, and there is none
	}
	if locked {
		return nil, ErrLocked
	}
	return nil, fmt.Errorf("the message cannot be signed or encrypted as asked, so it was not sent: %s", strings.Join(why, "; "))
}

// recipientAddrs are a message's To and Cc addresses (never Bcc).
func recipientAddrs(msg Message) []string {
	var out []string
	for _, list := range []string{msg.To, msg.Cc} {
		for _, a := range splitAddrs(list) {
			if addr := normAddr(a); addr != "" {
				out = append(out, addr)
			}
		}
	}
	return out
}

// missingRecipients are the recipients format's engine has no key for:
// comms-mail's own knows; secretvault is left to say when it is asked.
func (s *LocalStore) missingRecipients(format string, rcpts []string) []string {
	if s.engineOf(format) != EngineOwn {
		return nil
	}
	switch format {
	case FormatOpenPGP:
		_, missing := s.pgpRecipients(rcpts)
		return missing
	case FormatSMIME:
		_, missing := s.smimeRecipients(rcpts)
		return missing
	}
	return rcpts
}

// composeIn has format's engine sign and encrypt raw.
func (s *LocalStore) composeIn(format string, raw []byte, msg Message, from string, rcpts []string, sign, encrypt bool) ([]byte, error) {
	if s.engineOf(format) == EngineOwn {
		switch format {
		case FormatOpenPGP:
			return s.pgpCompose(raw, from, rcpts, sign, encrypt)
		case FormatSMIME:
			return s.smimeCompose(raw, from, rcpts, sign, encrypt)
		}
	}
	m := msg
	m.Protect = &Protection{Sign: sign, Encrypt: encrypt}
	return s.svProtect(raw, m, format)
}

// svProtect has secretvault sign and encrypt raw, in format ("" lets it
// choose, both formats' keys being in one vault), with the keys in the
// vault chosen for them.
func (s *LocalStore) svProtect(raw []byte, msg Message, format string) ([]byte, error) {
	p := msg.Protect
	if err := theSecretVault.available(); err != nil {
		if p.Sign || p.Encrypt {
			return nil, errors.New("secretvault, which signs and encrypts mail here, is not running: start it, or send the message without signing or encrypting")
		}
		return raw, nil // signing only if there is a key, and none can be asked about
	}
	vault := s.svVaultOf(firstNonEmpty(format, FormatOpenPGP))
	if err := svVaultReady(vault); err != nil {
		return nil, err
	}
	sign := p.Sign
	if p.SignIfKey && !sign {
		var can bool
		var err error
		if format == "" {
			can, err = hasKeyFor(ExtractAddr(msg.From), vault)
		} else {
			can, err = hasKeyIn(format, ExtractAddr(msg.From), vault)
		}
		if err != nil {
			return nil, err
		}
		sign = can
	}
	if !sign && !p.Encrypt {
		return raw, nil
	}
	var out struct {
		Message []byte `json:"message"`
		Format  string `json:"format"`
	}
	params := withVault(map[string]any{"message": raw, "sign": sign, "encrypt": p.Encrypt}, vault)
	if format != "" {
		params["format"] = format
	}
	err := theSecretVault.call("mail.compose", params, &out)
	switch svCode(err) {
	case 0:
		if err != nil {
			return nil, fmt.Errorf("secretvault: %w", err)
		}
	case svCodeLocked:
		return nil, ErrLocked
	case svCodeDenied, svCodeCanceled:
		return nil, errors.New("secretvault did not sign or encrypt the message, so it was not sent")
	default:
		return nil, fmt.Errorf("secretvault could not sign or encrypt the message: %w", err)
	}
	if len(out.Message) == 0 {
		return nil, errors.New("secretvault returned no message")
	}
	return out.Message, nil
}

// draftStaysHere says msg, a draft, is to be encrypted when sent, and so
// is kept on this machine only, never written to the server's Drafts.
func draftStaysHere(msg Message) bool { return msg.Protect.encrypts() }
