package cms

import (
	"crypto"
	"crypto/aes"
	"crypto/subtle"
	"encoding/binary"
	"errors"
)

// The key-encryption key of ECDH key agreement (RFC 5753): derived from the
// shared secret with the ANSI X9.63 KDF, then used to wrap the content key
// with the AES Key Wrap of RFC 3394.

// wrapIV is RFC 3394's default initial value, which unwrapping checks.
var wrapIV = [8]byte{0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6}

var errUnwrap = errors.New("cms: the content key does not unwrap: the message was changed, or is for another key")

// aesWrap wraps key, a multiple of 8 bytes and at least 16, under kek.
func aesWrap(kek, key []byte) ([]byte, error) {
	if len(key)%8 != 0 || len(key) < 16 {
		return nil, errors.New("cms: a key to wrap must be a multiple of 8 bytes, at least 16")
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	n := len(key) / 8
	out := make([]byte, 8+len(key))
	copy(out[8:], key)
	a := wrapIV
	var b [16]byte
	for j := range 6 {
		for i := 1; i <= n; i++ {
			copy(b[:8], a[:])
			copy(b[8:], out[8*i:8*i+8])
			block.Encrypt(b[:], b[:])
			binary.BigEndian.PutUint64(a[:], binary.BigEndian.Uint64(b[:8])^uint64(n*j+i))
			copy(out[8*i:], b[8:])
		}
	}
	copy(out, a[:])
	return out, nil
}

// aesUnwrap unwraps what aesWrap wrapped, checking it is whole.
func aesUnwrap(kek, wrapped []byte) ([]byte, error) {
	if len(wrapped)%8 != 0 || len(wrapped) < 24 {
		return nil, errUnwrap
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	n := len(wrapped)/8 - 1
	r := make([]byte, len(wrapped))
	copy(r, wrapped)
	var a [8]byte
	copy(a[:], r[:8])
	var b [16]byte
	for j := 5; j >= 0; j-- {
		for i := n; i >= 1; i-- {
			binary.BigEndian.PutUint64(b[:8], binary.BigEndian.Uint64(a[:])^uint64(n*j+i))
			copy(b[8:], r[8*i:8*i+8])
			block.Decrypt(b[:], b[:])
			copy(a[:], b[:8])
			copy(r[8*i:], b[8:])
		}
	}
	if subtle.ConstantTimeCompare(a[:], wrapIV[:]) != 1 {
		return nil, errUnwrap
	}
	return r[8:], nil
}

// x963KDF derives n bytes from the shared secret z: the digests of z, a
// 32-bit counter from 1 and sharedInfo, one after another (ANSI X9.63,
// SEC 1 3.6.1).
func x963KDF(h crypto.Hash, z, sharedInfo []byte, n int) []byte {
	var out []byte
	var counter [4]byte
	for i := uint32(1); len(out) < n; i++ {
		binary.BigEndian.PutUint32(counter[:], i)
		d := h.New()
		d.Write(z)
		d.Write(counter[:])
		d.Write(sharedInfo)
		out = d.Sum(out)
	}
	return out[:n]
}

// sharedInfo is the ECC-CMS-SharedInfo the KDF derives from (RFC 5753
// 7.2): the key wrap algorithm as the message gives it, the sender's
// keying material if any, and the key-encryption key's size in bits.
func sharedInfo(wrapAlg, ukm []byte, kekBits int) []byte {
	parts := [][]byte{wrapAlg}
	if ukm != nil {
		parts = append(parts, tlv(0xa0, tlv(0x04, ukm)))
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(kekBits))
	parts = append(parts, tlv(0xa2, tlv(0x04, size[:])))
	return tlv(0x30, parts...)
}
