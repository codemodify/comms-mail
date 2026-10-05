package mailcore

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Your own keys, as Settings › Security › Keys shows them: for each
// address you send from, the OpenPGP key and the S/MIME certificates
// secretvault holds for it. Making a key and bringing in a .p12 are
// secretvault's work: comms-mail asks it to, and never holds a private
// key. It asks only about those addresses (pgp.public by address,
// smime.list), never for a list of what else the vault holds.

// OwnKeys is what Settings shows.
type OwnKeys struct {
	// Available says secretvault is the store in use; Locked that it is
	// locked, so nothing is known; Why why not, otherwise.
	Available bool          `json:"available"`
	Locked    bool          `json:"locked,omitempty"`
	Why       string        `json:"why,omitempty"`
	Addresses []AddressKeys `json:"addresses,omitempty"`
}

// AddressKeys are one address's keys.
type AddressKeys struct {
	Address string         `json:"address"`
	PGP     *OwnPGPKey     `json:"pgp,omitempty"`
	SMIME   []OwnSMIMECert `json:"smime,omitempty"`
}

// OwnPGPKey is an OpenPGP key of yours, without its secret.
type OwnPGPKey struct {
	Fingerprint string    `json:"fingerprint"`
	Created     time.Time `json:"created"`
	Expires     time.Time `json:"expires,omitempty"`
	// PublicKey is the armoured public key, to give to people.
	PublicKey string `json:"publicKey,omitempty"`
}

// OwnSMIMECert is an S/MIME certificate of yours (its private key stays in
// secretvault).
type OwnSMIMECert struct {
	Subject  string    `json:"subject"`
	Issuer   string    `json:"issuer"`
	NotAfter time.Time `json:"notAfter"`
	SHA256   string    `json:"sha256"`
	Warnings []string  `json:"warnings,omitempty"`
}

// ownAddresses are the addresses you send from: each identity's and each
// account's, once.
func (s *LocalStore) ownAddresses() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	add := func(a string) {
		a = strings.ToLower(strings.TrimSpace(ExtractAddr(a)))
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, id := range s.identities {
		add(id.Address)
	}
	for _, a := range s.cfg.Accounts {
		if !a.IsLocal() {
			add(a.Address)
		}
	}
	sort.Strings(out)
	return out
}

// OwnKeys asks secretvault which of your addresses it holds keys for —
// OpenPGP keys in the vault chosen for them, S/MIME certificates in
// theirs. It never asks secretvault to unlock.
func (s *LocalStore) OwnKeys() OwnKeys {
	pgp, sm := s.svOwnKeys(FormatOpenPGP), s.svOwnKeys(FormatSMIME)
	out := OwnKeys{Available: pgp.Available || sm.Available, Locked: pgp.Locked || sm.Locked, Why: firstNonEmpty(pgp.Why, sm.Why)}
	for i, ak := range pgp.Addresses {
		if i < len(sm.Addresses) && sm.Addresses[i].Address == ak.Address {
			ak.SMIME = sm.Addresses[i].SMIME
		}
		out.Addresses = append(out.Addresses, ak)
	}
	if len(pgp.Addresses) == 0 {
		out.Addresses = sm.Addresses
	}
	return out
}

// svOwnKeys asks secretvault which of your addresses it holds keys of
// format for, in the vault chosen for them.
func (s *LocalStore) svOwnKeys(format string) OwnKeys {
	if err := theSecretVault.available(); err != nil {
		return OwnKeys{Why: "secretvault, which keeps your keys here, is not running: start it, or keep them in another place."}
	}
	vault := s.svVaultOf(format)
	out := OwnKeys{Available: true}
	switch err := svVaultReady(vault); {
	case errors.Is(err, ErrLocked):
		out.Locked = true
		return out
	case err != nil:
		out.Why = err.Error()
		return out
	}
	var certs []struct {
		Subject  string    `json:"subject"`
		Issuer   string    `json:"issuer"`
		Emails   []string  `json:"emails"`
		SHA256   string    `json:"sha256"`
		NotAfter time.Time `json:"not_after"`
		Warnings []string  `json:"warnings"`
	}
	if format == FormatSMIME {
		if err := theSecretVault.call("smime.list", withVault(map[string]any{}, vault), &certs); err != nil && svCode(err) != svCodeNotFound {
			out.Why = keysRefused(err)
			return out
		}
	}
	for _, addr := range s.ownAddresses() {
		ak := AddressKeys{Address: addr}
		if format == FormatOpenPGP {
			var key struct {
				Fingerprint string    `json:"fingerprint"`
				Created     time.Time `json:"created"`
				Expires     time.Time `json:"expires"`
				PublicKey   string    `json:"public_key"`
			}
			switch err := theSecretVault.call("pgp.public", withVault(map[string]any{"key": addr}, vault), &key); {
			case err == nil:
				ak.PGP = &OwnPGPKey{Fingerprint: key.Fingerprint, Created: key.Created, Expires: key.Expires, PublicKey: key.PublicKey}
			case svCode(err) == svCodeNotFound, svCode(err) == -32602:
			default:
				out.Why = keysRefused(err)
				return out
			}
		}
		for _, c := range certs {
			for _, e := range c.Emails {
				if strings.EqualFold(e, addr) {
					ak.SMIME = append(ak.SMIME, OwnSMIMECert{Subject: c.Subject, Issuer: c.Issuer,
						NotAfter: c.NotAfter, SHA256: c.SHA256, Warnings: c.Warnings})
					break
				}
			}
		}
		out.Addresses = append(out.Addresses, ak)
	}
	return out
}

// withVault is params naming vault, when it is not secretvault's default.
func withVault(params map[string]any, vault string) map[string]any {
	if vault != "" {
		params["vault"] = vault
	}
	return params
}

// keysRefused says why secretvault did not answer about the keys.
func keysRefused(err error) string {
	switch svCode(err) {
	case svCodeLocked:
		return "secretvault is locked"
	case svCodeDenied, svCodeCanceled:
		return "secretvault did not let comms-mail see which keys you have"
	}
	return err.Error()
}

// MakePGPKey has secretvault make an OpenPGP key for address, under the
// name you send as from it. The key is made and kept in secretvault.
func (s *LocalStore) MakePGPKey(address string) (OwnKeys, error) {
	address = strings.ToLower(strings.TrimSpace(ExtractAddr(address)))
	if address == "" {
		return OwnKeys{}, errors.New("no address to make a key for")
	}
	name := ""
	s.mu.Lock()
	for _, id := range s.identities {
		if strings.EqualFold(ExtractAddr(id.Address), address) && strings.TrimSpace(id.Name) != "" {
			name = strings.TrimSpace(id.Name)
			break
		}
	}
	s.mu.Unlock()
	if s.engineOf(FormatOpenPGP) == EngineOwn {
		return OwnKeys{}, s.makePGPKeyOwn(address, name)
	}
	if err := s.secretVaultOpen(FormatOpenPGP); err != nil {
		return OwnKeys{}, err
	}
	p := withVault(map[string]any{"emails": []string{address}}, s.svVaultOf(FormatOpenPGP))
	if name != "" {
		p["name"] = name
	}
	if err := theSecretVault.call("pgp.generate", p, nil); err != nil {
		return OwnKeys{}, keyWorkFailed("make the key", err)
	}
	Logf("secretvault: made an OpenPGP key for %s", address)
	return s.OwnKeys(), nil
}

// ImportSMIME hands a .p12 / .pfx file and its password to secretvault,
// which keeps the private key and certificate. Both are wiped here.
func (s *LocalStore) ImportSMIME(pkcs12, password []byte) (OwnKeys, error) {
	defer clear(pkcs12)
	defer clear(password)
	if len(pkcs12) == 0 {
		return OwnKeys{}, errors.New("the file is empty")
	}
	if s.engineOf(FormatSMIME) == EngineOwn {
		_, err := s.importSMIMEOwnOrCerts(slices.Clone(pkcs12), slices.Clone(password))
		return OwnKeys{}, err
	}
	if err := s.secretVaultOpen(FormatSMIME); err != nil {
		return OwnKeys{}, err
	}
	var key struct {
		Emails []string `json:"emails"`
	}
	if err := theSecretVault.call("smime.import", withVault(map[string]any{"pkcs12": pkcs12, "password": password}, s.svVaultOf(FormatSMIME)), &key); err != nil {
		switch svCode(err) {
		case -32004:
			return OwnKeys{}, errors.New("that password does not open the file")
		case -32602:
			var e *svError
			errors.As(err, &e)
			return OwnKeys{}, fmt.Errorf("secretvault cannot use the file: %s", e.Message)
		}
		return OwnKeys{}, keyWorkFailed("bring the certificate in", err)
	}
	Logf("secretvault: brought in an S/MIME certificate for %s", strings.Join(key.Emails, ", "))
	return s.OwnKeys(), nil
}

// secretVaultOpen is nil when secretvault runs and the vault for format's
// keys is unlocked: the person asked for this, so a locked one is said,
// not waited for.
func (s *LocalStore) secretVaultOpen(format string) error {
	if err := theSecretVault.available(); err != nil {
		return errors.New("secretvault, which keeps your keys here, is not running")
	}
	if err := svVaultReady(s.svVaultOf(format)); err != nil {
		if errors.Is(err, ErrLocked) {
			return errors.New("secretvault is locked: unlock it first")
		}
		return err
	}
	return nil
}

func keyWorkFailed(what string, err error) error {
	switch svCode(err) {
	case svCodeLocked:
		return errors.New("secretvault is locked: unlock it first")
	case svCodeDenied, svCodeCanceled:
		return fmt.Errorf("secretvault did not let comms-mail %s", what)
	}
	return fmt.Errorf("secretvault could not %s: %w", what, err)
}
