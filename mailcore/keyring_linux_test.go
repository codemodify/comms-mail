//go:build linux

package mailcore

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

// A private D-Bus with a fake Secret Service on it, so the keyring code
// runs against the real protocol without going near the user's keyring.
// The bus has no service directories: nothing on it can be activated, so
// no real keyring daemon can be started on it by accident.

type fakeSS struct {
	conn    *dbus.Conn
	mu      sync.Mutex
	locked  bool
	dismiss bool // the unlock prompt is dismissed
	items   map[dbus.ObjectPath]*fakeItem
	next    int
}

type fakeItem struct {
	attrs map[string]string
	value []byte
}

const (
	fakeColl    = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	fakeSession = dbus.ObjectPath("/org/freedesktop/secrets/session/1")
	fakePrompt  = dbus.ObjectPath("/org/freedesktop/secrets/prompt/1")
)

// withFakeKeyring starts the bus and the fake, and points this process's
// session bus at it.
func withFakeKeyring(t *testing.T) *fakeSS {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("no dbus-daemon")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "bus.conf")
	if err := os.WriteFile(cfg, []byte(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:path=`+filepath.Join(dir, "bus")+`</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dbus-daemon", "--config-file="+cfg, "--nofork", "--print-address")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	addr = strings.TrimSpace(addr)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)

	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if reply, err := conn.RequestName(ssName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("name: %v %v", reply, err)
	}
	f := &fakeSS{conn: conn, items: map[dbus.ObjectPath]*fakeItem{}}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(conn.Export(fakeService{f}, ssPath, ssService))
	must(conn.Export(fakeCollection{f}, fakeColl, ssCollection))
	must(conn.Export(fakeProps{f, fakeColl}, fakeColl, ssProps))
	must(conn.Export(fakeSessionObj{}, fakeSession, "org.freedesktop.Secret.Session"))
	must(conn.Export(fakePromptObj{f}, fakePrompt, ssPrompt))
	theKeyring.known, theKeyring.none = map[string]string{}, map[string]bool{}
	t.Cleanup(func() { theKeyring.known, theKeyring.none = map[string]string{}, map[string]bool{} })
	return f
}

func (f *fakeSS) values() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for _, it := range f.items {
		out[it.attrs["name"]] = string(it.value)
	}
	return out
}

type fakeService struct{ f *fakeSS }

func (s fakeService) OpenSession(alg string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	if alg != "plain" {
		return dbus.Variant{}, "/", dbus.MakeFailedError(errors.New("only plain"))
	}
	return dbus.MakeVariant(""), fakeSession, nil
}

func (s fakeService) ReadAlias(name string) (dbus.ObjectPath, *dbus.Error) {
	if name != "default" {
		return "/", nil
	}
	return fakeColl, nil
}

func (s fakeService) Unlock(objs []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	return nil, fakePrompt, nil
}

type fakeSessionObj struct{}

func (fakeSessionObj) Close() *dbus.Error { return nil }

type fakePromptObj struct{ f *fakeSS }

func (p fakePromptObj) Prompt(string) *dbus.Error {
	go func() {
		p.f.mu.Lock()
		dismissed := p.f.dismiss
		if !dismissed {
			p.f.locked = false
		}
		p.f.mu.Unlock()
		_ = p.f.conn.Emit(fakePrompt, ssPrompt+".Completed", dismissed, dbus.MakeVariant(""))
	}()
	return nil
}

type fakeCollection struct{ f *fakeSS }

func (c fakeCollection) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, *dbus.Error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	var out []dbus.ObjectPath
	for p, it := range c.f.items {
		match := true
		for k, v := range attrs {
			match = match && it.attrs[k] == v
		}
		if match {
			out = append(out, p)
		}
	}
	return out, nil
}

func (c fakeCollection) CreateItem(props map[string]dbus.Variant, sec ssSecret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	if c.f.locked {
		return "/", fakePrompt, nil
	}
	attrs, _ := props["org.freedesktop.Secret.Item.Attributes"].Value().(map[string]string)
	if sec.Session != fakeSession {
		return "/", "/", dbus.MakeFailedError(fmt.Errorf("unknown session %s", sec.Session))
	}
	if replace {
		for p, it := range c.f.items {
			if it.attrs["application"] == attrs["application"] && it.attrs["name"] == attrs["name"] {
				it.value = append([]byte(nil), sec.Value...)
				return p, "/", nil
			}
		}
	}
	c.f.next++
	p := dbus.ObjectPath(fmt.Sprintf("%s/%d", fakeColl, c.f.next))
	c.f.items[p] = &fakeItem{attrs: attrs, value: append([]byte(nil), sec.Value...)}
	_ = c.f.conn.Export(fakeItemObj{c.f, p}, p, ssItem)
	_ = c.f.conn.Export(fakeProps{c.f, p}, p, ssProps)
	return p, "/", nil
}

type fakeItemObj struct {
	f    *fakeSS
	path dbus.ObjectPath
}

func (i fakeItemObj) GetSecret(session dbus.ObjectPath) (ssSecret, *dbus.Error) {
	i.f.mu.Lock()
	defer i.f.mu.Unlock()
	it := i.f.items[i.path]
	if it == nil || i.f.locked {
		return ssSecret{}, dbus.MakeFailedError(errors.New("no such item, or locked"))
	}
	return ssSecret{Session: session, Parameters: []byte{}, Value: it.value, ContentType: "text/plain"}, nil
}

func (i fakeItemObj) Delete() (dbus.ObjectPath, *dbus.Error) {
	i.f.mu.Lock()
	defer i.f.mu.Unlock()
	delete(i.f.items, i.path)
	return "/", nil
}

type fakeProps struct {
	f    *fakeSS
	path dbus.ObjectPath
}

func (p fakeProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	switch {
	case iface == ssCollection && name == "Locked":
		return dbus.MakeVariant(p.f.locked), nil
	case iface == ssItem && name == "Attributes":
		if it := p.f.items[p.path]; it != nil {
			return dbus.MakeVariant(it.attrs), nil
		}
	}
	return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("no property %s.%s", iface, name))
}

func TestKeyringSecretService(t *testing.T) {
	f := withFakeKeyring(t)
	if err := keyringReady(); err != nil {
		t.Fatal(err)
	}
	if err := keyringSet("pass/home/imap", "s3cret ü"); err != nil {
		t.Fatal(err)
	}
	if err := keyringSet("oauth/home", `{"accessToken":"a"}`); err != nil {
		t.Fatal(err)
	}
	if err := keyringSet("pass/home/imap", "changed"); err != nil { // replaces
		t.Fatal(err)
	}
	if got := f.values(); len(got) != 2 || got["pass/home/imap"] != "changed" {
		t.Fatalf("keyring holds %v", got)
	}
	if v, ok, err := keyringGet("pass/home/imap"); err != nil || !ok || v != "changed" {
		t.Fatalf("get: %q %v %v", v, ok, err)
	}
	if _, ok, err := keyringGet("pass/none/imap"); err != nil || ok {
		t.Fatalf("get missing: %v %v", ok, err)
	}
	names, err := keyringNames()
	if err != nil || len(names) != 2 {
		t.Fatalf("names %v %v", names, err)
	}
	if err := keyringDelete("oauth/home"); err != nil {
		t.Fatal(err)
	}
	if got := f.values(); len(got) != 1 {
		t.Fatalf("after delete %v", got)
	}

	// Locked: nothing is read or written, and nothing prompts on its own.
	f.mu.Lock()
	f.locked = true
	f.mu.Unlock()
	if err := keyringReady(); !errors.Is(err, ErrLocked) {
		t.Fatalf("ready while locked: %v", err)
	}
	if _, _, err := keyringGet("pass/home/imap"); !errors.Is(err, ErrLocked) {
		t.Fatalf("get while locked: %v", err)
	}
	// A dismissed prompt leaves it locked; an answered one unlocks it.
	f.dismiss = true
	if err := keyringUnlock(); err == nil {
		t.Fatal("a dismissed prompt unlocked the keyring")
	}
	f.dismiss = false
	if err := keyringUnlock(); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := keyringGet("pass/home/imap"); err != nil || !ok || v != "changed" {
		t.Fatalf("after unlock: %q %v %v", v, ok, err)
	}
}

// With no keyring on the bus, it says so.
func TestKeyringMissing(t *testing.T) {
	if err := keyringReady(); err == nil {
		t.Fatal("a keyring was found where there is none")
	}
}
