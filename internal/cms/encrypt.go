package cms

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
)

// Encrypt makes a ContentInfo(EnvelopedData) in DER: content encrypted
// with AES-256-CBC under a random key, and that key for each recipient.
// An RSA recipient gets it by key transport (PKCS #1 v1.5, rsaEncryption);
// an ECDSA recipient on P-256, P-384 or P-521 by ephemeral-static ECDH
// (RFC 5753: the X9.63 KDF over SHA-256, -384 or -512 to match the curve,
// wrapped with id-aes256-wrap). Recipients are named by issuer and serial
// number. A recipient with any other key is an error.
func Encrypt(content []byte, recipients []*x509.Certificate) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, errors.New("cms: encrypting needs a recipient")
	}
	key := make([]byte, 32)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	version := 0 // 2 with key agreement, whose RecipientInfo is version 3
	var infos [][]byte
	for _, r := range recipients {
		if r == nil {
			return nil, errors.New("cms: a recipient's certificate is missing")
		}
		rid, err := issuerAndSerialDER(r)
		if err != nil {
			return nil, err
		}
		switch pub := r.PublicKey.(type) {
		case *rsa.PublicKey:
			encryptedKey, err := rsa.EncryptPKCS1v15(rand.Reader, pub, key)
			if err != nil {
				return nil, fmt.Errorf("cms: encrypting to %s: %w", certName(r), err)
			}
			infos = append(infos, tlv(0x30, smallInt(0), rid, algID(oidRSAEncryption, asn1NULL), tlv(0x04, encryptedKey)))
		case *ecdsa.PublicKey:
			info, err := keyAgreeInfo(pub, rid, key)
			if err != nil {
				return nil, fmt.Errorf("cms: encrypting to %s: %w", certName(r), err)
			}
			infos = append(infos, info)
			version = 2
		default:
			return nil, fmt.Errorf("cms: cannot encrypt to %s: its key is a %T", certName(r), pub)
		}
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	n := aes.BlockSize - len(content)%aes.BlockSize // PKCS #7 padding: n bytes of n
	padded := make([]byte, len(content)+n)
	copy(padded, content)
	for i := len(content); i < len(padded); i++ {
		padded[i] = byte(n)
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(padded, padded)

	encryptedContentInfo := tlv(0x30,
		oidDER(oidData),
		algID(oidAES256CBC, tlv(0x04, iv)),
		tlv(0x80, padded), // [0] IMPLICIT OCTET STRING
	)
	envelopedData := tlv(0x30, smallInt(version), setOf(infos...), encryptedContentInfo)
	return tlv(0x30, oidDER(oidEnvelopedData), tlv(0xa0, envelopedData)), nil
}

// keyAgreeInfo is a KeyAgreeRecipientInfo ([1] IMPLICIT) that gives key to
// pub, named by rid, through a fresh ephemeral key: the originatorKey.
func keyAgreeInfo(pub *ecdsa.PublicKey, rid, key []byte) ([]byte, error) {
	var scheme asn1.ObjectIdentifier
	var kdf crypto.Hash
	switch pub.Curve {
	case elliptic.P256():
		scheme, kdf = oidStdDHSHA256KDF, crypto.SHA256
	case elliptic.P384():
		scheme, kdf = oidStdDHSHA384KDF, crypto.SHA384
	case elliptic.P521():
		scheme, kdf = oidStdDHSHA512KDF, crypto.SHA512
	default:
		return nil, fmt.Errorf("the curve %s is not one this encrypts to", pub.Curve.Params().Name)
	}
	them, err := pub.ECDH()
	if err != nil {
		return nil, err
	}
	ephemeral, err := them.Curve().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	z, err := ephemeral.ECDH(them)
	if err != nil {
		return nil, err
	}
	wrapAlg := algID(oidAES256Wrap)
	kek := x963KDF(kdf, z, sharedInfo(wrapAlg, nil, 256), 32)
	wrapped, err := aesWrap(kek, key)
	if err != nil {
		return nil, err
	}
	originator := tlv(0xa0, // [0] EXPLICIT OriginatorIdentifierOrKey
		tlv(0xa1, // originatorKey [1] IMPLICIT OriginatorPublicKey
			algID(oidECPublicKey),
			tlv(0x03, append([]byte{0}, ephemeral.PublicKey().Bytes()...)),
		),
	)
	recipientEncryptedKeys := tlv(0x30, tlv(0x30, rid, tlv(0x04, wrapped)))
	return tlv(0xa1, smallInt(3), originator, algID(scheme, wrapAlg), recipientEncryptedKeys), nil
}
