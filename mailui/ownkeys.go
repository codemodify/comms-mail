package mailui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// keysSection is Settings › Security › Keys: for each address you send
// from, the OpenPGP key and S/MIME certificates secretvault holds, and
// making one or bringing one in. secretvault makes and keeps them;
// comms-mail never holds a private key. With another store it says
// secretvault keeps them. refresh asks secretvault again.
func keysSection(a *app.Application, cli *mailcore.Client) (section widget.Component, refresh func()) {
	list := widgets.NewColumn().WithGap(10)
	note := wrapLabel("")
	// say is the note under the intro, hidden when there is nothing to say.
	say := func(text string) {
		note.SetText(text)
		note.SetVisible(text != "")
	}
	say("")
	col := widgets.NewColumn(
		wrapLabel("secretvault keeps the keys that sign and decrypt your mail: comms-mail asks it to, and never holds a private key."),
		note, list).WithGap(8)

	var show func(keys mailcore.OwnKeys)
	refresh = func() {
		runAsync(a, func() (any, error) { return cli.OwnKeys() }, func(v any, err error) {
			if err != nil {
				say("Could not ask secretvault: " + err.Error())
				return
			}
			show(v.(mailcore.OwnKeys))
		})
	}
	work := func(btn *widgets.Button, what string, do func() (mailcore.OwnKeys, error)) {
		btn.SetEnabled(false)
		say(what + "…")
		runAsync(a, func() (any, error) { return do() }, func(v any, err error) {
			btn.SetEnabled(true)
			say("")
			if err != nil {
				widgets.Warn(list, "Your keys", err.Error(), nil)
				return
			}
			show(v.(mailcore.OwnKeys))
		})
	}
	show = func(keys mailcore.OwnKeys) {
		list.ClearChildren()
		say("")
		switch {
		case keys.Locked:
			say("secretvault is locked, so your keys cannot be shown.")
			unlock := newButton("Unlock secretvault…", nil)
			unlock.OnClick = func() {
				runAsync(a, func() (any, error) { return nil, cli.UnlockSecrets("") }, func(_ any, err error) {
					if err != nil {
						widgets.Warn(unlock, "secretvault", "secretvault stayed locked: "+err.Error(), nil)
						return
					}
					refresh()
				})
			}
			list.Add(foldRow(unlock))
		case !keys.Available || keys.Why != "":
			say(keys.Why)
		case len(keys.Addresses) == 0:
			say("Add an account first: keys are made for the addresses you send from.")
		}
		for _, ak := range keys.Addresses {
			list.Add(addressKeys(a, cli, ak, work))
		}
		list.RequestLayout()
		list.Invalidate()
	}
	refresh()
	return col, refresh
}

// addressKeys is one address: its OpenPGP key and S/MIME certificates, and
// what can be done.
func addressKeys(a *app.Application, cli *mailcore.Client, ak mailcore.AddressKeys,
	work func(*widgets.Button, string, func() (mailcore.OwnKeys, error))) widget.Component {
	col := widgets.NewColumn(widgets.NewLabel(ak.Address)).WithGap(3)
	line := func(icon style.ToolIcon, text string) { col.Add(iconLine(icon, text)) }
	var buttons []widget.Component
	if k := ak.PGP; k != nil {
		text := "OpenPGP key " + groupFingerprint(k.Fingerprint)
		if !k.Created.IsZero() {
			text += ", made " + k.Created.Format("2 Jan 2006")
		}
		if !k.Expires.IsZero() {
			text += ", until " + k.Expires.Format("2 Jan 2006")
		}
		line(style.IconLock, text)
		if k.PublicKey != "" {
			cp := newButton("Copy public key", nil)
			cp.Icon = style.IconCopy
			cp.Tip = "Copy your public key, to give to the people who write to you"
			cp.OnClick = func() {
				platform.ClipboardSet(k.PublicKey)
				cp.Text = "Copied"
				cp.Invalidate()
			}
			buttons = append(buttons, cp)
		}
	} else {
		line(style.IconInfo, "No OpenPGP key")
		mk := newButton("Make an OpenPGP key", nil)
		mk.Icon = style.IconPlus
		mk.Tip = "secretvault makes a key for this address and keeps it"
		mk.OnClick = func() {
			work(mk, "Making a key", func() (mailcore.OwnKeys, error) { return cli.MakePGPKey(ak.Address) })
		}
		buttons = append(buttons, mk)
	}
	for _, c := range ak.SMIME {
		text := "S/MIME certificate from " + certName(c.Issuer)
		if !c.NotAfter.IsZero() {
			text += ", valid until " + c.NotAfter.Format("2 Jan 2006")
		}
		line(style.IconLock, text)
		for _, w := range c.Warnings {
			line(style.IconWarning, w)
		}
	}
	if len(ak.SMIME) == 0 {
		line(style.IconInfo, "No S/MIME certificate")
	}
	imp := newButton("Import S/MIME…", nil)
	imp.Icon = style.IconOpen
	imp.Tip = "Bring in a certificate and its key from a .p12 or .pfx file; secretvault keeps them"
	imp.OnClick = func() { importSMIME(a, cli, imp, work) }
	buttons = append(buttons, imp)
	col.Add(foldRow(buttons...))
	return col
}

// importSMIME picks a .p12 / .pfx file, asks for its password in a field
// that holds bytes, never a string, and hands both to secretvault.
func importSMIME(a *app.Application, cli *mailcore.Client, from *widgets.Button,
	work func(*widgets.Button, string, func() (mailcore.OwnKeys, error))) {
	home, _ := os.UserHomeDir()
	widgets.ShowFileDialog(from, widgets.FileDialogOptions{
		Title: "Choose your certificate (.p12, .pfx)",
		Mode:  widgets.FileOpen,
		Path:  home,
		OnPick: func(path string) {
			if strings.TrimSpace(path) == "" {
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				widgets.Warn(from, "Import S/MIME", err.Error(), nil)
				return
			}
			askP12Password(from, filepath.Base(path), func(password []byte) {
				work(from, "Bringing the certificate in", func() (mailcore.OwnKeys, error) {
					defer clear(data)
					return cli.ImportSMIME(data, password)
				})
			}, func() { clear(data) })
		},
	})
}

// askP12Password asks for a certificate file's password. done gets a
// copy the caller wipes; the field wipes its own.
func askP12Password(from widget.Component, name string, done func([]byte), cancelled func()) {
	field := widgets.NewSecretField("Password")
	var ov *widgets.Overlay
	finish := func() {
		field.Wipe()
		widget.DismissOverlay(ov)
	}
	ok := newButton("Import", nil)
	ok.Primary = true
	ok.OnClick = func() {
		pw := field.Bytes()
		finish()
		done(pw)
	}
	cancel := newButton("Cancel", func() {
		finish()
		cancelled()
	})
	buttons := widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(ok, widgets.RoleAccept)
	col := widgets.NewColumn(wrapLabel("The password that opens "+name+":"), field, buttons).WithGap(8).WithPad(4)
	card := widgets.NewPanel("Import S/MIME", col)
	card.Window = true
	card.Raised = true
	card.OnClose = func() {
		finish()
		cancelled()
	}
	ov = widgets.NewOverlay(card)
	ov.Modal = true
	ov.MinCardW = 380
	ov.InitialFocus = field
	widget.ShowOverlay(from, ov)
}

// groupFingerprint is a fingerprint in groups of four, as people compare
// them.
func groupFingerprint(fp string) string {
	fp = strings.ToUpper(strings.ReplaceAll(fp, " ", ""))
	var b strings.Builder
	for i, r := range fp {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// certName is a certificate's issuer by its common name, where it has one.
func certName(dn string) string {
	for _, part := range strings.Split(dn, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "CN="); ok {
			return v
		}
	}
	return dn
}
