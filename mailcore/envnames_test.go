package mailcore

import "testing"

// A mail.json written before the variables took the COMMS_MAIL_ prefix
// names UITK_MAIL_PASS, the default then: it reads COMMS_MAIL_PASS, and
// saving drops the name as it drops the default's.
func TestOldDefaultPassEnvIsTheDefault(t *testing.T) {
	t.Setenv(EnvPass, "from-new")
	t.Setenv(legacyEnvPass, "from-old")
	s := ServerConfig{PassEnv: legacyEnvPass}
	if got := s.Password(); got != "from-new" {
		t.Fatalf("Password() = %q, want the %s value", got, EnvPass)
	}
	sanitizeServerSecret(&s)
	if s.PassEnv != EnvPass {
		t.Fatalf("passEnv after sanitizing = %q, want %q", s.PassEnv, EnvPass)
	}
	withPass := ServerConfig{Pass: "inline", PassEnv: legacyEnvPass}
	sanitizeServerSecret(&withPass)
	if withPass.PassEnv != "" {
		t.Fatalf("passEnv beside an inline password = %q, want none", withPass.PassEnv)
	}
}
