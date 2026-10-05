package mailcore

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Settings › Security › Keys, one format at a time: who does the work,
// where comms-mail keeps its own keys, your keys for each address you send
// from, and — with comms-mail's own — other people's keys it knows.

// KeysView is the Keys page for one format.
type KeysView struct {
	Format string   `json:"format"`
	Place  KeyPlace `json:"place"`
	// Available says the keys can be listed now; Locked that the engine's
	// keys are locked; Why why not, otherwise.
	Available bool          `json:"available"`
	Locked    bool          `json:"locked,omitempty"`
	Why       string        `json:"why,omitempty"`
	Addresses []AddressKeys `json:"addresses,omitempty"`
	// Contacts are other people's keys comms-mail's own engine knows.
	Contacts []KeyEntry `json:"contacts,omitempty"`
}

// KeysView is format's Keys page.
func (s *LocalStore) KeysView(format string) KeysView {
	out := KeysView{Format: format, Place: s.keyPlace(format)}
	if out.Place.Engine == EngineSecretVault {
		ok := s.svOwnKeys(format)
		out.Available, out.Locked, out.Why = ok.Available, ok.Locked, ok.Why
		for _, ak := range ok.Addresses {
			a := AddressKeys{Address: ak.Address}
			if format == FormatOpenPGP {
				a.PGP = ak.PGP
			} else {
				a.SMIME = ak.SMIME
			}
			out.Addresses = append(out.Addresses, a)
		}
		return out
	}
	out.Available = true
	k := s.keys()
	addrs := s.ownAddresses()
	for _, e := range k.own(format, "") {
		for _, a := range e.Addresses {
			if !slices.Contains(addrs, a) {
				addrs = append(addrs, a)
			}
		}
	}
	slices.Sort(addrs)
	for _, addr := range addrs {
		a := AddressKeys{Address: addr}
		own := k.own(format, addr)
		if format == FormatOpenPGP {
			if len(own) > 0 {
				e := own[0]
				a.PGP = &OwnPGPKey{Fingerprint: e.ID, Created: e.Created, Expires: e.Expires, PublicKey: e.Public}
			}
		} else {
			for _, e := range own {
				a.SMIME = append(a.SMIME, OwnSMIMECert{Subject: e.Name, Issuer: e.Issuer, NotAfter: e.Expires, SHA256: e.ID, Warnings: certWarnings(e)})
			}
		}
		out.Addresses = append(out.Addresses, a)
	}
	out.Contacts = k.contacts(format, "")
	return out
}

// certWarnings are what is wrong with one of your certificates.
func certWarnings(e KeyEntry) []string {
	var w []string
	if !e.Expires.IsZero() && e.Expires.Before(time.Now()) {
		w = append(w, "It expired on "+e.Expires.Format("2 Jan 2006")+": mail signed with it shows as not to be trusted.")
	} else if !e.Expires.IsZero() && e.Expires.Before(time.Now().AddDate(0, 1, 0)) {
		w = append(w, "It expires on "+e.Expires.Format("2 Jan 2006")+".")
	}
	return w
}

// ownEngine says format's keys are in a place of comms-mail's, or says why
// not.
func (s *LocalStore) ownEngine(format string) error {
	if s.engineOf(format) != EngineOwn {
		return fmt.Errorf("the %s keys are in secretvault, which does that itself", formatName(format))
	}
	return nil
}

// RemoveKey forgets a key of format: one of yours — its private half
// deleted from where it is kept, for good — or someone else's.
func (s *LocalStore) RemoveKey(format, id string, own bool) error {
	if err := s.ownEngine(format); err != nil {
		return err
	}
	if !own {
		return s.keys().removeContact(format, id)
	}
	st := s.keyStore(format)
	if st == nil {
		return errors.New("comms-mail keeps no keys yet")
	}
	if err := st.Ready(); err != nil {
		if errors.Is(err, ErrLocked) {
			return errors.New("where comms-mail keeps its keys is locked: unlock it first")
		}
		return err
	}
	if err := st.Update(nil, keyName(format, strings.ToUpper(id))); err != nil {
		return err
	}
	Logf("keys: removed your %s key %s", formatName(format), id)
	return s.keys().removeOwn(format, id)
}

// KeyBackup is your key of format with id, private half and all, locked
// with passphrase — an OpenPGP key armoured, an S/MIME one as .p12 — and
// the file name to save it as.
func (s *LocalStore) KeyBackup(format, id string, passphrase []byte) (data []byte, name string, err error) {
	if err := s.ownEngine(format); err != nil {
		return nil, "", err
	}
	switch format {
	case FormatOpenPGP:
		data, err = s.ExportPGPBackup(id, passphrase)
		return data, "openpgp-key-" + shortID(id) + ".asc", err
	case FormatSMIME:
		data, err = s.exportSMIMEBackup(id, passphrase)
		return data, "smime-" + shortID(id) + ".p12", err
	}
	return nil, "", fmt.Errorf("mail: no key format %q", format)
}

func shortID(id string) string {
	if len(id) > 16 {
		return strings.ToLower(id[len(id)-16:])
	}
	return strings.ToLower(id)
}

// ImportKeys brings in keys of format from data: OpenPGP keys (yours,
// opened with passphrase when protected, or other people's); for S/MIME,
// a .p12 of yours (passphrase its password) or other people's
// certificates.
func (s *LocalStore) ImportKeys(format string, data, passphrase []byte) (int, error) {
	defer clear(passphrase)
	if err := s.ownEngine(format); err != nil {
		return 0, err
	}
	switch format {
	case FormatOpenPGP:
		return s.ImportPGP(data, slices.Clone(passphrase))
	case FormatSMIME:
		return s.importSMIMEOwnOrCerts(data, slices.Clone(passphrase))
	}
	return 0, fmt.Errorf("mail: no key format %q", format)
}
