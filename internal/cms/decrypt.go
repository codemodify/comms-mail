package cms

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
)

type envelopedData struct {
	Version              int
	OriginatorInfo       asn1.RawValue `asn1:"optional,tag:0"`
	RecipientInfos       asn1.RawValue
	EncryptedContentInfo encryptedContentInfo
	UnprotectedAttrs     asn1.RawValue `asn1:"optional,tag:1"`
}

type authEnvelopedData struct {
	Version              int
	OriginatorInfo       asn1.RawValue `asn1:"optional,tag:0"`
	RecipientInfos       asn1.RawValue
	EncryptedContentInfo encryptedContentInfo
	AuthAttrs            asn1.RawValue `asn1:"optional,tag:1"`
	MAC                  []byte
	UnauthAttrs          asn1.RawValue `asn1:"optional,tag:2"`
}

// encryptedContentInfo's EncryptedContent is [0] IMPLICIT OCTET STRING,
// in pieces when it was streamed.
type encryptedContentInfo struct {
	ContentType      asn1.ObjectIdentifier
	Algorithm        pkix.AlgorithmIdentifier
	EncryptedContent asn1.RawValue `asn1:"optional,tag:0"`
}

type keyTransRecipientInfo struct {
	Version                int
	RID                    asn1.RawValue
	KeyEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedKey           []byte
}

// keyAgreeRecipientInfo's Originator and UKM are [0] and [1] EXPLICIT:
// what they tag is in their Bytes.
type keyAgreeRecipientInfo struct {
	Version                int
	Originator             asn1.RawValue `asn1:"tag:0"`
	UKM                    asn1.RawValue `asn1:"optional,tag:1"`
	KeyEncryptionAlgorithm pkix.AlgorithmIdentifier
	RecipientEncryptedKeys []recipientEncryptedKey
}

type recipientEncryptedKey struct {
	RID          asn1.RawValue
	EncryptedKey []byte
}

var errDoesNotDecrypt = errors.New("cms: the content does not decrypt: the message was changed, or is for another key")

// Decrypt opens a ContentInfo(EnvelopedData) or
// ContentInfo(AuthEnvelopedData), BER or DER, with cert and its key, an
// *rsa.PrivateKey or *ecdsa.PrivateKey. The recipient is found by cert's
// issuer and serial number or subject key identifier; when the message is
// not for cert the error is ErrNoRecipient.
//
// It reads key transport with RSA PKCS #1 v1.5 or RSAES-OAEP; key
// agreement by ephemeral-static ECDH with the X9.63 KDF over SHA-1, -224,
// -256, -384 or -512 and AES key wrap; content in AES-CBC, DES-EDE3-CBC
// or, authenticated, AES-GCM.
func Decrypt(der []byte, cert *x509.Certificate, key crypto.PrivateKey) ([]byte, error) {
	if cert == nil || key == nil {
		return nil, errors.New("cms: decrypting needs a certificate and its key")
	}
	// Checked here because a wrong RSA key is otherwise noticed only by
	// chance: its content key is random, and so is the CBC padding.
	if k, ok := key.(interface{ Public() crypto.PublicKey }); ok {
		if pub, ok := k.Public().(interface{ Equal(crypto.PublicKey) bool }); ok && !pub.Equal(cert.PublicKey) {
			return nil, errors.New("cms: the key is not the certificate's")
		}
	}
	ci, err := readContentInfo(der)
	if err != nil {
		return nil, err
	}
	inner, err := ci.content()
	if err != nil {
		return nil, err
	}
	switch {
	case ci.ContentType.Equal(oidEnvelopedData):
		var ed envelopedData
		if err := unmarshalAll(inner.FullBytes, &ed); err != nil {
			return nil, fmt.Errorf("cms: malformed enveloped data: %w", err)
		}
		alg := ed.EncryptedContentInfo.Algorithm
		keyLen, err := cbcKeyLen(alg.Algorithm)
		if err != nil {
			return nil, err
		}
		ciphertext, err := encryptedContent(ed.EncryptedContentInfo)
		if err != nil {
			return nil, err
		}
		cek, err := contentKey(ed.RecipientInfos, cert, key, keyLen)
		if err != nil {
			return nil, err
		}
		return openCBC(alg, cek, ciphertext)

	case ci.ContentType.Equal(oidAuthEnvelopedData):
		var ad authEnvelopedData
		if err := unmarshalAll(inner.FullBytes, &ad); err != nil {
			return nil, fmt.Errorf("cms: malformed authenticated enveloped data: %w", err)
		}
		alg := ad.EncryptedContentInfo.Algorithm
		keyLen, err := gcmKeyLen(alg.Algorithm)
		if err != nil {
			return nil, err
		}
		ciphertext, err := encryptedContent(ad.EncryptedContentInfo)
		if err != nil {
			return nil, err
		}
		cek, err := contentKey(ad.RecipientInfos, cert, key, keyLen)
		if err != nil {
			return nil, err
		}
		var aad []byte // the authenticated attributes, as a SET OF (RFC 5083 2.2)
		if len(ad.AuthAttrs.FullBytes) > 0 {
			aad = tlv(0x31, ad.AuthAttrs.Bytes)
		}
		return openGCM(alg, cek, ciphertext, ad.MAC, aad)
	}
	return nil, fmt.Errorf("cms: the message is not encrypted (%v)", ci.ContentType)
}

func encryptedContent(eci encryptedContentInfo) ([]byte, error) {
	if len(eci.EncryptedContent.FullBytes) == 0 {
		return nil, errors.New("cms: the encrypted content is not in the message")
	}
	b, err := octets(eci.EncryptedContent)
	if err != nil {
		return nil, fmt.Errorf("cms: malformed encrypted content: %w", err)
	}
	return b, nil
}

// contentKey finds cert among the recipients and recovers the content key
// for it with key; keyLen is the key's length for the content's cipher.
func contentKey(recipientInfos asn1.RawValue, cert *x509.Certificate, key crypto.PrivateKey, keyLen int) ([]byte, error) {
	if recipientInfos.Class != asn1.ClassUniversal || recipientInfos.Tag != asn1.TagSet {
		return nil, errors.New("cms: malformed enveloped data: no set of recipients")
	}
	infos, err := elements(recipientInfos.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cms: malformed recipients: %w", err)
	}
	for _, e := range infos {
		switch {
		case e.Class == asn1.ClassUniversal && e.Tag == asn1.TagSequence:
			var ri keyTransRecipientInfo
			if err := unmarshalAll(e.FullBytes, &ri); err != nil {
				return nil, fmt.Errorf("cms: malformed recipient: %w", err)
			}
			if namesRecipient(ri.RID, cert, false) {
				return keyTransport(ri, key, keyLen)
			}
		case e.Class == asn1.ClassContextSpecific && e.Tag == 1 && e.IsCompound:
			var ri keyAgreeRecipientInfo
			if rest, err := asn1.UnmarshalWithParams(e.FullBytes, &ri, "tag:1"); err != nil || len(rest) > 0 {
				return nil, fmt.Errorf("cms: malformed key agreement recipient: %v", err)
			}
			for _, rek := range ri.RecipientEncryptedKeys {
				if namesRecipient(rek.RID, cert, true) {
					return keyAgree(ri, rek.EncryptedKey, key, keyLen)
				}
			}
		}
		// [2] KEK, [3] password and [4] other recipients have no
		// certificate to be.
	}
	return nil, ErrNoRecipient
}

// namesRecipient reports whether a RecipientIdentifier names cert: by
// issuer and serial number, or by subject key identifier, which key
// transport gives as [0] IMPLICIT OCTET STRING and key agreement as
// [0] IMPLICIT RecipientKeyIdentifier, a SEQUENCE that starts with it.
func namesRecipient(rid asn1.RawValue, cert *x509.Certificate, keyAgree bool) bool {
	switch {
	case rid.Class == asn1.ClassUniversal && rid.Tag == asn1.TagSequence:
		return namedByIssuerAndSerial(rid.FullBytes, cert)
	case rid.Class == asn1.ClassContextSpecific && rid.Tag == 0:
		if !keyAgree {
			return !rid.IsCompound && namedByKeyID(rid.Bytes, cert)
		}
		fields, err := elements(rid.Bytes)
		if err != nil || len(fields) == 0 || fields[0].Tag != asn1.TagOctetString {
			return false
		}
		return namedByKeyID(fields[0].Bytes, cert)
	}
	return false
}

// keyTransport decrypts the content key with an RSA key.
func keyTransport(ri keyTransRecipientInfo, key crypto.PrivateKey, keyLen int) ([]byte, error) {
	priv, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("cms: the message is encrypted to an RSA key, not a %T", key)
	}
	switch alg := ri.KeyEncryptionAlgorithm; {
	case alg.Algorithm.Equal(oidRSAEncryption):
		// A key that does not decrypt is replaced by a random one, in
		// constant time, so the content fails to decrypt instead: no
		// padding oracle (RFC 3218).
		cek := make([]byte, keyLen)
		if _, err := rand.Read(cek); err != nil {
			return nil, err
		}
		if err := rsa.DecryptPKCS1v15SessionKey(rand.Reader, priv, ri.EncryptedKey, cek); err != nil {
			return nil, fmt.Errorf("cms: decrypting the content key: %w", err)
		}
		return cek, nil
	case alg.Algorithm.Equal(oidRSAOAEP):
		opts, err := oaepOptions(alg.Parameters)
		if err != nil {
			return nil, err
		}
		cek, err := priv.Decrypt(rand.Reader, ri.EncryptedKey, opts)
		if err != nil {
			return nil, fmt.Errorf("cms: decrypting the content key: %w", err)
		}
		if len(cek) != keyLen {
			return nil, errors.New("cms: the content key is not the length its cipher needs")
		}
		return cek, nil
	default:
		return nil, fmt.Errorf("cms: unknown key transport algorithm %v", alg.Algorithm)
	}
}

// oaepOptions reads RSAES-OAEP-params (RFC 4055), whose absent fields are
// SHA-1, MGF1 with SHA-1, and an empty label.
func oaepOptions(params asn1.RawValue) (*rsa.OAEPOptions, error) {
	var p struct {
		Hash   pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:0"`
		MGF    pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:1"`
		Source pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:2"`
	}
	if len(params.FullBytes) > 0 && !bytes.Equal(params.FullBytes, asn1NULL) {
		if err := unmarshalAll(params.FullBytes, &p); err != nil {
			return nil, fmt.Errorf("cms: malformed RSAES-OAEP parameters: %w", err)
		}
	}
	h, err := digestOfAlgorithm(p.Hash)
	if err != nil {
		return nil, err
	}
	mgf, err := mgf1Digest(p.MGF)
	if err != nil {
		return nil, err
	}
	opts := &rsa.OAEPOptions{Hash: h, MGFHash: mgf}
	if len(p.Source.Algorithm) > 0 {
		if !p.Source.Algorithm.Equal(oidPSpecified) {
			return nil, fmt.Errorf("cms: unknown OAEP label source %v", p.Source.Algorithm)
		}
		if err := unmarshalAll(p.Source.Parameters.FullBytes, &opts.Label); err != nil {
			return nil, fmt.Errorf("cms: malformed OAEP label: %w", err)
		}
	}
	return opts, nil
}

// kdfSchemes are the ECDH key agreement schemes, by the digest their KDF
// uses.
var kdfSchemes = []struct {
	oid  asn1.ObjectIdentifier
	hash crypto.Hash
}{
	{oidStdDHSHA1KDF, crypto.SHA1},
	{oidStdDHSHA224KDF, crypto.SHA224},
	{oidStdDHSHA256KDF, crypto.SHA256},
	{oidStdDHSHA384KDF, crypto.SHA384},
	{oidStdDHSHA512KDF, crypto.SHA512},
	{oidCofactorDHSHA1KDF, crypto.SHA1},
	{oidCofactorDHSHA224KDF, crypto.SHA224},
	{oidCofactorDHSHA256KDF, crypto.SHA256},
	{oidCofactorDHSHA384KDF, crypto.SHA384},
	{oidCofactorDHSHA512KDF, crypto.SHA512},
}

// keyAgree recovers the content key wrapped for an EC key: ECDH with the
// sender's ephemeral key, the X9.63 KDF, and AES key unwrap.
func keyAgree(ri keyAgreeRecipientInfo, wrapped []byte, key crypto.PrivateKey, keyLen int) ([]byte, error) {
	priv, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("cms: the message is encrypted to an EC key, not a %T", key)
	}
	me, err := priv.ECDH()
	if err != nil {
		return nil, fmt.Errorf("cms: %w", err)
	}
	var kdf crypto.Hash
	for _, s := range kdfSchemes {
		if s.oid.Equal(ri.KeyEncryptionAlgorithm.Algorithm) {
			kdf = s.hash
		}
	}
	if kdf == 0 {
		return nil, fmt.Errorf("cms: unknown key agreement algorithm %v", ri.KeyEncryptionAlgorithm.Algorithm)
	}
	wrapAlgDER := ri.KeyEncryptionAlgorithm.Parameters.FullBytes
	var wrapAlg pkix.AlgorithmIdentifier
	if err := unmarshalAll(wrapAlgDER, &wrapAlg); err != nil {
		return nil, fmt.Errorf("cms: malformed key wrap algorithm: %w", err)
	}
	var kekLen int
	switch {
	case wrapAlg.Algorithm.Equal(oidAES128Wrap):
		kekLen = 16
	case wrapAlg.Algorithm.Equal(oidAES192Wrap):
		kekLen = 24
	case wrapAlg.Algorithm.Equal(oidAES256Wrap):
		kekLen = 32
	default:
		return nil, fmt.Errorf("cms: unknown key wrap algorithm %v", wrapAlg.Algorithm)
	}

	originator, err := explicitInner(ri.Originator)
	if err != nil {
		return nil, err
	}
	if originator.Class != asn1.ClassContextSpecific || originator.Tag != 1 || !originator.IsCompound {
		return nil, errors.New("cms: the sender's key is named by certificate; only an ephemeral key (originatorKey) is supported")
	}
	var opk struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if rest, err := asn1.UnmarshalWithParams(originator.FullBytes, &opk, "tag:1"); err != nil || len(rest) > 0 {
		return nil, fmt.Errorf("cms: malformed originator key: %v", err)
	}
	them, err := me.Curve().NewPublicKey(opk.PublicKey.RightAlign())
	if err != nil {
		return nil, fmt.Errorf("cms: the sender's key is not a point on the recipient's curve: %w", err)
	}
	z, err := me.ECDH(them)
	if err != nil {
		return nil, fmt.Errorf("cms: %w", err)
	}
	var ukm []byte
	if len(ri.UKM.FullBytes) > 0 {
		v, err := explicitInner(ri.UKM)
		if err != nil {
			return nil, err
		}
		ukm = append([]byte{}, v.Bytes...)
	}
	kek := x963KDF(kdf, z, sharedInfo(wrapAlgDER, ukm, kekLen*8), kekLen)
	cek, err := aesUnwrap(kek, wrapped)
	if err != nil {
		return nil, err
	}
	if len(cek) != keyLen {
		return nil, errors.New("cms: the content key is not the length its cipher needs")
	}
	return cek, nil
}

// cbcKeyLen is the key length of a CBC content cipher.
func cbcKeyLen(oid asn1.ObjectIdentifier) (int, error) {
	switch {
	case oid.Equal(oidAES128CBC):
		return 16, nil
	case oid.Equal(oidAES192CBC):
		return 24, nil
	case oid.Equal(oidAES256CBC):
		return 32, nil
	case oid.Equal(oidDESEDE3CBC):
		return 24, nil
	}
	return 0, fmt.Errorf("cms: unknown content encryption algorithm %v", oid)
}

// gcmKeyLen is the key length of an AES-GCM content cipher.
func gcmKeyLen(oid asn1.ObjectIdentifier) (int, error) {
	switch {
	case oid.Equal(oidAES128GCM):
		return 16, nil
	case oid.Equal(oidAES192GCM):
		return 24, nil
	case oid.Equal(oidAES256GCM):
		return 32, nil
	}
	return 0, fmt.Errorf("cms: unknown authenticated content encryption algorithm %v", oid)
}

// openCBC decrypts CBC content, whose parameters are the IV, and takes off
// its PKCS #7 padding.
func openCBC(alg pkix.AlgorithmIdentifier, key, ciphertext []byte) ([]byte, error) {
	var block cipher.Block
	var err error
	if alg.Algorithm.Equal(oidDESEDE3CBC) {
		block, err = des.NewTripleDESCipher(key)
	} else {
		block, err = aes.NewCipher(key)
	}
	if err != nil {
		return nil, fmt.Errorf("cms: %w", err)
	}
	var iv []byte
	if err := unmarshalAll(alg.Parameters.FullBytes, &iv); err != nil || len(iv) != block.BlockSize() {
		return nil, errors.New("cms: malformed content encryption parameters: no IV of the block's size")
	}
	bs := block.BlockSize()
	if len(ciphertext) == 0 || len(ciphertext)%bs != 0 {
		return nil, errDoesNotDecrypt
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	n := int(out[len(out)-1])
	if n == 0 || n > bs {
		return nil, errDoesNotDecrypt
	}
	for _, p := range out[len(out)-n:] {
		if int(p) != n {
			return nil, errDoesNotDecrypt
		}
	}
	return out[:len(out)-n], nil
}

// openGCM decrypts and authenticates AES-GCM content (RFC 5084), whose
// parameters are the nonce and the tag's length; the tag is the mac.
func openGCM(alg pkix.AlgorithmIdentifier, key, ciphertext, mac, aad []byte) ([]byte, error) {
	var p struct {
		Nonce  []byte
		ICVLen int `asn1:"optional,default:12"`
	}
	p.ICVLen = 12
	if err := unmarshalAll(alg.Parameters.FullBytes, &p); err != nil {
		return nil, fmt.Errorf("cms: malformed GCM parameters: %w", err)
	}
	if len(mac) != p.ICVLen {
		return nil, errors.New("cms: the authentication tag is not the length its parameters give")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cms: %w", err)
	}
	var aead cipher.AEAD
	switch {
	case len(p.Nonce) == 12:
		aead, err = cipher.NewGCMWithTagSize(block, p.ICVLen)
	case p.ICVLen == 16 && len(p.Nonce) > 0:
		aead, err = cipher.NewGCMWithNonceSize(block, len(p.Nonce))
	default:
		err = fmt.Errorf("a %d-byte nonce with a %d-byte tag", len(p.Nonce), p.ICVLen)
	}
	if err != nil {
		return nil, fmt.Errorf("cms: unsupported GCM parameters: %w", err)
	}
	sealed := make([]byte, 0, len(ciphertext)+len(mac))
	sealed = append(append(sealed, ciphertext...), mac...)
	out, err := aead.Open(nil, p.Nonce, sealed, aad)
	if err != nil {
		return nil, errDoesNotDecrypt
	}
	return out, nil
}
