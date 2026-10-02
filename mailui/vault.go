package mailui

import (
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Where comms-mail keeps saved passwords and sign-ins (mailcore's
// secrets.go): the desktop keyring, secretvault, an encrypted file, or a
// plain file. The window asks when it finds passwords readable in
// mail.json and before the first account is saved, unlocks the store at
// start, and Settings › Privacy moves everything to another store.

// forgetText is what a forgotten passphrase costs.
const forgetText = "If you forget the passphrase, the saved passwords cannot be recovered: you would type each account's password again."

// secretsMotto opens the chooser.
const secretsMotto = "Secrets. Secrets. Secrets. Keep'em safe."

// plainIntro is the chooser's text before a store is chosen: what is
// saved readable now — accounts' passwords, old sign-in files — or that
// nothing is saved yet (no accounts, or none with a password).
func plainIntro(st mailcore.SecretsStatus) string {
	var now, next string
	switch {
	case len(st.PlainAccounts) > 0:
		now = "At the moment the passwords for " + strings.Join(st.PlainAccounts, ", ") + " are stored as readable text in " +
			mailcore.ConfigPath() + ", where any program running as you can read them."
		if st.PlainTokens {
			now += " So are your Google or Microsoft sign-ins."
		}
		next = "Choose where they should be kept. They all move there and leave mail.json (unless you keep the plain file). " +
			"Not now leaves everything as it is."
	case st.PlainTokens:
		now = "At the moment your Google or Microsoft sign-ins are saved in files whose key is kept right beside them, " +
			"so any program running as you can read them."
		next = "Choose where they should be kept. They all move there, and the old files are removed. " +
			"Not now leaves everything as it is."
	default:
		now = "No passwords are saved yet."
		next = "Choose where comms-mail should keep them once you add an account. " +
			"Not now leaves it for later: comms-mail asks again before it saves a password."
	}
	return secretsMotto + "\n\n" + now + "\n\n" + next
}

// accountIntro is the text before the first password is saved.
const accountIntro = "Before comms-mail saves this account's password, choose where your passwords are kept."

// laterNote is under the choices when they are first offered.
const laterNote = "You can set this up later, or switch to another place at any time, with Change where… on the Privacy tab in Settings."

// switchIntro is the text for moving the secrets elsewhere.
const switchIntro = "Choose where comms-mail keeps your passwords and sign-ins. They all move there, and the copies where they are now are removed."

// storeOption is one of the places in the chooser.
type storeOption struct {
	kind, title, text string
	available         bool
}

func storeOptions(st mailcore.SecretsStatus) []storeOption {
	keyring := "Your system's own password store — here, " + st.KeyringName + ". The desktop unlocks it when you log in, so there is no extra passphrase."
	if !st.KeyringAvailable {
		keyring += "\nNot available: " + st.KeyringProblem
	}
	return []storeOption{
		{mailcore.StoreKeyring, "Desktop keyring", keyring, st.KeyringAvailable},
		{mailcore.StoreSecretVault, "secretvault", "codemodify/secretvault. Not available yet.", st.SecretVaultAvailable},
		{mailcore.StoreEncrypted, "Encrypted file", "A file only your passphrase opens (Argon2id, AES-256-GCM). comms-mail asks for the passphrase once each time it starts. " + forgetText, true},
		{mailcore.StorePlain, "Plain file", "Passwords stay readable in mail.json, as before: any program running as you can read them.", true},
	}
}

// openStoreChooser asks where the secrets should be kept and moves them
// there. Nothing is picked beforehand. done runs on the UI goroutine once
// they have moved.
func openStoreChooser(a *app.Application, cli *mailcore.Client, intro string, st mailcore.SecretsStatus, done func()) *app.Window {
	title := "Where should comms-mail keep your passwords?"
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 620, Height: 800, MinWidth: 460, MinHeight: 420,
		// A question the desktop puts in front, in the middle, as it does
		// any dialog, rather than where a new window happens to land.
		Role: platform.RoleDialog, Center: true,
	})
	if err != nil {
		return nil
	}
	first := widgets.NewPasswordField("", nil)
	again := widgets.NewPasswordField("", nil)
	passBox := widgets.NewColumn(
		widgets.NewLabel("Passphrase (at least 8 characters)"), first,
		widgets.NewLabel("Type it again"), again,
	).WithGap(6)
	passBox.SetVisible(false)

	opts := storeOptions(st)
	chosen := -1
	var okBtn *widgets.Button
	radios := make([]*widgets.RadioButton, len(opts))
	list := widgets.NewColumn().WithGap(10)
	for i, o := range opts {
		i, o := i, o
		label := o.title
		if o.kind == st.Store {
			label += " (in use now)"
		}
		rb := widgets.NewRadio(label, false, nil)
		rb.OnChange = func(on bool) {
			if !on {
				return
			}
			chosen = i
			for j, other := range radios {
				if j != i && other.Selected {
					other.SetSelected(false)
				}
			}
			passBox.SetVisible(o.kind == mailcore.StoreEncrypted)
			okBtn.SetEnabled(true)
			passBox.RequestLayout()
		}
		rb.SetEnabled(o.available && o.kind != st.Store)
		radios[i] = rb
		desc := wrapLabel(o.text)
		list.Add(widgets.NewColumn(rb, widgets.NewColumn(desc).WithPadding(28, 0, 0, 0)).WithGap(2))
	}

	submit := func() {
		if chosen < 0 || !okBtn.Enabled() {
			return
		}
		kind := opts[chosen].kind
		p := ""
		if kind == mailcore.StoreEncrypted {
			p = first.Text
			if len([]rune(p)) < mailcore.MinPassphrase {
				widgets.Warn(win.Content(), "Passphrase", "Choose a passphrase of at least 8 characters.", nil)
				return
			}
			if p != again.Text {
				widgets.Warn(win.Content(), "Passphrase", "The two passphrases are not the same.", nil)
				return
			}
		}
		okBtn.SetEnabled(false)
		runAsync(a, func() (any, error) {
			return nil, cli.UseStore(kind, p)
		}, func(_ any, err error) {
			if err != nil {
				okBtn.SetEnabled(true)
				widgets.Warn(win.Content(), "Keep passwords", err.Error(), nil)
				return
			}
			win.Close()
			if done != nil {
				done()
			}
		})
	}
	first.OnSubmit = func(string) { submit() }
	again.OnSubmit = func(string) { submit() }
	okBtn = newButton("OK", submit)
	okBtn.Primary = true
	okBtn.SetEnabled(false)
	cancelText := "Not now"
	if st.Store != "" {
		cancelText = "Cancel"
	}
	buttons := widgets.NewButtonBox().
		AddButton(newButton(cancelText, func() { win.Close() }), widgets.RoleReject).
		AddButton(okBtn, widgets.RoleAccept)

	// The explanation and the choices scroll in a small window; the
	// passphrase fields, the note that this can be changed later and the
	// buttons stay in view.
	body := widgets.NewColumn(wrapLabel(intro), list).WithGap(14)
	body.AddFlex(widgets.NewSpacer(), 1)
	scroll := widgets.NewScrollView(body)
	col := widgets.NewColumn(scroll, passBox).WithGap(10)
	col.AddFlex(scroll, 1)
	if st.Store == "" {
		// Asked from Settings it goes without saying.
		col.Add(wrapLabel(laterNote))
	}
	col.Add(buttons)
	setContent(win, widgets.NewPad(14, col))
	return win
}

type passMode int

const (
	passUnlock passMode = iota // unlock the encrypted file for this run
	passChange                 // replace its passphrase
)

// openPassphrase asks for the encrypted file's passphrase: to unlock it,
// or to change it. done runs on the UI goroutine once it worked.
func openPassphrase(a *app.Application, cli *mailcore.Client, mode passMode, done func()) *app.Window {
	title := map[passMode]string{passUnlock: "Unlock comms-mail", passChange: "Change passphrase"}[mode]
	height := map[passMode]int{passUnlock: 330, passChange: 380}[mode]
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 560, Height: height, MinWidth: 440, MinHeight: 300,
		Role: platform.RoleDialog, Center: true,
	})
	if err != nil {
		return nil
	}
	current := widgets.NewPasswordField("", nil)
	first := widgets.NewPasswordField("", nil)
	again := widgets.NewPasswordField("", nil)
	// Each field has its name above it: a placeholder goes the moment
	// the field has focus, and two blank fields read the same.
	labelled := func(label string, f *widgets.TextField) []widget.Component {
		return []widget.Component{widgets.NewLabel(label), f}
	}
	var text string
	var fields []widget.Component
	var focus widget.Component
	okText := ""
	switch mode {
	case passUnlock:
		text = "comms-mail keeps your mail passwords in an encrypted file, locked with your passphrase. Enter it to connect to your mail.\n\n" +
			"Until you do, comms-mail shows only mail it has already downloaded, and messages you send wait in the Outbox."
		fields = labelled("Passphrase", first)
		focus, okText = first, "Unlock"
	case passChange:
		text = "Your saved passwords and sign-ins are locked again with the new passphrase; the old one stops working."
		fields = append(append(labelled("Current passphrase", current),
			labelled("New passphrase (at least 8 characters)", first)...), labelled("Type the new one again", again)...)
		focus, okText = current, "Change"
	}

	var okBtn *widgets.Button
	submit := func() {
		if !okBtn.Enabled() {
			return // already working on it
		}
		p := first.Text
		if mode == passChange {
			if len([]rune(p)) < mailcore.MinPassphrase {
				widgets.Warn(win.Content(), title, "Choose a passphrase of at least 8 characters.", nil)
				return
			}
			if p != again.Text {
				widgets.Warn(win.Content(), title, "The two passphrases are not the same.", nil)
				return
			}
		} else if p == "" {
			return
		}
		old := current.Text
		okBtn.SetEnabled(false)
		runAsync(a, func() (any, error) {
			if mode == passUnlock {
				return nil, cli.UnlockSecrets(p)
			}
			return nil, cli.ChangePassphrase(old, p)
		}, func(_ any, err error) {
			if err != nil {
				okBtn.SetEnabled(true)
				widgets.Warn(win.Content(), title, err.Error(), nil)
				return
			}
			win.Close()
			if done != nil {
				done()
			}
		})
	}
	for _, f := range []*widgets.TextField{current, first, again} {
		f.OnSubmit = func(string) { submit() }
	}
	okBtn = newButton(okText, submit)
	okBtn.Primary = true
	cancelText := "Not now"
	if mode == passChange {
		cancelText = "Cancel"
	}
	buttons := widgets.NewButtonBox().AddButton(newButton(cancelText, func() { win.Close() }), widgets.RoleReject)
	if mode == passUnlock {
		buttons.AddButton(newButton("Forgot it…", func() { forgotPassphrase(a, cli, win) }), widgets.RoleHelp)
	}
	buttons.AddButton(okBtn, widgets.RoleAccept)

	// A label centres its lines in the height it is given; the spacer
	// under it keeps the text at the top. The text scrolls when the window
	// is small; the fields and buttons stay in view.
	textCol := widgets.NewColumn(wrapLabel(text))
	textCol.AddFlex(widgets.NewSpacer(), 1)
	explain := widgets.NewScrollView(textCol)
	col := widgets.NewColumn(explain).WithGap(6)
	col.AddFlex(explain, 1)
	for _, f := range fields {
		col.Add(f)
	}
	col.Add(buttons)
	setContent(win, widgets.NewPad(14, col))
	win.SetInitialFocus(focus)
	return win
}

// forgotPassphrase offers to start over: without the passphrase the saved
// secrets cannot be read, so they are deleted; accounts and mail stay.
func forgotPassphrase(a *app.Application, cli *mailcore.Client, win *app.Window) {
	widgets.Confirm(win.Content(), "Forget the saved passwords?",
		"Without the passphrase the saved passwords cannot be read. Starting over deletes them and every sign-in; "+
			"your accounts and mail stay, and each account needs its password again (the Accounts tab in Settings).",
		func(yes bool) {
			if !yes {
				return
			}
			runAsync(a, func() (any, error) { return nil, cli.ResetVault() }, func(_ any, err error) {
				if err != nil {
					widgets.Warn(win.Content(), "Start over", err.Error(), nil)
					return
				}
				widgets.Info(win.Content(), "Passwords forgotten",
					"Enter each account's password again on the Accounts tab in Settings; you will choose where to keep it then.",
					func() { win.Close() })
			})
		})
}

// unlockStore opens the store in use for this run: the encrypted file
// asks for its passphrase, the desktop keyring shows its own prompt.
func unlockStore(a *app.Application, cli *mailcore.Client, st mailcore.SecretsStatus, win *app.Window, then func()) *app.Window {
	if st.Store == mailcore.StoreEncrypted {
		return openPassphrase(a, cli, passUnlock, then)
	}
	runAsync(a, func() (any, error) { return nil, cli.UnlockSecrets("") }, func(_ any, err error) {
		if err != nil {
			if win != nil {
				widgets.Warn(win.Content(), "Keyring", "The desktop keyring stayed locked, so comms-mail cannot connect: "+err.Error(), nil)
			}
			return
		}
		if then != nil {
			then()
		}
	})
	return nil
}

// withVault runs then once the daemon can keep a secret: a place chosen
// (asking first) and unlocked (asking to unlock first). A store that
// keeps no secrets (the demo) runs it at once.
func withVault(a *app.Application, cli *mailcore.Client, win *app.Window, intro string, then func()) {
	st, err := cli.SecretsStatus()
	switch {
	case err != nil || !st.Supported || st.Ready && st.Store != "":
		then()
	case st.Store == "":
		openStoreChooser(a, cli, intro, st, then)
	case st.Locked:
		unlockStore(a, cli, st, win, then)
	default:
		then() // not ready for another reason: the save says why
	}
}

// checkVault is the window starting: a locked store is unlocked, passwords
// readable in mail.json are offered a better place, and a store that
// cannot be reached says why. It asks once per window.
func (s *session) checkVault() {
	if s.vaultAsked || s.cli == nil {
		return
	}
	s.vaultAsked = true
	s.async(func() (any, error) {
		return s.cli.SecretsStatus()
	}, func(v any, err error) {
		if err != nil {
			return
		}
		st := v.(mailcore.SecretsStatus)
		switch {
		case !st.Supported:
		case st.Locked:
			s.promptUnlock()
		case st.Store == "" && st.PlainSecrets:
			openStoreChooser(s.app, s.cli, plainIntro(st), st, func() { s.mark("Passwords moved") })
		case st.Problem != "":
			widgets.Warn(s.win.Content(), "Passwords",
				"comms-mail cannot read your saved passwords from "+mailcore.StoreLabel(st.Store)+": "+st.Problem+
					"\n\nThe Privacy tab in Settings can move them elsewhere once it can reach them again.", nil)
		}
	})
}

// promptUnlock unlocks the store in use, unless the window for it is up.
func (s *session) promptUnlock() {
	if s.unlockWin != nil && !s.unlockWin.Closed() {
		s.unlockWin.Raise()
		return
	}
	st, err := s.cli.SecretsStatus()
	if err != nil || !st.Locked {
		return
	}
	s.unlockWin = unlockStore(s.app, s.cli, st, s.win, func() {
		s.mark("Unlocked")
		s.refreshAll()
	})
}
