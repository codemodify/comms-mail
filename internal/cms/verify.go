package cms

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Signer is one signature of a message and what it says.
type Signer struct {
	Certificate *x509.Certificate // nil when the signer's certificate is not in the message
	SigningTime time.Time         // from the signed attributes; zero if none
	Digest      crypto.Hash
	Err         error // nil: the signature is good over the content
}

// SignedData is a signed message as Verify read it.
type SignedData struct {
	Content      []byte              // encapsulated content; nil when detached
	Certificates []*x509.Certificate // all certificates the message carries
	Signers      []Signer
}

type signedData struct {
	Version          int
	DigestAlgorithms asn1.RawValue
	EncapContentInfo encapContentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      asn1.RawValue
}

// encapContentInfo's EContent is [0] EXPLICIT: the OCTET STRING is in
// EContent.Bytes.
type encapContentInfo struct {
	EContentType asn1.ObjectIdentifier
	EContent     asn1.RawValue `asn1:"optional,tag:0"`
}

type signerInfo struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    pkix.AlgorithmIdentifier
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
	UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
}

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue
}

var (
	errNoContent       = errors.New("cms: the signature is detached and no content was given to check it against")
	errBadSignature    = errors.New("cms: the signature does not match: the message was changed, or not signed with this certificate")
	errContentChanged  = errors.New("cms: the content is not what was signed: it was changed after signing")
	errNoSignerCert    = errors.New("cms: the signer's certificate is not in the message")
	errKeyAlgorithm    = errors.New("cms: the signature's algorithm is not for the certificate's key")
	errNoMessageDigest = errors.New("cms: the signed attributes have no message digest")
)

// Verify reads a ContentInfo(SignedData), BER or DER, and checks each
// signature over detachedContent, or over the encapsulated content when
// detachedContent is nil. An error is returned only for a malformed
// message: a bad signature, an algorithm not known here or a missing
// certificate is that Signer's Err. Whether a certificate is to be
// trusted is not checked.
//
// With signed attributes, the messageDigest attribute must be the
// content's digest, a contentType attribute must name the encapsulated
// content's type, and the signature is over the attributes' DER encoding
// as a SET OF. Without them the signature is over the content.
func Verify(der []byte, detachedContent []byte) (*SignedData, error) {
	ci, err := readContentInfo(der)
	if err != nil {
		return nil, err
	}
	if !ci.ContentType.Equal(oidSignedData) {
		return nil, fmt.Errorf("cms: the message is not signed data (%v)", ci.ContentType)
	}
	inner, err := ci.content()
	if err != nil {
		return nil, err
	}
	var sd signedData
	if err := unmarshalAll(inner.FullBytes, &sd); err != nil {
		return nil, fmt.Errorf("cms: malformed signed data: %w", err)
	}
	out := &SignedData{}
	eci := sd.EncapContentInfo
	if len(eci.EContent.FullBytes) > 0 {
		econtent, err := explicitInner(eci.EContent)
		if err != nil {
			return nil, err
		}
		// An OCTET STRING, or in old PKCS #7 any type, whose contents
		// are what was signed.
		out.Content = econtent.Bytes
		if out.Content == nil {
			out.Content = []byte{}
		}
	}
	var unreadable error
	out.Certificates, unreadable, err = readCertificates(sd.Certificates)
	if err != nil {
		return nil, err
	}
	if sd.SignerInfos.Class != asn1.ClassUniversal || sd.SignerInfos.Tag != asn1.TagSet {
		return nil, errors.New("cms: malformed signed data: no set of signers")
	}
	infos, err := elements(sd.SignerInfos.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cms: malformed signed data: %w", err)
	}
	content := detachedContent
	if content == nil {
		content = out.Content
	}
	for _, e := range infos {
		var si signerInfo
		if err := unmarshalAll(e.FullBytes, &si); err != nil {
			return nil, fmt.Errorf("cms: malformed signer: %w", err)
		}
		s := Signer{Certificate: findSigner(si.SID, out.Certificates)}
		s.Err = checkSigner(&s, si, eci.EContentType, content, unreadable)
		out.Signers = append(out.Signers, s)
	}
	return out, nil
}

// readCertificates reads a CertificateSet. A certificate x509 cannot
// read is left out and the first such error returned as unreadable; the
// other kinds of certificate (attribute, other) are left out.
func readCertificates(set asn1.RawValue) (certs []*x509.Certificate, unreadable, err error) {
	if len(set.FullBytes) == 0 {
		return nil, nil, nil
	}
	elems, err := elements(set.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("cms: malformed certificates: %w", err)
	}
	for _, e := range elems {
		if e.Class != asn1.ClassUniversal || e.Tag != asn1.TagSequence {
			continue
		}
		c, err := x509.ParseCertificate(e.FullBytes)
		if err != nil {
			if unreadable == nil {
				unreadable = err
			}
			continue
		}
		certs = append(certs, c)
	}
	return certs, unreadable, nil
}

// findSigner is the certificate a SignerIdentifier names: by issuer and
// serial number, or by subject key identifier ([0]).
func findSigner(sid asn1.RawValue, certs []*x509.Certificate) *x509.Certificate {
	for _, c := range certs {
		switch {
		case sid.Class == asn1.ClassUniversal && sid.Tag == asn1.TagSequence:
			if namedByIssuerAndSerial(sid.FullBytes, c) {
				return c
			}
		case sid.Class == asn1.ClassContextSpecific && sid.Tag == 0 && !sid.IsCompound:
			if namedByKeyID(sid.Bytes, c) {
				return c
			}
		}
	}
	return nil
}

// checkSigner checks one SignerInfo, filling in s as it reads it.
func checkSigner(s *Signer, si signerInfo, eContentType asn1.ObjectIdentifier, content []byte, unreadable error) error {
	h, digestKnown := digestByOID(si.DigestAlgorithm.Algorithm)
	if digestKnown {
		s.Digest = h
	}
	hasAttrs := len(si.SignedAttrs.FullBytes) > 0
	var attrs []attribute
	if hasAttrs {
		var err error
		if attrs, err = readAttributes(si.SignedAttrs.Bytes); err != nil {
			return err
		}
		for _, a := range attrs {
			if a.Type.Equal(oidAttrSigningTime) {
				s.SigningTime = signingTime(a)
			}
		}
	}
	if s.Certificate == nil {
		if unreadable != nil {
			return fmt.Errorf("%w (one could not be read: %v)", errNoSignerCert, unreadable)
		}
		return errNoSignerCert
	}
	if !digestKnown {
		return fmt.Errorf("cms: unknown digest algorithm %v", si.DigestAlgorithm.Algorithm)
	}
	if content == nil {
		return errNoContent
	}
	if !hasAttrs {
		return checkSignature(s.Certificate.PublicKey, si.SignatureAlgorithm, h, content, si.Signature)
	}

	var digest []byte
	for _, a := range attrs {
		switch {
		case a.Type.Equal(oidAttrMessageDigest):
			v, err := singleValue(a)
			if err != nil {
				return err
			}
			if v.Class != asn1.ClassUniversal || v.Tag != asn1.TagOctetString || v.IsCompound {
				return errors.New("cms: the message digest attribute is not an OCTET STRING")
			}
			digest = v.Bytes
		case a.Type.Equal(oidAttrContentType):
			v, err := singleValue(a)
			if err != nil {
				return err
			}
			var ct asn1.ObjectIdentifier
			if unmarshalAll(v.FullBytes, &ct) != nil || !ct.Equal(eContentType) {
				return errors.New("cms: the signed content type is not the content's type")
			}
		}
	}
	if digest == nil {
		return errNoMessageDigest
	}
	if !bytes.Equal(digest, sum(h, content)) {
		return errContentChanged
	}
	signed := tlv(0x31, si.SignedAttrs.Bytes)
	err := checkSignature(s.Certificate.PublicKey, si.SignatureAlgorithm, h, signed, si.Signature)
	if errors.Is(err, errBadSignature) {
		// RFC 5652 has the signature over the attributes' DER, which
		// puts them in order; a signer that wrote them out of order may
		// have signed either.
		if sorted, changed := inDEROrder(si.SignedAttrs.Bytes); changed &&
			checkSignature(s.Certificate.PublicKey, si.SignatureAlgorithm, h, sorted, si.Signature) == nil {
			return nil
		}
	}
	return err
}

// readAttributes reads the contents of a SET OF Attribute. An attribute
// RFC 5652 allows once (contentType, messageDigest, signingTime) given
// twice is an error: which one would count is not for us to guess.
func readAttributes(b []byte) ([]attribute, error) {
	elems, err := elements(b)
	if err != nil {
		return nil, fmt.Errorf("cms: malformed signed attributes: %w", err)
	}
	var attrs []attribute
	for _, e := range elems {
		var a attribute
		if err := unmarshalAll(e.FullBytes, &a); err != nil {
			return nil, fmt.Errorf("cms: malformed signed attribute: %w", err)
		}
		if a.Values.Class != asn1.ClassUniversal || a.Values.Tag != asn1.TagSet {
			return nil, fmt.Errorf("cms: malformed signed attribute %v: its values are not a SET", a.Type)
		}
		for _, once := range []asn1.ObjectIdentifier{oidAttrContentType, oidAttrMessageDigest, oidAttrSigningTime} {
			if a.Type.Equal(once) && slices.ContainsFunc(attrs, func(b attribute) bool { return b.Type.Equal(once) }) {
				return nil, fmt.Errorf("cms: the signed attribute %v is there twice", once)
			}
		}
		attrs = append(attrs, a)
	}
	return attrs, nil
}

// singleValue is the value of an attribute that has exactly one.
func singleValue(a attribute) (asn1.RawValue, error) {
	vs, err := elements(a.Values.Bytes)
	if err != nil || len(vs) != 1 {
		return asn1.RawValue{}, fmt.Errorf("cms: the signed attribute %v does not have one value", a.Type)
	}
	return vs[0], nil
}

// signingTime is a signingTime attribute's time, zero when it cannot be
// read; it is the signer's claim, not something to refuse a message over.
func signingTime(a attribute) time.Time {
	v, err := singleValue(a)
	if err != nil {
		return time.Time{}
	}
	var t time.Time
	if unmarshalAll(v.FullBytes, &t) != nil {
		return time.Time{}
	}
	return t
}

// inDEROrder is the SET OF whose contents are b with its elements in DER
// order, and whether that differs from b.
func inDEROrder(b []byte) ([]byte, bool) {
	elems, err := elements(b)
	if err != nil {
		return nil, false
	}
	raw := make([][]byte, len(elems))
	for i, e := range elems {
		raw[i] = e.FullBytes
	}
	sorted := setOf(raw...)
	if bytes.Equal(sorted, tlv(0x31, raw...)) {
		return nil, false
	}
	return sorted, true
}

// signatureAlgorithms are the RSA PKCS #1 v1.5 and ECDSA signature
// algorithms, with the digest each names (0: the signer's digest
// algorithm, for the identifiers that name only the key).
var signatureAlgorithms = []struct {
	oid   asn1.ObjectIdentifier
	ecdsa bool
	hash  crypto.Hash
}{
	{oidRSAEncryption, false, 0},
	{oidSHA1WithRSA, false, crypto.SHA1},
	{oidSHA224WithRSA, false, crypto.SHA224},
	{oidSHA256WithRSA, false, crypto.SHA256},
	{oidSHA384WithRSA, false, crypto.SHA384},
	{oidSHA512WithRSA, false, crypto.SHA512},
	{oidECPublicKey, true, 0},
	{oidECDSAWithSHA1, true, crypto.SHA1},
	{oidECDSAWithSHA224, true, crypto.SHA224},
	{oidECDSAWithSHA256, true, crypto.SHA256},
	{oidECDSAWithSHA384, true, crypto.SHA384},
	{oidECDSAWithSHA512, true, crypto.SHA512},
}

// checkSignature checks sig over signed with pub, under alg; digest is the
// signer's digest algorithm.
func checkSignature(pub crypto.PublicKey, alg pkix.AlgorithmIdentifier, digest crypto.Hash, signed, sig []byte) error {
	switch {
	case alg.Algorithm.Equal(oidEd25519):
		k, ok := pub.(ed25519.PublicKey)
		if !ok {
			return errKeyAlgorithm
		}
		if !ed25519.Verify(k, signed, sig) {
			return errBadSignature
		}
		return nil
	case alg.Algorithm.Equal(oidRSAPSS):
		k, ok := pub.(*rsa.PublicKey)
		if !ok {
			return errKeyAlgorithm
		}
		h, salt, err := pssParameters(alg.Parameters)
		if err != nil {
			return err
		}
		if rsa.VerifyPSS(k, h, sum(h, signed), sig, &rsa.PSSOptions{SaltLength: salt, Hash: h}) != nil {
			return errBadSignature
		}
		return nil
	}
	for _, a := range signatureAlgorithms {
		if !a.oid.Equal(alg.Algorithm) {
			continue
		}
		h := a.hash
		if h == 0 {
			h = digest
		}
		if a.ecdsa {
			k, ok := pub.(*ecdsa.PublicKey)
			if !ok {
				return errKeyAlgorithm
			}
			if !ecdsa.VerifyASN1(k, sum(h, signed), sig) {
				return errBadSignature
			}
			return nil
		}
		k, ok := pub.(*rsa.PublicKey)
		if !ok {
			return errKeyAlgorithm
		}
		if err := rsa.VerifyPKCS1v15(k, h, sum(h, signed), sig); err != nil {
			if errors.Is(err, rsa.ErrVerification) {
				return errBadSignature
			}
			return fmt.Errorf("cms: %w", err) // a key too small, for one
		}
		return nil
	}
	return fmt.Errorf("cms: unknown signature algorithm %v", alg.Algorithm)
}

// pssParameters reads RSASSA-PSS-params (RFC 4055): the digest and the
// salt length. Go's PSS masks with MGF1 over the same digest, which is
// what everyone uses.
func pssParameters(params asn1.RawValue) (crypto.Hash, int, error) {
	var p struct {
		Hash         pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:0"`
		MGF          pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:1"`
		SaltLength   int                      `asn1:"explicit,optional,default:20,tag:2"`
		TrailerField int                      `asn1:"explicit,optional,default:1,tag:3"`
	}
	p.SaltLength, p.TrailerField = 20, 1
	if len(params.FullBytes) > 0 && !bytes.Equal(params.FullBytes, asn1NULL) {
		if err := unmarshalAll(params.FullBytes, &p); err != nil {
			return 0, 0, fmt.Errorf("cms: malformed RSASSA-PSS parameters: %w", err)
		}
	}
	h, err := digestOfAlgorithm(p.Hash)
	if err != nil {
		return 0, 0, err
	}
	mgf, err := mgf1Digest(p.MGF)
	if err != nil {
		return 0, 0, err
	}
	if mgf != h {
		return 0, 0, errors.New("cms: RSASSA-PSS with a mask digest other than the message digest")
	}
	if p.TrailerField != 1 || p.SaltLength < 0 {
		return 0, 0, errors.New("cms: malformed RSASSA-PSS parameters")
	}
	return h, p.SaltLength, nil
}
