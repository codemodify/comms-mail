package mail

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uitk-mail-test-")
	if err != nil {
		panic(err)
	}
	if err := IsolateTestEnv(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestDisposableMailSocket(t *testing.T) {
	if IsDisposableMailSocket("") {
		t.Fatal("empty")
	}
	if IsDisposableMailSocket("/tmp/mailclientd-1000.sock") {
		t.Fatal("uid default socket must not look disposable")
	}
	if IsDisposableMailSocket("/run/user/1000/mailclientd.sock") {
		t.Fatal("xdg default socket must not look disposable")
	}
	ok := filepath.Join("/tmp/mailclientd-abc123", "mailclientd.sock")
	if !IsDisposableMailSocket(ok) {
		t.Fatalf("StartDemo path %s", ok)
	}
	if err := AssertMemoryBackend("memory"); err != nil {
		t.Fatal(err)
	}
	if err := AssertMemoryBackend("imap"); err == nil {
		t.Fatal("imap must be rejected")
	}
}
