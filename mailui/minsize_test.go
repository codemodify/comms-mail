package mailui

import (
	"fmt"
	"testing"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Every window, at the smallest size it allows, shows each of its controls
// whole: none cut off at an edge, squeezed to nothing or lying over
// another, so no one has to resize a window to find a button (they had to,
// for Import). A control in a scrolling area may sit below the fold; that
// is what the scroll is for. The smallest size is the larger of what the
// window was made with and what its content needs (fitMinWidth).

// shownOnScreen reports whether c and every ancestor are visible, and
// whether it sits inside a scrolling area.
func shownOnScreen(c widget.Component) (shown, scrolled bool) {
	for x := c; x != nil; x = x.Parent() {
		if !x.Visible() {
			return false, false
		}
		if _, ok := x.(*widgets.ScrollView); ok && x != c {
			scrolled = true
		}
	}
	return true, scrolled
}

func scrollAncestor(c widget.Component) widget.Component {
	for x := c.Parent(); x != nil; x = x.Parent() {
		if _, ok := x.(*widgets.ScrollView); ok {
			return x
		}
	}
	return c
}

func controlName(c widget.Component) string {
	switch v := c.(type) {
	case *widgets.Button:
		return fmt.Sprintf("button %q", v.Text)
	case *widgets.Checkbox:
		return fmt.Sprintf("checkbox %q", v.Text)
	case *widgets.TextField:
		return fmt.Sprintf("field %q", v.Placeholder)
	case *widgets.ComboBox:
		return "combo box"
	}
	return ""
}

// auditAtMinSize sizes w to minW×minH and checks its controls, on every
// tab of every tab view.
func auditAtMinSize(t *testing.T, a *app.Application, w *app.Window, name string, minW, minH int) {
	t.Helper()
	if mw, _ := w.MinSize(); int(mw+0.5) > minW {
		minW = int(mw + 0.5)
	}
	// The minimum does not resize the window, so it has to open at least
	// that wide.
	if opened := w.Content().Bounds().Dx(); opened+0.5 < float32(minW) {
		t.Errorf("%s opens %v wide, under its minimum %d", name, opened, minW)
	}
	w.Inject(platform.Event{Kind: platform.EventResize, Width: minW, Height: minH})
	a.PumpOnce()
	check := func(where string) int {
		n := 0
		type placed struct {
			c     widget.Component
			label string
			r     paintengine2d.Rect
		}
		var seen []placed
		defer func() {
			for i := range seen {
				for j := i + 1; j < len(seen); j++ {
					p, q := seen[i], seen[j]
					if widget.Contains(p.c, q.c) || widget.Contains(q.c, p.c) {
						continue
					}
					if in := p.r.Intersect(q.r); in.Dx() > 1 && in.Dy() > 1 {
						t.Errorf("%s%s: %s lies over %s at %dx%d: %v and %v", name, where, p.label, q.label, minW, minH, p.r, q.r)
					}
				}
			}
		}()
		widget.Walk(w.Content(), func(c widget.Component) {
			label := controlName(c)
			if label == "" {
				return
			}
			shown, scrolled := shownOnScreen(c)
			if !shown {
				return
			}
			n++
			r := widget.LocalToWindow(c, c.LocalBounds())
			if scrolled {
				// Below the fold is fine in a scrolling area; past its
				// sides is not (nothing scrolls sideways).
				sv := scrollAncestor(c)
				vr := widget.LocalToWindow(sv, sv.LocalBounds())
				if r.Min.X < vr.Min.X-0.5 || r.Max.X > vr.Max.X+0.5 {
					t.Errorf("%s%s: %s runs off the side of its scroll area: %v in %v", name, where, label, r, vr)
				}
				// What the scroll area clips away lies over nothing.
				if r = r.Intersect(vr); !r.Empty() {
					seen = append(seen, placed{c, label, r})
				}
				return
			}
			seen = append(seen, placed{c, label, r})
			if r.Min.X < -0.5 || r.Min.Y < -0.5 || r.Max.X > float32(minW)+0.5 || r.Max.Y > float32(minH)+0.5 {
				t.Errorf("%s%s: %s is cut off at %dx%d: %v", name, where, label, minW, minH, r)
			} else if r.Dx() < 8 || r.Dy() < 8 {
				t.Errorf("%s%s: %s is squeezed to %v at %dx%d", name, where, label, r, minW, minH)
			}
		})
		return n
	}
	check("")
	var tvs []*widgets.TabView
	widget.Walk(w.Content(), func(c widget.Component) {
		if tv, ok := c.(*widgets.TabView); ok {
			tvs = append(tvs, tv)
		}
	})
	for _, tv := range tvs {
		for i, title := range tv.Bar().Titles {
			tv.Select(i)
			a.PumpOnce()
			check(fmt.Sprintf(" (tab %q)", title))
		}
		tv.Select(0)
	}
}

// newWindowFrom runs open and returns the window it made.
func newWindowFrom(t *testing.T, a *app.Application, open func()) *app.Window {
	t.Helper()
	before := map[*app.Window]bool{}
	for _, w := range a.Windows() {
		before[w] = true
	}
	open()
	a.PumpOnce()
	for _, w := range a.Windows() {
		if !before[w] {
			return w
		}
	}
	t.Fatal("no window opened")
	return nil
}

func TestWindowsFitAtTheirMinimumSize(t *testing.T) {
	t.Setenv("UITK_MAIL_NO_OPEN", "1")
	cli := demoClient(t)
	for _, look := range []struct {
		name string
		look style.LookAndFeel
	}{{"light", style.LightLook()}, {"compact dark", style.WithDensity(style.DarkLook(), style.DensityCompact)}} {
		a := uitoolkit.New(uitoolkit.Options{Look: look.look, Headless: true, Scale: 1})
		reply := mailcore.Message{From: "Kai <kai@paintengine.example>", To: "ada@example.com", Subject: "A long thread",
			Body: "a line of the quoted message\n"}
		for i := 0; i < 6; i++ {
			reply.Body += reply.Body
		}
		inbox, _ := cli.ListMessages(mailcore.FolderAdaInbox, mailcore.Filter{})
		wins := []struct {
			name       string
			minW, minH int
			open       func()
		}{
			{"Write", 520, 400, func() { _, _ = OpenCompose(a, cli, ComposeOptions{}) }},
			{"Write (reply)", 520, 400, func() { _, _ = OpenCompose(a, cli, ComposeOptions{ReplyTo: &reply}) }},
			{"Add account", 460, 520, func() { _, _ = OpenAddAccount(a, cli, nil) }},
			{"Settings", 710, 440, func() { _, _ = OpenPrefs(a, cli, nil) }},
			{"Tag editor", 360, 200, func() { _, _ = OpenTagEditor(a, mailcore.Tag{}, false, nil) }},
			{"Import", 500, 440, func() {
				openImportWindow(a, cli, []mailcore.ImportSource{sampleImportSource()}, map[string]bool{}, nil)
			}},
			{"Message source", 480, 320, func() { _, _ = OpenMessageSource(a, inbox[0], "From: a\r\n\r\nbody") }},
			{"Rule editor", 520, 420, func() { openRuleEditor(a, cli, mailcore.FilterRule{Enabled: true}, nil) }},
			{"Where passwords are kept", 460, 420, func() {
				openStoreChooser(a, cli, plainIntro(mailcore.SecretsStatus{PlainAccounts: []string{"ada@example.com"}}), mailcore.SecretsStatus{Supported: true, KeyringName: "the desktop keyring", KeyringAvailable: true}, nil)
			}},
			{"Passphrase (unlock)", 440, 300, func() { openPassphrase(a, cli, passUnlock, nil) }},
			{"Passphrase (change)", 440, 300, func() { openPassphrase(a, cli, passChange, nil) }},
		}
		for _, win := range wins {
			w := newWindowFrom(t, a, win.open)
			auditAtMinSize(t, a, w, look.name+" "+win.name, win.minW, win.minH)
			w.Close()
		}
	}
}

// The main window at its minimum size, with an invitation's card and a
// newsletter's image bar showing in the reading pane.
func TestMainWindowFitsAtMinimumSize(t *testing.T) {
	for _, layout := range []LayoutMode{LayoutVertical, LayoutClassic} {
		s, a, w, done := openMailLookSession(t, style.DarkLook(), false, AppOptions{Layout: layout})
		s.selectFolder(mailcore.FolderWorkInbox)
		s.selected = []mailcore.MessageID{mailcore.DemoInviteID}
		s.loadPreview()
		s.waitIdle()
		auditAtMinSize(t, a, w, fmt.Sprintf("Mail (layout %d, invite)", layout), 860, 560)

		s.selectFolder(mailcore.FolderAdaInbox)
		s.selected = []mailcore.MessageID{mailcore.DemoNewsletterID}
		s.loadPreview()
		s.waitIdle()
		auditAtMinSize(t, a, w, fmt.Sprintf("Mail (layout %d, images)", layout), 860, 560)
		done()
	}
}

// Smart Folder is opened from the session.
func TestSmartFolderWindowFits(t *testing.T) {
	s, a, _, done := openMailLookSession(t, style.LightLook(), false, AppOptions{})
	defer done()
	w := newWindowFrom(t, a, s.newSmartFolder)
	auditAtMinSize(t, a, w, "Smart Folder", 320, 220)
}

func sampleImportSource() mailcore.ImportSource {
	src := mailcore.ImportSource{Source: "Thunderbird", Note: "A note about this client that is long enough to wrap across the window."}
	for i := 0; i < 6; i++ {
		addr := fmt.Sprintf("person%d@example.com", i)
		src.Accounts = append(src.Accounts, mailcore.ImportedAccount{Source: "Thunderbird", Account: mailcore.AccountConfig{
			Address: addr, Protocol: mailcore.ProtoIMAP, IMAP: mailcore.ServerConfig{Host: "imap.example.com:993"},
		}})
	}
	for i := 0; i < 12; i++ {
		src.Mail = append(src.Mail, mailcore.LocalMailStore{Source: "Thunderbird", Name: fmt.Sprintf("Folder %d", i),
			Path: "/tmp/x", Kind: mailcore.StoreMbox})
	}
	return src
}
