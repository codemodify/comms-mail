package cms

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"time"
)

// SignOptions are how Sign signs.
type SignOptions struct {
	Detached    bool                // no encapsulated content (multipart/signed)
	Chain       []*x509.Certificate // intermediates to include after the signer's certificate
	SigningTime time.Time           // zero: now
	Digest      crypto.Hash         // 0: SHA-256
}

// ecdsaSignatureOIDs are the ECDSA signature algorithms by digest.
var ecdsaSignatureOIDs = map[crypto.Hash]asn1.ObjectIdentifier{
	crypto.SHA224: oidECDSAWithSHA224,
	crypto.SHA256: oidECDSAWithSHA256,
	crypto.SHA384: oidECDSAWithSHA384,
	crypto.SHA512: oidECDSAWithSHA512,
}

// Sign makes a ContentInfo(SignedData) in DER: one signer, named by
// issuer and serial number, signing the attributes contentType (id-data),
// signingTime and messageDigest; the certificates are the signer's and
// then opts.Chain. An RSA key signs with PKCS #1 v1.5 under the identifier
// rsaEncryption, as OpenSSL does; an ECDSA key with ecdsa-with-SHA*,
// matching the digest. SHA-1 is refused: it is too weak to sign with.
func Sign(content []byte, cert *x509.Certificate, key crypto.Signer, opts SignOptions) ([]byte, error) {
	if cert == nil || key == nil {
		return nil, errors.New("cms: signing needs a certificate and its key")
	}
	if pub, ok := key.Public().(interface{ Equal(crypto.PublicKey) bool }); !ok || !pub.Equal(cert.PublicKey) {
		return nil, errors.New("cms: the key is not the certificate's")
	}
	h := opts.Digest
	if h == 0 {
		h = crypto.SHA256
	}
	digestAlg, ok := digestOID(h)
	if !ok || h == crypto.SHA1 {
		return nil, fmt.Errorf("cms: cannot sign with the digest %v", h)
	}
	var sigAlg []byte
	switch cert.PublicKey.(type) {
	case *rsa.PublicKey:
		sigAlg = algID(oidRSAEncryption, asn1NULL)
	case *ecdsa.PublicKey:
		sigAlg = algID(ecdsaSignatureOIDs[h])
	default:
		return nil, fmt.Errorf("cms: cannot sign with a %T key", cert.PublicKey)
	}

	when := opts.SigningTime
	if when.IsZero() {
		when = time.Now()
	}
	signingTime, err := asn1.Marshal(when.UTC().Truncate(time.Second))
	if err != nil {
		return nil, fmt.Errorf("cms: signing time: %w", err)
	}
	attrs := setOf(
		tlv(0x30, oidDER(oidAttrContentType), tlv(0x31, oidDER(oidData))),
		tlv(0x30, oidDER(oidAttrSigningTime), tlv(0x31, signingTime)),
		tlv(0x30, oidDER(oidAttrMessageDigest), tlv(0x31, tlv(0x04, sum(h, content)))),
	)
	// The signature is over the attributes as a SET; the message carries
	// them under [0] IMPLICIT.
	signature, err := key.Sign(rand.Reader, sum(h, attrs), h)
	if err != nil {
		return nil, fmt.Errorf("cms: signing: %w", err)
	}
	signedAttrs := append([]byte{0xa0}, attrs[1:]...)

	sid, err := issuerAndSerialDER(cert)
	if err != nil {
		return nil, err
	}
	signerInfo := tlv(0x30, smallInt(1), sid, algID(digestAlg), signedAttrs, sigAlg, tlv(0x04, signature))

	certs := [][]byte{cert.Raw}
	for _, c := range opts.Chain {
		if c == nil {
			return nil, errors.New("cms: a certificate of the chain is missing")
		}
		certs = append(certs, c.Raw)
	}
	encap := [][]byte{oidDER(oidData)}
	if !opts.Detached {
		encap = append(encap, tlv(0xa0, tlv(0x04, content)))
	}
	signedData := tlv(0x30,
		smallInt(1),
		tlv(0x31, algID(digestAlg)),
		tlv(0x30, encap...),
		tlv(0xa0, certs...),
		tlv(0x31, signerInfo),
	)
	return tlv(0x30, oidDER(oidSignedData), tlv(0xa0, signedData)), nil
}
