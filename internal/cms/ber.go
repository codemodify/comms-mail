package cms

import "errors"

// BER, as Outlook, Exchange and openssl -stream write CMS, may leave a
// length open until an end-of-contents marker (indefinite length) and cut
// an OCTET STRING into pieces (a constructed OCTET STRING), so content can
// be written as it is read. encoding/asn1 reads neither. toDER rewrites a
// message with every length given, in its shortest form, and every
// OCTET STRING whole; all else is left as it was. Signed attributes, which
// their signer encoded in DER, come out as they were signed.
//
// An OCTET STRING whose tag is implicit ([0] IMPLICIT encryptedContent)
// cannot be told from any other tagged element without the schema, so
// its pieces are left for the reader to join (octets, in cms.go).
//
// It works in two passes over the bytes, sizing an element before writing
// it, and keeps nothing per element: a message of many small pieces costs
// no more memory than its size.

// maxDepth is how deeply elements may nest. CMS nests a dozen deep, a
// certificate in it a few more.
const maxDepth = 64

var errTruncated = errors.New("cms: malformed message: it ends early")

// toDER rewrites ber, one element, as DER.
func toDER(ber []byte) ([]byte, error) {
	if len(ber) == 0 {
		return nil, errors.New("cms: the message is empty")
	}
	h, err := readHeader(ber)
	if err != nil {
		return nil, err
	}
	size, span, err := derSize(ber, 0) // checks all of it, once
	if err != nil {
		return nil, err
	}
	if span != len(ber) {
		return nil, errors.New("cms: malformed message: data after its end")
	}
	out, _, err := appendDER(make([]byte, 0, derHeaderLen(h, size)+size), ber, 0)
	return out, err
}

// header is what an element's identifier and length octets say.
type header struct {
	id          []byte // the identifier octets
	constructed bool
	indefinite  bool
	length      int // of the contents, when definite
	size        int // of the identifier and length octets
}

// joined reports whether the element is a constructed OCTET STRING, whose
// pieces become one.
func (h header) joined() bool { return len(h.id) == 1 && h.id[0] == 0x24 }

func (h header) octetString() bool { return len(h.id) == 1 && (h.id[0] == 0x04 || h.id[0] == 0x24) }

// derHeaderLen is the length of the identifier and length octets of h's
// element in DER, with contents of size.
func derHeaderLen(h header, size int) int {
	if h.joined() {
		return 1 + lengthLen(size)
	}
	return len(h.id) + lengthLen(size)
}

func readHeader(b []byte) (header, error) {
	idLen, err := identifierLen(b)
	if err != nil {
		return header{}, err
	}
	if b[0] == 0 {
		return header{}, errors.New("cms: malformed message: an end-of-contents marker where none belongs")
	}
	h := header{id: b[:idLen], constructed: b[0]&0x20 != 0}
	length, indefinite, lenLen, err := readLength(b[idLen:])
	if err != nil {
		return header{}, err
	}
	h.size = idLen + lenLen
	if indefinite && !h.constructed {
		return header{}, errors.New("cms: malformed message: a primitive element without a length")
	}
	if !indefinite && length > len(b)-h.size {
		return header{}, errTruncated
	}
	h.length, h.indefinite = length, indefinite
	return h, nil
}

// identifierLen is the length of the identifier octets at the start of b.
func identifierLen(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errTruncated
	}
	if b[0]&0x1f != 0x1f {
		return 1, nil
	}
	for i := 1; i < len(b); i++ { // a tag number of more than 30, 7 bits a byte
		if i > 4 {
			return 0, errors.New("cms: malformed message: a tag number too large")
		}
		if b[i]&0x80 == 0 {
			return i + 1, nil
		}
	}
	return 0, errTruncated
}

// readLength reads the length octets at the start of b.
func readLength(b []byte) (length int, indefinite bool, n int, err error) {
	if len(b) == 0 {
		return 0, false, 0, errTruncated
	}
	c := b[0]
	switch {
	case c < 0x80:
		return int(c), false, 1, nil
	case c == 0x80:
		return 0, true, 1, nil
	case c == 0xff:
		return 0, false, 0, errors.New("cms: malformed message: a reserved length")
	}
	k := int(c & 0x7f)
	if k > 4 { // over 4 GiB: not mail
		return 0, false, 0, errors.New("cms: malformed message: a length too large")
	}
	if len(b) < 1+k {
		return 0, false, 0, errTruncated
	}
	var l uint64
	for _, d := range b[1 : 1+k] {
		l = l<<8 | uint64(d)
	}
	if l > uint64(len(b)) {
		return 0, false, 0, errTruncated
	}
	return int(l), false, 1 + k, nil
}

// eachChild calls fn on the elements inside the constructed element at the
// start of b, whose header is h; fn is given the bytes from the child on
// and says how many the child spans. eachChild says how many the element
// spans.
func eachChild(b []byte, h header, fn func(child []byte) (int, error)) (int, error) {
	p, end := h.size, len(b)
	if !h.indefinite {
		end = h.size + h.length
	}
	for {
		if h.indefinite {
			if end-p < 2 {
				return 0, errTruncated
			}
			if b[p] == 0 && b[p+1] == 0 {
				return p + 2, nil
			}
		} else if p == end {
			return p, nil
		}
		n, err := fn(b[p:end])
		if err != nil {
			return 0, err
		}
		p += n
	}
}

// derSize is the length of the DER contents of the element at the start
// of b, and how many bytes of b the element spans.
func derSize(b []byte, depth int) (size, span int, err error) {
	if depth > maxDepth {
		return 0, 0, errors.New("cms: malformed message: it nests too deeply")
	}
	h, err := readHeader(b)
	if err != nil {
		return 0, 0, err
	}
	if !h.constructed {
		return h.length, h.size + h.length, nil
	}
	span, err = eachChild(b, h, func(child []byte) (int, error) {
		ch, err := readHeader(child)
		if err != nil {
			return 0, err
		}
		if h.joined() && !ch.octetString() {
			return 0, errors.New("cms: malformed message: a piece of an OCTET STRING is not an OCTET STRING")
		}
		n, childSpan, err := derSize(child, depth+1)
		if err != nil {
			return 0, err
		}
		if h.joined() {
			size += n
		} else {
			size += derHeaderLen(ch, n) + n
		}
		return childSpan, nil
	})
	return size, span, err
}

// appendDER appends the DER of the element at the start of b, which
// derSize has checked, and says how many bytes of b it spans.
func appendDER(out, b []byte, depth int) ([]byte, int, error) {
	h, err := readHeader(b)
	if err != nil {
		return nil, 0, err
	}
	if !h.constructed {
		out = append(out, h.id...)
		out = appendLength(out, h.length)
		return append(out, b[h.size:h.size+h.length]...), h.size + h.length, nil
	}
	size, span, err := derSize(b, depth)
	if err != nil {
		return nil, 0, err
	}
	if h.joined() {
		out = appendLength(append(out, 0x04), size)
		out, _, err = appendPieces(out, b)
		return out, span, err
	}
	out = appendLength(append(out, h.id...), size)
	_, err = eachChild(b, h, func(child []byte) (n int, err error) {
		out, n, err = appendDER(out, child, depth+1)
		return n, err
	})
	return out, span, err
}

// appendPieces appends the contents of the OCTET STRING at the start of b,
// its pieces joined, and says how many bytes of b it spans.
func appendPieces(out, b []byte) ([]byte, int, error) {
	h, err := readHeader(b)
	if err != nil {
		return nil, 0, err
	}
	if !h.constructed {
		return append(out, b[h.size:h.size+h.length]...), h.size + h.length, nil
	}
	span, err := eachChild(b, h, func(child []byte) (n int, err error) {
		out, n, err = appendPieces(out, child)
		return n, err
	})
	return out, span, err
}

// lengthLen is the length of appendLength's encoding of n.
func lengthLen(n int) int {
	if n < 0x80 {
		return 1
	}
	k := 1
	for ; n > 0; n >>= 8 {
		k++
	}
	return k
}
