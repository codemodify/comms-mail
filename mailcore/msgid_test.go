package mailcore

import (
	"regexp"
	"strings"
	"testing"
)

// A Message-ID made here is random and at the sender's domain: it names
// neither the software nor the sender, nor when it was written.
func TestMessageIDsAreNeutral(t *testing.T) {
	re := regexp.MustCompile(`^<[0-9a-f]{32}@example\.com>$`)
	a, b := newMessageID("Ada <Ada@Example.com>"), newMessageID("ada@example.com")
	if !re.MatchString(a) || !re.MatchString(b) || a == b {
		t.Fatalf("ids %q %q", a, b)
	}
	if got := newMessageID("ada@bad domain"); !strings.HasSuffix(got, "@localhost>") {
		t.Fatalf("a domain that cannot stand in a Message-ID: %q", got)
	}
	raw := string(BuildRFC822(Message{To: "bob@example.org", Subject: "s", Body: "b"},
		Identity{Name: "Ada", Address: "ada@example.com"}, nil))
	mid := regexp.MustCompile(`(?m)^Message-ID: (\S+)\r?$`).FindStringSubmatch(raw)
	if mid == nil || !re.MatchString(mid[1]) {
		t.Fatalf("built message's Message-ID: %v\n%s", mid, raw)
	}
	if strings.Contains(strings.ToLower(raw), "uitoolkit") {
		t.Fatal("the software is named in the message")
	}
}
