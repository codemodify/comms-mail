package mailui

import (
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// The passphrase that locks comms-mail's saved passwords and sign-ins
// (mailcore's vault). comms-maild asks for it through the window: once per
// run of the daemon to unlock, once ever to set it, and from Settings →
// Privacy to change it.

type passMode int

const (
	passCreate passMode = iota // set a passphrase; the secrets move under it
	passUnlock                 // unlock for this run of the daemon
	passChange                 // replace the passphrase
)

// What the window says when a passphrase is set: why it is asked now,
// what Protect does, and what Not now leaves.

// forgetText is what a forgotten passphrase costs.
const forgetText = "If you forget the passphrase, the saved passwords cannot be recovered: you would type each account's password again."

// plainIntro is the text for passwords found in plain text: an install
// from before the vault. accounts are whose they are.
func plainIntro(accounts []string) string {
	whose := "your accounts"
	if len(accounts) > 0 {
		whose = strings.Join(accounts, ", ")
	}
	return "comms-mail can now keep your saved passwords encrypted, locked with a passphrase you choose.\n\n" +
		"At the moment the passwords for " + whose + " are stored as readable text in " + mailcore.ConfigPath() +
		", where any program running as you can read them.\n\n" +
		"Protect will:\n" +
		"•  move them into an encrypted file, locked with this passphrase, and take them out of mail.json;\n" +
		"•  ask for the passphrase once each time comms-mail starts, before it connects to your mail.\n\n" +
		forgetText + "\n\n" +
		"Not now leaves everything as it is; you can do this later in Settings › Privacy."
}

// accountIntro is the text before the first password is saved.
const accountIntro = "comms-mail keeps account passwords encrypted, locked with a passphrase you choose, and asks for it once each time it starts. " +
	"Choose it now: this account's password is then saved under it.\n\n" + forgetText

// openPassphrase opens the window for mode. intro (passCreate) says why
// a passphrase is asked for and what setting it does (plainIntro,
// accountIntro); done runs on the UI goroutine once it worked.
func openPassphrase(a *app.Application, cli *mailcore.Client, mode passMode, intro string, done func()) *app.Window {
	title := map[passMode]string{passCreate: "Protect your passwords", passUnlock: "Unlock comms-mail", passChange: "Change passphrase"}[mode]
	height := map[passMode]int{passCreate: 640, passUnlock: 330, passChange: 380}[mode]
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 560, Height: height, MinWidth: 440, MinHeight: 300,
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
	case passCreate:
		text = intro
		fields = append(labelled("Passphrase (at least 8 characters)", first), labelled("Type it again", again)...)
		focus, okText = first, "Protect"
	case passUnlock:
		text = "comms-mail keeps your mail passwords encrypted, locked with your passphrase. Enter it to connect to your mail.\n\n" +
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
		if mode != passUnlock {
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
			switch mode {
			case passCreate:
				return nil, cli.CreateVault(p)
			case passUnlock:
				return nil, cli.UnlockVault(p)
			default:
				return nil, cli.ChangePassphrase(old, p)
			}
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
	okBtn = widgets.NewButton(okText, submit)
	okBtn.Primary = true
	cancelText := "Not now"
	if mode == passChange {
		cancelText = "Cancel"
	}
	buttons := widgets.NewButtonBox().AddButton(widgets.NewButton(cancelText, func() { win.Close() }), widgets.RoleReject)
	if mode == passUnlock {
		buttons.AddButton(widgets.NewButton("Forgot it…", func() { forgotPassphrase(a, cli, win) }), widgets.RoleHelp)
	}
	buttons.AddButton(okBtn, widgets.RoleAccept)

	// The explanation scrolls when the window is small; the fields and
	// buttons stay in view.
	// A label centres its lines in the height it is given; the spacer
	// under it keeps the text at the top.
	textCol := widgets.NewColumn(wrapLabel(text))
	textCol.AddFlex(widgets.NewSpacer(), 1)
	explain := widgets.NewScrollView(textCol)
	col := widgets.NewColumn(explain).WithGap(6)
	col.AddFlex(explain, 1)
	for _, f := range fields {
		col.Add(f)
	}
	col.Add(buttons)
	win.SetContent(widgets.NewPad(14, col))
	win.SetInitialFocus(focus)
	return win
}

// forgotPassphrase offers to start over: without the passphrase the saved
// secrets cannot be read, so they are deleted; accounts and mail stay.
func forgotPassphrase(a *app.Application, cli *mailcore.Client, win *app.Window) {
	widgets.Confirm(win.Content(), "Forget the saved passwords?",
		"Without the passphrase the saved passwords cannot be read. Starting over deletes them and every sign-in; "+
			"your accounts and mail stay, and each account needs its password again (Settings › Accounts).",
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
					"Enter each account's password again in Settings › Accounts; you will choose a new passphrase then.",
					func() { win.Close() })
			})
		})
}

// withVault runs then once the daemon can keep a secret: a passphrase set
// (asking for one first) and unlocked (asking to unlock first). A store
// that keeps no secrets (the demo) runs it at once.
func withVault(a *app.Application, cli *mailcore.Client, reason string, then func()) {
	st, err := cli.VaultStatus()
	switch {
	case err != nil || !st.Supported || (st.Exists && st.Unlocked):
		then()
	case !st.Exists:
		openPassphrase(a, cli, passCreate, reason, then)
	default:
		openPassphrase(a, cli, passUnlock, "", then)
	}
}

// checkVault is the window starting: a locked daemon is offered its
// passphrase, and secrets still in plain text a passphrase to lock them.
// It asks once per window.
func (s *session) checkVault() {
	if s.vaultAsked || s.cli == nil {
		return
	}
	s.vaultAsked = true
	s.async(func() (any, error) {
		return s.cli.VaultStatus()
	}, func(v any, err error) {
		if err != nil {
			return
		}
		st := v.(mailcore.VaultStatus)
		switch {
		case !st.Supported:
		case st.Exists && !st.Unlocked:
			s.promptUnlock()
		case !st.Exists && st.PlainSecrets:
			openPassphrase(s.app, s.cli, passCreate, plainIntro(st.PlainAccounts), func() { s.mark("Passwords locked with your passphrase") })
		}
	})
}

// promptUnlock asks for the passphrase, unless the window for it is up.
func (s *session) promptUnlock() {
	if s.unlockWin != nil && !s.unlockWin.Closed() {
		s.unlockWin.Raise()
		return
	}
	s.unlockWin = openPassphrase(s.app, s.cli, passUnlock, "", func() {
		s.mark("Unlocked")
		s.refreshAll()
	})
}
