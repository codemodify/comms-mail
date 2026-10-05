package cms

import (
	"bytes"
	"crypto"
	"testing"
)

// AES Key Wrap gives RFC 3394's own test vectors (4.1, 4.4, 4.6), unwraps
// them again, and refuses a wrapped key that was changed.
func TestKeyWrapMatchesRFC3394(t *testing.T) {
	for _, v := range []struct{ kek, key, wrapped string }{
		{"000102030405060708090A0B0C0D0E0F",
			"00112233445566778899AABBCCDDEEFF",
			"1FA68B0A8112B447 AEF34BD8FB5A7B82 9D3E862371D2CFE5"},
		{"000102030405060708090A0B0C0D0E0F1011121314151617",
			"00112233445566778899AABBCCDDEEFF0001020304050607",
			"031D33264E15D332 68F24EC260743EDC E1C6C7DDEE725A93 6BA814915C6762D2"},
		{"000102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F",
			"00112233445566778899AABBCCDDEEFF000102030405060708090A0B0C0D0E0F",
			"28C9F404C4B810F4 CBCCB35CFB87F826 3F5786E2D80ED326 CBC7F0E71A99F43B FB988B9B7A02DD21"},
	} {
		kek, key, wrapped := unhex(t, v.kek), unhex(t, v.key), unhex(t, v.wrapped)
		got, err := aesWrap(kek, key)
		if err != nil || !bytes.Equal(got, wrapped) {
			t.Fatalf("wrap: %X, %v", got, err)
		}
		got, err = aesUnwrap(kek, wrapped)
		if err != nil || !bytes.Equal(got, key) {
			t.Fatalf("unwrap: %X, %v", got, err)
		}
		wrapped[len(wrapped)-1] ^= 1
		if _, err := aesUnwrap(kek, wrapped); err == nil {
			t.Fatal("a changed wrapped key unwrapped")
		}
	}
}

// The X9.63 KDF gives what OpenSSL's X963KDF gives, for one digest's worth
// and for more than one (the counter counts).
func TestX963KDFMatchesOpenSSL(t *testing.T) {
	// openssl kdf -keylen 32 -kdfopt digest:SHA256 -kdfopt hexsecret:96c0…4f08 -kdfopt hexinfo:3019…0080 X963KDF
	got := x963KDF(crypto.SHA256,
		unhex(t, "96c05619d56c328ab95fe84b18264b08725b85e33fd34f08"),
		unhex(t, "30190609608648016503040105a20c040a00000080"), 32)
	if want := unhex(t, "474c93de897a18fed18473c1934f1a47cc61da1485c782f372a42d91546c9d07"); !bytes.Equal(got, want) {
		t.Fatalf("SHA-256: %x", got)
	}
	got = x963KDF(crypto.SHA1, unhex(t, "000102030405060708090a0b0c0d0e0f"), unhex(t, "abcdef"), 24)
	if want := unhex(t, "558d483303cef19d333a557c03f3b26bc055eb7bdbbf62ad"); !bytes.Equal(got, want) {
		t.Fatalf("SHA-1: %x", got)
	}
}
