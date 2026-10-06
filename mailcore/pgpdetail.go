package mailcore

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// What comms-mail's own engine says of an OpenPGP message besides whether
// it holds: whom it is encrypted to and how, read from its packets before
// anything is opened; when and with what a signature was made, and the
// key that made it — as secretvault's report says them.

// pgpEncryption is an OpenPGP message's encrypted layer as its packets
// say it: a recipient for each key, and a passphrase; the integrity, and
// with AEAD the cipher and its mode.
func pgpEncryption(data []byte) EncryptionCheck {
	ec := EncryptionCheck{Format: FormatOpenPGP}
	in := io.Reader(bytes.NewReader(data))
	if bytes.Contains(data, []byte("-----BEGIN PGP")) {
		block, err := armor.Decode(bytes.NewReader(data))
		if err != nil {
			return ec
		}
		in = block.Body
	}
	pr := packet.NewReader(in)
	for {
		p, err := pr.Next()
		if err != nil {
			return ec
		}
		switch k := p.(type) {
		case *packet.EncryptedKey:
			r := Recipient{Algorithm: pgpAlgoName(k.Algo)}
			switch {
			case len(k.KeyFingerprint) > 0:
				r.Fingerprint = strings.ToUpper(hex.EncodeToString(k.KeyFingerprint))
			case k.KeyId == 0:
				r.KeyID = "hidden" // a recipient the message does not name
			default:
				r.KeyID = fmt.Sprintf("%016X", k.KeyId)
			}
			ec.Recipients = append(ec.Recipients, r)
		case *packet.SymmetricKeyEncrypted:
			ec.Recipients = append(ec.Recipients, Recipient{Passphrase: true})
		case *packet.SymmetricallyEncrypted:
			switch {
			case k.Version == 2:
				ec.Integrity = "aead"
				ec.Cipher = strings.TrimSpace(pgpCipherName(k.Cipher) + " " + pgpModeName(k.Mode))
			case k.IntegrityProtected:
				ec.Integrity = "mdc"
			default:
				ec.Integrity = "none"
			}
			return ec
		case *packet.AEADEncrypted:
			ec.Integrity = "aead"
			return ec
		}
	}
}

func pgpAlgoName(a packet.PublicKeyAlgorithm) string {
	switch a {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSAEncryptOnly, packet.PubKeyAlgoRSASignOnly:
		return "RSA"
	case packet.PubKeyAlgoElGamal:
		return "ElGamal"
	case packet.PubKeyAlgoDSA:
		return "DSA"
	case packet.PubKeyAlgoECDH:
		return "ECDH"
	case packet.PubKeyAlgoECDSA:
		return "ECDSA"
	case packet.PubKeyAlgoEdDSA:
		return "EdDSA"
	case packet.PubKeyAlgoX25519:
		return "X25519"
	case packet.PubKeyAlgoX448:
		return "X448"
	case packet.PubKeyAlgoEd25519:
		return "Ed25519"
	case packet.PubKeyAlgoEd448:
		return "Ed448"
	case packet.PubKeyAlgoMlkem768X25519:
		return "ML-KEM-768 + X25519"
	case packet.PubKeyAlgoMlkem1024X448:
		return "ML-KEM-1024 + X448"
	case packet.PubKeyAlgoMldsa65Ed25519:
		return "ML-DSA-65 + Ed25519"
	case packet.PubKeyAlgoMldsa87Ed448:
		return "ML-DSA-87 + Ed448"
	}
	return fmt.Sprintf("algorithm %d", a)
}

func pgpCipherName(c packet.CipherFunction) string {
	switch c {
	case packet.CipherAES128:
		return "AES-128"
	case packet.CipherAES192:
		return "AES-192"
	case packet.CipherAES256:
		return "AES-256"
	case packet.CipherCAST5:
		return "CAST5"
	case packet.Cipher3DES:
		return "3DES"
	}
	return ""
}

func pgpModeName(m packet.AEADMode) string {
	switch m {
	case packet.AEADModeOCB:
		return "OCB"
	case packet.AEADModeGCM:
		return "GCM"
	case packet.AEADModeEAX:
		return "EAX"
	}
	return ""
}

// pgpSignatureDetail fills in when signature p was made and with what,
// and the key of signer's that made it: its size and its dates.
func pgpSignatureDetail(c *SignatureCheck, p *packet.Signature, signer *openpgp.Entity) {
	if p == nil {
		return
	}
	c.SignedAt = p.CreationTime
	c.Hash = p.Hash.String()
	c.Algorithm = pgpAlgoName(p.PubKeyAlgo)
	if signer == nil {
		return
	}
	key, self := signer.PrimaryKey, (*packet.Signature)(nil)
	if id := signer.PrimaryIdentity(); id != nil {
		self = id.SelfSignature
	}
	if p.IssuerKeyId != nil && key.KeyId != *p.IssuerKeyId {
		for _, sub := range signer.Subkeys {
			if sub.PublicKey != nil && sub.PublicKey.KeyId == *p.IssuerKeyId {
				key, self = sub.PublicKey, sub.Sig
			}
		}
	}
	if bits, err := key.BitLength(); err == nil {
		c.KeyBits = int(bits)
	}
	c.KeyCreated = key.CreationTime
	if self != nil && self.KeyLifetimeSecs != nil && *self.KeyLifetimeSecs > 0 {
		c.KeyExpires = key.CreationTime.Add(time.Duration(*self.KeyLifetimeSecs) * time.Second)
	}
}

// openedPGP marks which of ec's recipients the key with opened.
func openedPGP(ec *EncryptionCheck, with string) {
	ec.OpenedWith = with
	for i := range ec.Recipients {
		ec.Recipients[i].Opened = openedBy(ec.Recipients[i], with)
	}
}
