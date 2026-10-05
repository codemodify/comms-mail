// Package cms reads and writes the Cryptographic Message Syntax (RFC 5652)
// that S/MIME is made of: signed data, enveloped data and authenticated
// enveloped data (RFC 5083). It is built on the standard library alone.
//
// It checks signatures and opens encryption; it does not judge
// certificates. Whether a signer's certificate is trusted, current, or
// the sender's is the caller's to decide.
//
// Mail programs write CMS in BER (Outlook, Exchange, openssl -stream), and
// encoding/asn1 reads only DER, so every message is rewritten as DER
// (ber.go) before it is read.
package cms

import (
	"bytes"
	"crypto"
	"crypto/sha1"
	_ "crypto/sha256" // SHA-224 and SHA-256, for crypto.Hash.New
	_ "crypto/sha512" // SHA-384 and SHA-512, for crypto.Hash.New
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"
)

// ErrNoRecipient is a message that is not encrypted to the certificate it
// was opened with.
var ErrNoRecipient = errors.New("cms: the message is not encrypted to this certificate")

// ContentType names what a ContentInfo holds: "signed-data",
// "enveloped-data", "authEnveloped-data", or "other".
func ContentType(der []byte) (string, error) {
	ci, err := readContentInfo(der)
	if err != nil {
		return "", err
	}
	switch {
	case ci.ContentType.Equal(oidSignedData):
		return "signed-data", nil
	case ci.ContentType.Equal(oidEnvelopedData):
		return "enveloped-data", nil
	case ci.ContentType.Equal(oidAuthEnvelopedData):
		return "authEnveloped-data", nil
	}
	return "other", nil
}

var (
	oidData              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSignedData        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidEnvelopedData     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 3}
	oidAuthEnvelopedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 23}

	oidAttrContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidAttrMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidAttrSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}

	oidSHA1   = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA224 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 4}
	oidSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}

	oidRSAEncryption   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidRSAOAEP         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 7}
	oidMGF1            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}
	oidPSpecified      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 9}
	oidRSAPSS          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	oidSHA1WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	oidSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidSHA224WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 14}
	oidECPublicKey     = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidECDSAWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	oidECDSAWithSHA224 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 1}
	oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidEd25519         = asn1.ObjectIdentifier{1, 3, 101, 112}

	oidAES128CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidAES128GCM  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 6}
	oidAES192GCM  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 26}
	oidAES256GCM  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 46}
	oidAES128Wrap = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 5}
	oidAES192Wrap = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 25}
	oidAES256Wrap = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 45}
	oidDESEDE3CBC = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}

	// The ECDH key agreement schemes of RFC 5753. The prime curves have a
	// cofactor of 1, so cofactor DH is standard DH for them.
	oidStdDHSHA1KDF        = asn1.ObjectIdentifier{1, 3, 133, 16, 840, 63, 0, 2}
	oidStdDHSHA224KDF      = asn1.ObjectIdentifier{1, 3, 132, 1, 11, 0}
	oidStdDHSHA256KDF      = asn1.ObjectIdentifier{1, 3, 132, 1, 11, 1}
	oidStdDHSHA384KDF      = asn1.ObjectIdentifier{1, 3, 132, 1, 11, 2}
	oidStdDHSHA512KDF      = asn1.ObjectIdentifier{1, 3, 132, 1, 11, 3}
	oidCofactorDHSHA1KDF   = asn1.ObjectIdentifier{1, 3, 133, 16, 840, 63, 0, 3}
	oidCofactorDHSHA224KDF = asn1.ObjectIdentifier{1, 3, 132, 1, 14, 0}
	oidCofactorDHSHA256KDF = asn1.ObjectIdentifier{1, 3, 132, 1, 14, 1}
	oidCofactorDHSHA384KDF = asn1.ObjectIdentifier{1, 3, 132, 1, 14, 2}
	oidCofactorDHSHA512KDF = asn1.ObjectIdentifier{1, 3, 132, 1, 14, 3}
)

// digests are the digest algorithms a message may name.
var digests = []struct {
	oid  asn1.ObjectIdentifier
	hash crypto.Hash
}{
	{oidSHA1, crypto.SHA1},
	{oidSHA224, crypto.SHA224},
	{oidSHA256, crypto.SHA256},
	{oidSHA384, crypto.SHA384},
	{oidSHA512, crypto.SHA512},
}

func digestByOID(oid asn1.ObjectIdentifier) (crypto.Hash, bool) {
	for _, d := range digests {
		if d.oid.Equal(oid) {
			return d.hash, true
		}
	}
	return 0, false
}

func digestOID(h crypto.Hash) (asn1.ObjectIdentifier, bool) {
	for _, d := range digests {
		if d.hash == h {
			return d.oid, true
		}
	}
	return nil, false
}

// digestOfAlgorithm is the digest an AlgorithmIdentifier names, where an
// absent one means SHA-1, as RSASSA-PSS and RSAES-OAEP have it.
func digestOfAlgorithm(alg pkix.AlgorithmIdentifier) (crypto.Hash, error) {
	if len(alg.Algorithm) == 0 {
		return crypto.SHA1, nil
	}
	if h, ok := digestByOID(alg.Algorithm); ok {
		return h, nil
	}
	return 0, fmt.Errorf("cms: unknown digest algorithm %v", alg.Algorithm)
}

// mgf1Digest is the digest of an MGF1 mask generation function; absent,
// it is MGF1 with SHA-1.
func mgf1Digest(alg pkix.AlgorithmIdentifier) (crypto.Hash, error) {
	if len(alg.Algorithm) == 0 {
		return crypto.SHA1, nil
	}
	if !alg.Algorithm.Equal(oidMGF1) {
		return 0, fmt.Errorf("cms: unknown mask generation function %v", alg.Algorithm)
	}
	var h pkix.AlgorithmIdentifier
	if err := unmarshalAll(alg.Parameters.FullBytes, &h); err != nil {
		return 0, fmt.Errorf("cms: malformed MGF1 parameters: %w", err)
	}
	return digestOfAlgorithm(h)
}

func sum(h crypto.Hash, b []byte) []byte {
	d := h.New()
	d.Write(b)
	return d.Sum(nil)
}

// contentInfo is the outermost structure of every message. Content is
// [0] EXPLICIT: the element it tags is in Content.Bytes.
type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"optional,tag:0"`
}

// readContentInfo reads a message, BER or DER.
func readContentInfo(ber []byte) (contentInfo, error) {
	b, err := toDER(ber)
	if err != nil {
		return contentInfo{}, err
	}
	var ci contentInfo
	if err := unmarshalAll(b, &ci); err != nil {
		return contentInfo{}, fmt.Errorf("cms: malformed message: %w", err)
	}
	return ci, nil
}

// content is what a ContentInfo holds, which must be there.
func (ci contentInfo) content() (asn1.RawValue, error) {
	if len(ci.Content.FullBytes) == 0 {
		return asn1.RawValue{}, errors.New("cms: malformed message: it has no content")
	}
	return explicitInner(ci.Content)
}

// explicitInner is the element an EXPLICIT tag wraps.
func explicitInner(tagged asn1.RawValue) (asn1.RawValue, error) {
	var inner asn1.RawValue
	if !tagged.IsCompound {
		return inner, errors.New("cms: malformed message: an explicit tag holds nothing")
	}
	if err := unmarshalAll(tagged.Bytes, &inner); err != nil {
		return inner, fmt.Errorf("cms: malformed message: %w", err)
	}
	return inner, nil
}

// unmarshalAll is asn1.Unmarshal of exactly one value: anything after it
// is an error.
func unmarshalAll(b []byte, v any) error {
	rest, err := asn1.Unmarshal(b, v)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return errors.New("trailing data after a value")
	}
	return nil
}

// elements are the encoded elements one after another in b, as the
// contents of a SET OF or SEQUENCE OF are.
func elements(b []byte) ([]asn1.RawValue, error) {
	var out []asn1.RawValue
	for len(b) > 0 {
		var e asn1.RawValue
		rest, err := asn1.Unmarshal(b, &e)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		b = rest
	}
	return out, nil
}

// octets is the value of an OCTET STRING whose tag may have been replaced
// by an implicit one ([0] IMPLICIT encryptedContent), joined from its
// pieces when a streaming encoder wrote it in pieces.
func octets(v asn1.RawValue) ([]byte, error) {
	if !v.IsCompound {
		return v.Bytes, nil
	}
	pieces, err := elements(v.Bytes)
	if err != nil {
		return nil, err
	}
	out := []byte{}
	for _, p := range pieces {
		if p.Class != asn1.ClassUniversal || p.Tag != asn1.TagOctetString || p.IsCompound {
			return nil, errors.New("a piece of an OCTET STRING is not an OCTET STRING")
		}
		out = append(out, p.Bytes...)
	}
	return out, nil
}

// issuerAndSerial names a certificate by its issuer and serial number.
type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

// names reports whether ias names cert. The issuer is compared as it is
// encoded and, failing that, by its attributes, for programs that encode
// a name again in their own way.
func (ias issuerAndSerial) names(cert *x509.Certificate) bool {
	if ias.Serial == nil || cert.SerialNumber == nil || ias.Serial.Cmp(cert.SerialNumber) != 0 {
		return false
	}
	if bytes.Equal(ias.Issuer.FullBytes, cert.RawIssuer) {
		return true
	}
	var a, b pkix.RDNSequence
	if unmarshalAll(ias.Issuer.FullBytes, &a) != nil || unmarshalAll(cert.RawIssuer, &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// namedByIssuerAndSerial reports whether the encoded IssuerAndSerialNumber
// names cert.
func namedByIssuerAndSerial(der []byte, cert *x509.Certificate) bool {
	var ias issuerAndSerial
	return unmarshalAll(der, &ias) == nil && ias.names(cert)
}

// namedByKeyID reports whether a subject key identifier is cert's: the one
// its extension gives, or the SHA-1 of its public key (RFC 5280 4.2.1.2),
// which is what programs use for a certificate without the extension.
func namedByKeyID(id []byte, cert *x509.Certificate) bool {
	if len(id) == 0 {
		return false
	}
	if len(cert.SubjectKeyId) > 0 && bytes.Equal(id, cert.SubjectKeyId) {
		return true
	}
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if unmarshalAll(cert.RawSubjectPublicKeyInfo, &spki) != nil {
		return false
	}
	h := sha1.Sum(spki.PublicKey.Bytes)
	return bytes.Equal(id, h[:])
}

// The rest builds DER.

var asn1NULL = []byte{0x05, 0x00}

// tlv is one DER element of tag whose contents are parts, one after
// another.
func tlv(tag byte, parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, 6+n)
	out = append(out, tag)
	out = appendLength(out, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// appendLength appends a DER length: the shortest form that holds n.
func appendLength(b []byte, n int) []byte {
	if n < 0x80 {
		return append(b, byte(n))
	}
	var tmp [8]byte
	i := len(tmp)
	for ; n > 0; n >>= 8 {
		i--
		tmp[i] = byte(n)
	}
	b = append(b, 0x80|byte(len(tmp)-i))
	return append(b, tmp[i:]...)
}

// setOf is a DER SET OF the encoded elems, which DER has in the order of
// their encodings.
func setOf(elems ...[]byte) []byte {
	sorted := slices.Clone(elems)
	slices.SortFunc(sorted, bytes.Compare)
	return tlv(0x31, sorted...)
}

func smallInt(n int) []byte { return []byte{0x02, 0x01, byte(n)} }

func oidDER(oid asn1.ObjectIdentifier) []byte {
	b, err := asn1.Marshal(oid)
	if err != nil {
		panic(err) // the identifiers are this package's own
	}
	return b
}

// algID is an AlgorithmIdentifier; params, when given, are its
// parameters, already encoded.
func algID(oid asn1.ObjectIdentifier, params ...[]byte) []byte {
	return tlv(0x30, append([][]byte{oidDER(oid)}, params...)...)
}

// issuerAndSerialDER is the IssuerAndSerialNumber that names cert.
func issuerAndSerialDER(cert *x509.Certificate) ([]byte, error) {
	serial, err := asn1.Marshal(cert.SerialNumber)
	if err != nil {
		return nil, err
	}
	return tlv(0x30, cert.RawIssuer, serial), nil
}

// certName is how an error names a certificate.
func certName(cert *x509.Certificate) string {
	if len(cert.EmailAddresses) > 0 {
		return cert.EmailAddresses[0]
	}
	return cert.Subject.String()
}
