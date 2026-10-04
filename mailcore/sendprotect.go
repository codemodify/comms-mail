package mailcore

import (
	"errors"
	"fmt"
	"strings"
)

// Sending signed and encrypted mail. comms-mail builds the message as it
// always does; secretvault signs it with the writer's key for the From
// address and encrypts it to each recipient's key from its contacts (and
// to the writer's own, so Sent stays readable), OpenPGP or S/MIME
// (mail.compose). What goes out — and what is filed in Sent — is what
// secretvault returned.
//
// While secretvault is locked a message waits in the Outbox, as one does
// for a locked password store, and is protected when it goes. A draft of
// a message to be encrypted is kept on this machine only: it is never
// written to the server's Drafts.

// Protection is how the writer asked a message to go.
type Protection struct {
	// Sign signs it. SignIfKey signs it if secretvault holds a key for
	// the From address: the Write window's default when it could not ask
	// (secretvault locked), since signing is on whenever there is a key.
	Sign      bool `json:"sign,omitempty"`
	SignIfKey bool `json:"signIfKey,omitempty"`
	Encrypt   bool `json:"encrypt,omitempty"`
}

func (p *Protection) any() bool { return p != nil && (p.Sign || p.SignIfKey || p.Encrypt) }

// encrypts says p encrypts.
func (p *Protection) encrypts() bool { return p != nil && p.Encrypt }

// SigningKeys is what the Write window needs to know of a From address.
type SigningKeys struct {
	// Available says secretvault is the store in use, so messages can be
	// signed and encrypted at all.
	Available bool `json:"available"`
	// Locked: secretvault is locked, so whether it holds a key is not
	// known (the window signs if it does: SignIfKey).
	Locked bool `json:"locked,omitempty"`
	// CanSign: secretvault holds a key for the address — an OpenPGP key
	// or an S/MIME certificate.
	CanSign bool   `json:"canSign,omitempty"`
	Why     string `json:"why,omitempty"`
}

// SigningKeys says whether secretvault can sign as from. It never asks
// secretvault to unlock.
func (s *LocalStore) SigningKeys(from string) SigningKeys {
	if s.secretKind() != StoreSecretVault {
		return SigningKeys{Why: "secretvault signs and encrypts mail: choose it in Settings › Privacy."}
	}
	out := SigningKeys{Available: true}
	switch err := (secretVaultStore{theSecretVault}).Ready(); {
	case errors.Is(err, ErrLocked):
		out.Locked = true
		return out
	case err != nil:
		out.Why = err.Error()
		return out
	}
	can, err := hasKeyFor(ExtractAddr(from))
	if err != nil {
		out.Why = err.Error()
	}
	out.CanSign = can
	return out
}

// hasKeyFor asks secretvault whether it holds a key that signs as addr:
// an OpenPGP key with the address, or an S/MIME certificate for it.
func hasKeyFor(addr string) (bool, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false, nil
	}
	var key struct {
		Fingerprint string `json:"fingerprint"`
	}
	err := theSecretVault.call("pgp.public", map[string]string{"key": addr}, &key)
	if err == nil {
		return true, nil
	}
	switch svCode(err) {
	case -32602: // several keys have the address: one is chosen when signing
		return true, nil
	case svCodeNotFound:
	default:
		return false, err
	}
	var certs []struct {
		Emails []string `json:"emails"`
	}
	if err := theSecretVault.call("smime.list", map[string]string{}, &certs); err != nil && svCode(err) != svCodeNotFound {
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

// protect has secretvault sign and encrypt raw as msg.Protect asks, and
// returns what is to be sent. ErrLocked means secretvault is locked: the
// message waits in the Outbox. Any other error is the writer's to see —
// no key for From, a recipient without one.
func (s *LocalStore) protect(raw []byte, msg Message) ([]byte, error) {
	p := msg.Protect
	if !p.any() {
		return raw, nil
	}
	if s.secretKind() != StoreSecretVault {
		if p.Sign || p.Encrypt {
			return nil, errors.New("signing and encrypting are secretvault's: choose it in Settings › Privacy, or send without them")
		}
		return raw, nil // signing only if there is a key, and there is none
	}
	if err := (secretVaultStore{theSecretVault}).Ready(); err != nil {
		return nil, err
	}
	sign := p.Sign
	if p.SignIfKey && !sign {
		can, err := hasKeyFor(ExtractAddr(msg.From))
		if err != nil {
			return nil, err
		}
		sign = can
	}
	if !sign && !p.Encrypt {
		return raw, nil
	}
	if p.Encrypt && strings.TrimSpace(msg.Bcc) != "" {
		// secretvault encrypts to the recipients the message names, and
		// Bcc is not among them (it would tell everyone who else got it).
		return nil, errors.New("an encrypted message cannot have Bcc recipients: they could not read it. Move them to To or Cc, or send it without encryption")
	}
	var out struct {
		Message []byte `json:"message"`
		Format  string `json:"format"`
	}
	err := theSecretVault.call("mail.compose", map[string]any{"message": raw, "sign": sign, "encrypt": p.Encrypt}, &out)
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
