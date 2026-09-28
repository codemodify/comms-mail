package mailcore

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"testing"
	"time"
)

// Fixtures written by Python's plistlib (FMT_BINARY), an independent
// writer; the archives have NSKeyedArchiver's shape.
const (
	bplistPlain       = "YnBsaXN0MDDaAQIDBAUGBwgJCgsMDRESFRYXGBlTQmlnVEJsb2JUTGlzdFROYW1lVk5lc3RlZFNPZmZUUG9ydFVSYXRpb1NTU0xUV2hlbhMAAAEAAAAAAEMAAQKjDg8QUWFRYhADawBaAG8A6wAgAEUAeABhAG0AcABsAGXRExRRa1F2CBED4SM/+AAAAAAAAAkzQcX0XKyAAAAIHSEmKzA3O0BGSk9YXGBiZGZ9gIKEhYiRkgAAAAAAAAEBAAAAAAAAABoAAAAAAAAAAAAAAAAAAACb"
	bplistArchStr     = "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGxfEBBpbWFwLmV4YW1wbGUuY29t0QoLVHJvb3SAARIAAYagCBEbJCkyREdNYGNoagAAAAAAAAEBAAAAAAAAAA0AAAAAAAAAAAAAAAAAAABv"
	bplistArchTrue    = "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGwJ0QoLVHJvb3SAARIAAYagCBEbJCkyREdNTlFWWAAAAAAAAAEBAAAAAAAAAA0AAAAAAAAAAAAAAAAAAABd"
	bplistArchNum     = "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGwRA+HRCgtUcm9vdIABEgABhqAIERskKTJER01QU1haAAAAAAAAAQEAAAAAAAAADQAAAAAAAAAAAAAAAAAAAF8="
	bplistArchNumStr  = "YnBsaXN0MDDUAQIDBAUGCQxZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKiBwhVJG51bGxTNTg30QoLVHJvb3SAARIAAYagCBEbJCkyREdNUVRZWwAAAAAAAAEBAAAAAAAAAA0AAAAAAAAAAAAAAAAAAABg"
	bplistArchAliases = "YnBsaXN0MDDUAQIDBAUGODtZJGFyY2hpdmVyWCRvYmplY3RzVCR0b3BYJHZlcnNpb25fEA9OU0tleWVkQXJjaGl2ZXKvEBAHCA4ZGhscHSEpKisyNjY3VSRudWxs0gkKCwxWJGNsYXNzWk5TLm9iamVjdHOADKENgALTCQ8KEBEVV05TLmtleXOAC6MSExSAA4AEgAWjFhcYgAaAB4ANW0Rpc3BsYXlOYW1lXkVtYWlsQWRkcmVzc2VzWUlzUHJpbWFyeVxBZGEgTG92ZWxhY2XSCQoeH4AMoSCACNMJDwoiIyaAC6IkJYAJgA+iJyiACoAOXEVtYWlsQWRkcmVzc18QD2FkYUBleGFtcGxlLmNvbdIsLS4vWCRjbGFzc2VzWiRjbGFzc25hbWWjLzAxXxATTlNNdXRhYmxlRGljdGlvbmFyeVxOU0RpY3Rpb25hcnlYTlNPYmplY3TSLC0zNKM0NTFeTlNNdXRhYmxlQXJyYXlXTlNBcnJheQlZSXNEZWZhdWx00Tk6VHJvb3SAARIAAYagAAgAEQAbACQAKQAyAEQAVwBdAGIAaQB0AHYAeAB6AIEAiQCLAI8AkQCTAJUAmQCbAJ0AnwCrALoAxADRANYA2ADaANwA4wDlAOgA6gDsAO8A8QDzAQABEgEXASABKwEvAUUBUgFbAWABZAFzAXsBfAGGAYkBjgGQAAAAAAAAAgEAAAAAAAAAPAAAAAAAAAAAAAAAAAAAAZU="
)

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeBinaryPlist(t *testing.T) {
	v, err := decodeBinaryPlist(mustB64(t, bplistPlain))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"Name": "Zoë Example", "Port": int64(993), "Big": int64(1 << 40), "SSL": true, "Off": false,
		"Ratio": 1.5, "When": time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC), "Blob": []byte{0, 1, 2},
		"List": []any{"a", "b", int64(3)}, "Nested": map[string]any{"k": "v"},
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got  %#v\nwant %#v", v, want)
	}
	// decodePlist takes either form.
	if v2, err := decodePlist(bytes.NewReader(mustB64(t, bplistPlain))); err != nil || !reflect.DeepEqual(v2, want) {
		t.Fatalf("decodePlist: %#v, %v", v2, err)
	}
}

// Damaged input is an error, never a panic or a hang.
func TestDecodeBinaryPlistRefusesDamage(t *testing.T) {
	good := mustB64(t, bplistArchAliases)
	for i := 0; i < len(good); i++ {
		for _, c := range []byte{0x00, 0xff, 0x0f, 0xdf} {
			b := append([]byte(nil), good...)
			b[i] = c
			_, _ = decodeBinaryPlist(b)
		}
	}
	for n := 0; n < len(good); n += 7 {
		if _, err := decodeBinaryPlist(good[:n]); err == nil && n < len(good)-32 {
			t.Fatalf("a plist cut at %d decoded", n)
		}
	}
}

func TestUnarchiveKeyed(t *testing.T) {
	for name, tc := range map[string]struct {
		b64  string
		want any
	}{
		"string": {bplistArchStr, "imap.example.com"},
		"bool":   {bplistArchTrue, true},
		"number": {bplistArchNum, int64(993)},
		"aliases": {bplistArchAliases, []any{map[string]any{
			"DisplayName": "Ada Lovelace", "IsPrimary": true,
			"EmailAddresses": []any{map[string]any{"EmailAddress": "ada@example.com", "IsDefault": true}},
		}}},
	} {
		v, err := decodeBinaryPlist(mustB64(t, tc.b64))
		if err != nil {
			t.Fatal(name, err)
		}
		got, ok := unarchiveKeyed(v)
		if !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %#v (%v), want %#v", name, got, ok, tc.want)
		}
	}
	if _, ok := unarchiveKeyed(map[string]any{"a": "b"}); ok {
		t.Fatal("a plain dict is not an archive")
	}
}
