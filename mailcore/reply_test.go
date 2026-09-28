package mailcore

import "testing"

func TestReplyAllRecipients(t *testing.T) {
	m := Message{
		From: `"Doe, Jane" <jane@example.com>`,
		To:   `ada@example.com, Bob <bob@example.com>, "Doe, Jane" <JANE@example.com>`,
		Cc:   `Carol <carol@example.com>, Ada L. <ada.lovelace@example.com>, bob@example.com`,
	}
	to, cc := ReplyAllRecipients(m, []string{"Ada <ada@example.com>", "ada.lovelace@example.com"})
	if want := `"Doe, Jane" <jane@example.com>, Bob <bob@example.com>`; to != want {
		t.Errorf("to = %q, want %q", to, want)
	}
	if want := `Carol <carol@example.com>`; cc != want {
		t.Errorf("cc = %q, want %q", cc, want)
	}

	m.ReplyTo = "list@example.com"
	to, _ = ReplyAllRecipients(m, nil)
	if want := `list@example.com, ada@example.com, Bob <bob@example.com>, "Doe, Jane" <JANE@example.com>`; to != want {
		t.Errorf("with Reply-To: to = %q, want %q", to, want)
	}
}
