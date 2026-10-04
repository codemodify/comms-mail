//go:build linux

package mailcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

// The desktop keyring on Linux: the freedesktop Secret Service over D-Bus
// (org.freedesktop.secrets — GNOME Keyring, KWallet's compatibility
// service, KeePassXC). comms-mail's secrets are items in the default
// collection, found by their attributes: application=comms-mail and
// name=<the secret's name>. Secrets travel in the "plain" session: over
// the user's own session bus, which only the user's programs can reach.
//
// Nothing here prompts unless asked to (keyringUnlock): a locked keyring
// is ErrLocked, and the window asks the desktop to unlock it, so the
// daemon never waits on a dialog while it holds its own locks.

const (
	ssName       = "org.freedesktop.secrets"
	ssPath       = dbus.ObjectPath("/org/freedesktop/secrets")
	ssService    = "org.freedesktop.Secret.Service"
	ssCollection = "org.freedesktop.Secret.Collection"
	ssItem       = "org.freedesktop.Secret.Item"
	ssPrompt     = "org.freedesktop.Secret.Prompt"
	ssProps      = "org.freedesktop.DBus.Properties"
	ssApp        = "comms-mail"
)

// ssCallTimeout bounds each call, so a hung keyring cannot hang the daemon.
var ssCallTimeout = 5 * time.Second

// ssSecret is the Secret Service's Secret struct, (oayays).
type ssSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// ssConn is one conversation with the Secret Service: a private bus
// connection and a plain session on it, closed when done.
type ssConn struct {
	conn    *dbus.Conn
	session dbus.ObjectPath
}

func keyringName() string { return "the desktop keyring (Secret Service)" }

// keyringBackend is the keyring's own name.
func keyringBackend() string { return "Secret Service" }

func ssOpen() (*ssConn, error) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" && os.Getenv("XDG_RUNTIME_DIR") == "" {
		return nil, errors.New("no desktop session bus to reach a keyring on")
	}
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, fmt.Errorf("the desktop session bus: %w", err)
	}
	c := &ssConn{conn: conn}
	ctx, cancel := context.WithTimeout(context.Background(), ssCallTimeout)
	defer cancel()
	if err := conn.Auth(nil); err != nil {
		c.close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		c.close()
		return nil, err
	}
	var owned bool
	_ = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, ssName).Store(&owned)
	if !owned {
		var activatable []string
		_ = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable)
		found := false
		for _, n := range activatable {
			found = found || n == ssName
		}
		if !found {
			c.close()
			return nil, errors.New("no keyring is running on this desktop (nothing provides org.freedesktop.secrets)")
		}
	}
	var out dbus.Variant
	if err := c.service().CallWithContext(ctx, ssService+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&out, &c.session); err != nil {
		c.close()
		return nil, fmt.Errorf("the keyring: %w", err)
	}
	return c, nil
}

func (c *ssConn) close() {
	if c.session != "" && c.session != "/" {
		_ = c.conn.Object(ssName, c.session).Call("org.freedesktop.Secret.Session.Close", 0).Err
	}
	_ = c.conn.Close()
}

func (c *ssConn) service() dbus.BusObject { return c.conn.Object(ssName, ssPath) }

func (c *ssConn) call(path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), ssCallTimeout)
	defer cancel()
	return c.conn.Object(ssName, path).CallWithContext(ctx, method, 0, args...)
}

// collection is the default collection (the login keyring).
func (c *ssConn) collection() (dbus.ObjectPath, error) {
	var path dbus.ObjectPath
	if err := c.call(ssPath, ssService+".ReadAlias", "default").Store(&path); err != nil {
		return "", fmt.Errorf("the keyring: %w", err)
	}
	if path == "/" || path == "" {
		return "", errors.New("the keyring has no default collection")
	}
	return path, nil
}

func (c *ssConn) locked(coll dbus.ObjectPath) (bool, error) {
	var v dbus.Variant
	if err := c.call(coll, ssProps+".Get", ssCollection, "Locked").Store(&v); err != nil {
		return false, fmt.Errorf("the keyring: %w", err)
	}
	locked, _ := v.Value().(bool)
	return locked, nil
}

// ready opens the default collection, unlocked; ErrLocked when it is
// locked (nothing prompts).
func (c *ssConn) ready() (dbus.ObjectPath, error) {
	coll, err := c.collection()
	if err != nil {
		return "", err
	}
	locked, err := c.locked(coll)
	if err != nil {
		return "", err
	}
	if locked {
		return "", ErrLocked
	}
	return coll, nil
}

func (c *ssConn) search(coll dbus.ObjectPath, attrs map[string]string) ([]dbus.ObjectPath, error) {
	var items []dbus.ObjectPath
	if err := c.call(coll, ssCollection+".SearchItems", attrs).Store(&items); err != nil {
		return nil, fmt.Errorf("the keyring: %w", err)
	}
	return items, nil
}

func ssAttrs(name string) map[string]string {
	return map[string]string{"application": ssApp, "name": name}
}

func keyringReady() error {
	c, err := ssOpen()
	if err != nil {
		return err
	}
	defer c.close()
	_, err = c.ready()
	return err
}

func keyringGet(name string) (string, bool, error) {
	c, err := ssOpen()
	if err != nil {
		return "", false, err
	}
	defer c.close()
	coll, err := c.ready()
	if err != nil {
		return "", false, err
	}
	items, err := c.search(coll, ssAttrs(name))
	if err != nil || len(items) == 0 {
		return "", false, err
	}
	var sec ssSecret
	if err := c.call(items[0], ssItem+".GetSecret", c.session).Store(&sec); err != nil {
		return "", false, fmt.Errorf("the keyring: %w", err)
	}
	return string(sec.Value), true, nil
}

func keyringSet(name, value string) error {
	c, err := ssOpen()
	if err != nil {
		return err
	}
	defer c.close()
	coll, err := c.ready()
	if err != nil {
		return err
	}
	props := map[string]dbus.Variant{
		"org.freedesktop.Secret.Item.Label":      dbus.MakeVariant("comms-mail: " + name),
		"org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(ssAttrs(name)),
	}
	sec := ssSecret{Session: c.session, Parameters: []byte{}, Value: []byte(value), ContentType: "text/plain; charset=utf8"}
	var item, prompt dbus.ObjectPath
	if err := c.call(coll, ssCollection+".CreateItem", props, sec, true).Store(&item, &prompt); err != nil {
		return fmt.Errorf("the keyring: %w", err)
	}
	if prompt != "/" && prompt != "" {
		return ErrLocked // the collection wants confirming: unlock first
	}
	return nil
}

func keyringDelete(name string) error {
	c, err := ssOpen()
	if err != nil {
		return err
	}
	defer c.close()
	coll, err := c.ready()
	if err != nil {
		return err
	}
	items, err := c.search(coll, ssAttrs(name))
	if err != nil {
		return err
	}
	for _, it := range items {
		var prompt dbus.ObjectPath
		if err := c.call(it, ssItem+".Delete").Store(&prompt); err != nil {
			return fmt.Errorf("the keyring: %w", err)
		}
	}
	return nil
}

func keyringNames() ([]string, error) {
	c, err := ssOpen()
	if err != nil {
		return nil, err
	}
	defer c.close()
	coll, err := c.ready()
	if err != nil {
		return nil, err
	}
	items, err := c.search(coll, map[string]string{"application": ssApp})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, it := range items {
		var v dbus.Variant
		if err := c.call(it, ssProps+".Get", ssItem, "Attributes").Store(&v); err != nil {
			continue
		}
		if attrs, ok := v.Value().(map[string]string); ok && attrs["name"] != "" {
			names = append(names, attrs["name"])
		}
	}
	return names, nil
}

// keyringUnlock asks the desktop to unlock the default collection: it
// shows its own prompt, and this waits (up to two minutes) for the answer.
func keyringUnlock() error {
	c, err := ssOpen()
	if err != nil {
		return err
	}
	defer c.close()
	coll, err := c.collection()
	if err != nil {
		return err
	}
	if locked, err := c.locked(coll); err != nil || !locked {
		return err
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := c.call(ssPath, ssService+".Unlock", []dbus.ObjectPath{coll}).Store(&unlocked, &prompt); err != nil {
		return fmt.Errorf("the keyring: %w", err)
	}
	if prompt == "/" || prompt == "" {
		return nil
	}
	signals := make(chan *dbus.Signal, 4)
	c.conn.Signal(signals)
	defer c.conn.RemoveSignal(signals)
	if err := c.conn.AddMatchSignal(dbus.WithMatchObjectPath(prompt), dbus.WithMatchInterface(ssPrompt), dbus.WithMatchMember("Completed")); err != nil {
		return err
	}
	if err := c.call(prompt, ssPrompt+".Prompt", "").Err; err != nil {
		return fmt.Errorf("the keyring: %w", err)
	}
	timeout := time.After(2 * time.Minute)
	for {
		select {
		case sig := <-signals:
			if sig == nil || sig.Path != prompt || sig.Name != ssPrompt+".Completed" {
				continue
			}
			if len(sig.Body) > 0 {
				if dismissed, _ := sig.Body[0].(bool); dismissed {
					return errors.New("the keyring stayed locked (the prompt was dismissed)")
				}
			}
			return nil
		case <-timeout:
			return errors.New("the keyring was not unlocked in time")
		}
	}
}
