package mailui

import (
	"strings"
	"testing"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// openSettings is Settings over the demo daemon, 700×560.
func openSettings(t *testing.T) (*app.Application, *app.Window, *mailcore.Client, *widgets.TabView) {
	t.Helper()
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	return openSettingsOn(t, demoClient(t))
}

// openSettingsOn is Settings over cli's daemon, 700×560.
func openSettingsOn(t *testing.T, cli *mailcore.Client) (*app.Application, *app.Window, *mailcore.Client, *widgets.TabView) {
	t.Helper()
	a := uitoolkit.New(uitoolkit.Options{Look: style.LightLook(), Headless: true, Scale: 1})
	w, err := a.NewWindow(platform.WindowOptions{Title: "Settings", Width: 700, Height: 560, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	w.SetContent(PrefsApp(a, w, cli, nil))
	a.PumpOnce()
	var tv *widgets.TabView
	widget.Walk(w.Content(), func(c widget.Component) {
		if v, ok := c.(*widgets.TabView); ok && tv == nil {
			tv = v
		}
	})
	if tv == nil {
		t.Fatal("no tabs")
	}
	return a, w, cli, tv
}

// selectTab shows the tab called title and returns the tab view, which
// walks as the page showing (hidden pages are not walked).
func selectTab(a *app.Application, tv *widgets.TabView, title string) widget.Component {
	for i, tt := range tv.Bar().Titles {
		if tt == title {
			tv.Select(i)
			a.PumpOnce()
			return tv
		}
	}
	return nil
}

// Settings starts with its tabs at the top — no title strip — with Close
// at the right under them, and a status bar that names the config file in
// use and nothing else. The lists start each tab: no text over them.
func TestSettingsLayout(t *testing.T) {
	a, w, _, tv := openSettings(t)
	widget.Walk(w.Content(), func(c widget.Component) {
		if _, ok := c.(*widgets.TitleBar); ok {
			t.Fatal("Settings has a title strip")
		}
	})
	if y := widget.DeviceOrigin(tv).Y; y > 1 {
		t.Fatalf("the tabs start at %v, not the top", y)
	}
	var closeBtn *widgets.Button
	var status *widgets.StatusBar
	widget.Walk(w.Content(), func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.Button:
			if v.Text == "Close" {
				closeBtn = v
			}
		case *widgets.StatusBar:
			status = v
		}
	})
	if closeBtn == nil || widget.DeviceBounds(closeBtn).Max.X < 700-20 {
		t.Fatalf("Close is not at the right: %v", widget.DeviceBounds(closeBtn))
	}
	if parts := status.Parts(); len(parts) != 1 || parts[0] != mailcore.ConfigPath() {
		t.Fatalf("status bar %q", parts)
	}
	for _, title := range []string{"Accounts", "Tags", "Filters", "Appearance"} {
		page := selectTab(a, tv, title)
		widget.Walk(page, func(c widget.Component) {
			// A heading is a Label too (NewTitle).
			if v, ok := c.(*widgets.Label); ok && strings.TrimSpace(v.Text) != "" {
				t.Errorf("%s: text %q", title, v.Text)
			}
		})
	}
	// Appearance: the shades and Use the shared theme on one row.
	page := selectTab(a, tv, "Appearance")
	var shades *widgets.ComboBox
	var follow *widgets.Button
	widget.Walk(page, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.ComboBox:
			shades = v
		case *widgets.Button:
			if strings.HasPrefix(v.Text, "Use the shared theme") {
				follow = v
			}
		}
	})
	if shades == nil || follow == nil {
		t.Fatal("Appearance: no shades or Use the shared theme")
	}
	sb, fb := widget.DeviceBounds(shades), widget.DeviceBounds(follow)
	if sb.Max.Y < fb.Min.Y || fb.Max.Y < sb.Min.Y || fb.Min.X < sb.Max.X {
		t.Fatalf("not on one row: shades %v, follow %v", sb, fb)
	}
}

// Signatures: the Froms on the left, the one picked's signature on the
// right, saved as it is typed — no Save button.
func TestSignaturesSaveAsTyped(t *testing.T) {
	a, _, cli, tv := openSettings(t)
	page := selectTab(a, tv, "Signatures")
	var list *widgets.TableView
	var sig *widgets.TextArea
	widget.Walk(page, func(c widget.Component) {
		switch v := c.(type) {
		case *widgets.TableView:
			list = v
		case *widgets.TextArea:
			sig = v
		case *widgets.Button:
			t.Errorf("a button on Signatures: %q", v.Text)
		}
	})
	if list == nil || sig == nil || list.RowCount < 2 {
		t.Fatal("Signatures is not a list and a signature")
	}
	if widget.DeviceBounds(list).Max.X > widget.DeviceBounds(sig).Min.X {
		t.Fatal("the list is not to the left of the signature")
	}
	identity := func(i int) mailcore.Identity {
		ids := sendIdentities(cli)
		return ids[i]
	}
	// Typing saves a moment after it stops.
	sig.SetText("Ada\nTyped here")
	sig.OnInput(sig.Text)
	deadline := time.Now().Add(3 * time.Second)
	for identity(0).Signature != "Ada\nTyped here" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		a.PumpOnce()
	}
	if got := identity(0).Signature; got != "Ada\nTyped here" {
		t.Fatalf("not saved after typing: %q", got)
	}
	// Picking another From saves what was typed at once, and shows its own.
	sig.SetText("Ada\nTyped again")
	sig.OnInput(sig.Text)
	list.Selected = 1
	list.OnSelect(1)
	a.PumpOnce()
	if got := identity(0).Signature; got != "Ada\nTyped again" {
		t.Fatalf("not saved on switching: %q", got)
	}
	if sig.Text != identity(1).Signature {
		t.Fatalf("the second From's signature is not shown: %q", sig.Text)
	}
}

// The logo is the window icon at every size a desktop asks for: round,
// with its corners clear.
func TestLogoIcons(t *testing.T) {
	imgs := AppIcons()
	if len(imgs) != 8 {
		t.Fatalf("%d sizes", len(imgs))
	}
	for _, img := range imgs {
		if img.Width != img.Height {
			t.Fatalf("not square: %dx%d", img.Width, img.Height)
		}
		_, _, _, corner := img.PremulAt(0, 0)
		_, _, _, mid := img.PremulAt(img.Width/2, img.Height/2)
		if corner != 0 || mid == 0 {
			t.Fatalf("%d px: corner alpha %d, middle %d", img.Width, corner, mid)
		}
	}
}

// Settings › Security: its topics — Passwords, Keys, Remote images — in a
// sidebar on the left, and only the one picked on the right; no Privacy or
// Keys tab of their own. Keys asks secretvault again whenever it shows, so
// a key made in secretvault meanwhile is there when the tab is opened
// again.
func TestSecurityTopics(t *testing.T) {
	s, _, sv, _ := securityDaemon(t)
	if _, err := s.cli.PutAccount(mailcore.AccountConfig{ID: "home", Address: "ada@example.com",
		IMAP: mailcore.ServerConfig{Host: "127.0.0.1:1", TLSMode: "ssl"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.cli.AllowRemoteImages("news@example.com", true); err != nil {
		t.Fatal(err)
	}
	a, _, _, tv := openSettingsOn(t, s.cli)
	for _, title := range tv.Bar().Titles {
		if title == "Privacy" || title == "Keys" {
			t.Errorf("a %s tab", title)
		}
	}
	page := selectTab(a, tv, "Security")
	var list *widgets.ListView
	widget.Walk(page, func(c widget.Component) {
		if v, ok := c.(*widgets.ListView); ok {
			list = v
		}
	})
	if list == nil || !list.Sidebar {
		t.Fatal("no sidebar of topics")
	}
	var topics []string
	for i := 0; i < list.Count; i++ {
		topics = append(topics, list.ItemText(i))
	}
	if strings.Join(topics, "|") != "Passwords|Keys|Remote images" {
		t.Fatalf("topics %q", topics)
	}
	// shows is what the right-hand side says: its labels and buttons, and
	// "(table)" for a table.
	shows := func() string {
		var out []string
		widget.Walk(page, func(c widget.Component) {
			switch v := c.(type) {
			case *widgets.Label:
				out = append(out, v.Text)
			case *widgets.Button:
				out = append(out, v.Text)
			case *widgets.TableView:
				out = append(out, "(table)")
			}
		})
		return strings.Join(out, "\n")
	}
	pick := func(i int) string {
		list.Selected = i
		list.OnSelect(i)
		a.PumpOnce()
		return shows()
	}
	for i, want := range []struct{ has, hasNot string }{
		{"Apply", "OpenPGP"},
		{"No OpenPGP key", "Apply"},
		{"(table)", "OpenPGP"},
	} {
		if got := pick(i); !strings.Contains(got, want.has) || strings.Contains(got, want.hasNot) {
			t.Fatalf("%s shows:\n%s", topics[i], got)
		}
	}
	pick(1)
	sv.OwnKeys("ada@example.com")
	selectTab(a, tv, "Accounts")
	selectTab(a, tv, "Security")
	if got := shows(); !strings.Contains(got, "OpenPGP key ") || strings.Contains(got, "No OpenPGP key") {
		t.Fatalf("Keys did not ask again:\n%s", got)
	}
}
