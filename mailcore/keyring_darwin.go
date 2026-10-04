//go:build darwin

package mailcore

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// The desktop keyring on macOS: the login Keychain, through Apple's own
// security tool. Each secret is a generic password with service
// "comms-mail" and account <the secret's name>. A value goes to the tool
// on its standard input (security -i), never on a command line where
// other programs could see it, and base64-encoded so any byte survives.
// The Keychain cannot list items by service, so the names are kept in an
// index beside the other data (names only, no secrets).

const kcTool = "/usr/bin/security"
const kcService = "comms-mail"
const kcPrefix = "comms-mail:b64:"

func keyringName() string { return "the macOS Keychain" }

// keyringBackend is the keyring's own name.
func keyringBackend() string { return "macOS Keychain" }

func keyringReady() error {
	if _, err := exec.LookPath(kcTool); err != nil {
		return errors.New("the macOS security tool is missing")
	}
	return nil
}

func keyringGet(name string) (string, bool, error) {
	out, err := exec.Command(kcTool, "find-generic-password", "-s", kcService, "-a", name, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 { // errSecItemNotFound
			return "", false, nil
		}
		return "", false, fmt.Errorf("the Keychain: %w", err)
	}
	v := strings.TrimRight(string(out), "\n")
	if strings.HasPrefix(v, kcPrefix) {
		b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, kcPrefix))
		if err != nil {
			return "", false, err
		}
		v = string(b)
	}
	return v, true, nil
}

// kcQuote quotes an argument for security -i, which splits its input
// the way a shell does.
func kcQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func keyringSet(name, value string) error {
	cmd := exec.Command(kcTool, "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n",
		kcQuote(kcService), kcQuote(name), kcQuote(kcPrefix+base64.StdEncoding.EncodeToString([]byte(value)))))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the Keychain: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return keyringIndexAdd(name)
}

func keyringDelete(name string) error {
	err := exec.Command(kcTool, "delete-generic-password", "-s", kcService, "-a", name).Run()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 44) {
		return fmt.Errorf("the Keychain: %w", err)
	}
	return keyringIndexRemove(name)
}

func keyringNames() ([]string, error) { return keyringIndexNames() }

func keyringUnlock() error { return nil } // the Keychain asks for itself
