package mailui

import (
	"os"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
)

// TestMain keeps every test away from the user's own files: config (the
// window's mailui.json, mail.json), data and the daemon socket all go to a
// temporary directory. Without it, tests that switch layout or density
// wrote the user's real mailui.json.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "comms-mailui-test-")
	if err != nil {
		panic(err)
	}
	if err := mailcore.IsolateTestEnv(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
