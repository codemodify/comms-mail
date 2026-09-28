package mailui

import (
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

// Why a passphrase is being set, which the window says first.
const (
	reasonPlain   = "Your mail passwords are saved in plain text, readable by any program running as you. Choose a passphrase to lock them away."
	reasonAccount = "Before comms-mail saves this account's password, choose a passphrase to lock your passwords away."
)

// openPassphrase opens the window for mode. reason (passCreate) says why;
// done runs on the UI goroutine once it worked.
func openPassphrase(a *app.Application, cli *mailcore.Client, mode passMode, reason string, done func()) *app.Window {
	title := map[passMode]string{passCreate: "Protect your passwords", passUnlock: "Unlock comms-mail", passChange: "Change passphrase"}[mode]
	// The heights hold the text wrapped at the narrowest width.
	height := map[passMode]int{passCreate: 360, passUnlock: 260, passChange: 330}[mode]
	win, err := a.NewWindow(platform.WindowOptions{
		Title: title, Width: 480, Height: height, MinWidth: 400, MinHeight: height - 20,
	})
	if err != nil {
		return nil
	}
	current := widgets.NewPasswordField("Current passphrase", nil)
	first := widgets.NewPasswordField("Passphrase", nil)
	again := widgets.NewPasswordField("The same passphrase again", nil)

	var text string
	var fields []widget.Component
	var focus widget.Component
	okText := ""
	switch mode {
	case passCreate:
		text = reason + " comms-mail asks for it once each time it starts.\n\n" +
			"If you forget it, the saved passwords cannot be recovered: each account then needs its password again."
		first.Placeholder = "Passphrase (at least 8 characters)"
		fields = []widget.Component{first, again}
		focus, okText = first, "Protect"
	case passUnlock:
		text = "Your mail passwords are locked. Enter your passphrase to connect to your mail; until then comms-mail shows only what it has already downloaded."
		first.Placeholder = "Passphrase"
		fields = []widget.Component{first}
		focus, okText = first, "Unlock"
	case passChange:
		text = "The saved passwords and sign-ins are locked again with the new passphrase."
		first.Placeholder = "New passphrase (at least 8 characters)"
		again.Placeholder = "The new passphrase again"
		fields = []widget.Component{current, first, again}
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

	col := widgets.NewColumn(wrapLabel(text)).WithGap(8)
	for _, f := range fields {
		col.Add(f)
	}
	col.Add(widgets.NewSpacer())
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
			"your accounts and mail stay, and each account needs its password again (Settings → Accounts).",
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
					"Enter each account's password again in Settings → Accounts; you will choose a new passphrase then.",
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
			openPassphrase(s.app, s.cli, passCreate, reasonPlain, func() { s.mark("Passwords locked with your passphrase") })
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
