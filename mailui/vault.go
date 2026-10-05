package mailui

import (
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Where comms-mail keeps saved passwords and sign-ins (mailcore's
// secrets.go): the desktop keyring, secretvault, an encrypted file, or a
// plain file. The window asks when it finds passwords readable in
// mail.json and before the first account is saved, unlocks the store at
// start, and Settings › Security › Passwords is the same choice, which
// moves everything to another store.

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
const laterNote = "You can set this up later, or switch to another place at any time, under Security › Passwords in Settings."

// switchIntro is the text for moving the secrets elsewhere.
const switchIntro = "Choose where comms-mail keeps your passwords and sign-ins. They all move there, and the copies where they are now are removed."

// storeOption is one of the places in the chooser: its name, and what it
// is, shown in brackets after it; path is the file it keeps them in, for
// the two that are files; problem why it cannot be used, when it cannot.
type storeOption struct {
	kind, title, what, path, problem string
	available                        bool
}

func storeOptions(st mailcore.SecretsStatus) []storeOption {
	return []storeOption{
		{mailcore.StoreKeyring, "System Keyring", st.KeyringBackend, "", st.KeyringProblem, st.KeyringAvailable},
		{mailcore.StoreSecretVault, "Secret Vault", "", "", st.SecretVaultProblem, st.SecretVaultAvailable},
		{mailcore.StoreEncrypted, "Encrypted file", "Argon2id hashing + AES-256-GCM encryption", st.EncryptedFile, "", true},
		{mailcore.StorePlain, "Plain file", "open to anyone", st.PlainFile, "", true},
	}
}

// pathView is a file's path as text that can be selected and copied, not
// edited, all of it in view.
func pathView(path string) widget.Component { return newFitText(path) }

// fitText is a read-only text view as tall as its text wrapped to the width
// it is given: a TextArea is MinRows tall whatever it holds, and scrolls
// the rest.
type fitText struct {
	widget.Base
	view *widgets.TextArea
}

func newFitText(text string) *fitText {
	f := &fitText{view: widgets.NewTextView(text, "")}
	f.view.MinRows = 1
	f.Init(f)
	f.Add(f.view)
	return f
}

func (f *fitText) Measure(c layout.Constraints) paintengine2d.Point {
	// The view wraps at the width it is laid out at: laid out at this one,
	// with room for every line, it says how many there are. With no width
	// to keep to, the text is one line.
	f.view.MinRows = 1
	if c.HasMaxW() {
		f.view.Arrange(paintengine2d.XYWH(0, 0, c.MaxW, 1<<14))
		f.view.MinRows = max(1, len(f.view.Lines()))
		// A measure can come after the layout (a parent probing how
		// narrow it can go): the view goes back to the box it was given.
		b := f.Bounds()
		f.view.Arrange(paintengine2d.XYWH(0, 0, b.Dx(), b.Dy()))
	}
	return f.view.Measure(c)
}

func (f *fitText) Arrange(r paintengine2d.Rect) {
	f.SetBounds(r)
	f.view.Arrange(paintengine2d.XYWH(0, 0, r.Dx(), r.Dy()))
}

// storeChoices is the choice of where secrets are kept — the passwords',
// or comms-mail's own keys of a format (purpose) — each place with what it
// means, and the passphrase fields while the encrypted file is picked to
// move to: a new passphrase, typed twice; or, when the file already keeps
// someone else's secrets, its own passphrase if it is not open yet, and
// nothing if it is. The window that asks picks nothing beforehand and
// cannot pick the place in use; Settings shows the place in use picked,
// with under[kind] beneath it.
type storeChoices struct {
	st      mailcore.SecretsStatus
	purpose string
	// shared: the encrypted file keeps another purpose's secrets.
	shared  bool
	opts    []storeOption
	radios  []*widgets.RadioButton
	chosen  int // -1: none
	list    *widgets.FlexBox
	passBox *widgets.FlexBox
	first   *widgets.TextField
	again   *widgets.TextField
	// vaultPick is secretvault's vault: its default (0) or the one named
	// in vaultName (1).
	vaultPick *widgets.RadioGroup
	vaultName *widgets.TextField
	// onPick runs after each pick.
	onPick func()
}

func newStoreChoices(st mailcore.SecretsStatus, purpose string, showInUse bool, under map[string]widget.Component) *storeChoices {
	c := &storeChoices{st: st, purpose: purpose, opts: storeOptions(st), chosen: -1}
	if purpose != "passwords" {
		// For keys, Secret Vault is also who does the work.
		for i := range c.opts {
			if c.opts[i].kind == mailcore.StoreSecretVault {
				c.opts[i].what = "keeps the keys and does the work"
			}
		}
	}
	for _, u := range st.EncryptedUsers {
		if u != purpose {
			c.shared = true
		}
	}
	c.first = widgets.NewPasswordField("", nil)
	c.again = widgets.NewPasswordField("", nil)
	switch {
	case c.shared && st.EncryptedOpen:
		c.passBox = widgets.NewColumn(wrapLabel("The encrypted file is open: they are added to it.")).WithGap(6)
	case c.shared:
		c.passBox = widgets.NewColumn(
			wrapLabel("The encrypted file already keeps other secrets: they are added to it, locked with its passphrase."),
			widgets.NewLabel("The encrypted file's passphrase"), c.first,
		).WithGap(6)
	default:
		c.passBox = widgets.NewColumn(
			wrapLabel(forgetText),
			widgets.NewLabel("Passphrase (at least 8 characters)"), c.first,
			widgets.NewLabel("Type it again"), c.again,
		).WithGap(6)
	}
	c.passBox.SetVisible(false)
	c.radios = make([]*widgets.RadioButton, len(c.opts))
	c.list = widgets.NewColumn().WithGap(10)
	for i, o := range c.opts {
		rb := widgets.NewRadio(o.title, false, nil)
		rb.OnChange = func(on bool) {
			if on {
				c.pick(i)
			}
		}
		inUse := o.kind == st.Store
		rb.SetEnabled(o.available && (!inUse || showInUse))
		c.radios[i] = rb
		// A file's path is its first row.
		details := widgets.NewColumn().WithGap(6)
		if o.path != "" {
			details.Add(pathView(o.path))
		}
		if !o.available && o.problem != "" {
			details.Add(iconLine(style.IconWarning, "Not available: "+o.problem))
		}
		if o.kind == mailcore.StoreSecretVault {
			details.Add(c.vaultChoice(i, o.available))
		}
		if extra := under[o.kind]; extra != nil {
			details.Add(extra)
		}
		// The name, what it is in brackets — wrapping, where the window
		// is narrow — and the place in use's green check at the end.
		head := widgets.NewRow(rb).WithGap(2).WithAlign(layout.AlignCenter)
		var what widget.Component = widgets.NewSpacer()
		if o.what != "" {
			what = wrapLabel("(" + o.what + ")")
		}
		head.AddFlex(what, 1)
		if inUse {
			head.Add(inUseMark())
		}
		c.list.Add(widgets.NewColumn(head, details.WithPadding(28, 0, 0, 0)).WithGap(2))
		if inUse && showInUse {
			rb.Selected = true
			c.chosen = i
		}
	}
	return c
}

// inUseMark is the green check after the place in use: a mark, in the
// look's own success colour.
func inUseMark() *widgets.Label {
	l := widgets.NewIconLabel(style.IconCheck, "")
	l.Tone = widgets.ToneSuccess
	return l
}

// vaultChoice is which of secretvault's vaults: its default, named when
// known, or one named here. Choosing either picks Secret Vault (option
// i).
func (c *storeChoices) vaultChoice(i int, available bool) widget.Component {
	def := "Default"
	if c.st.SecretVaultDefault != "" {
		def += " (" + c.st.SecretVaultDefault + ")"
	}
	sel := 0
	if c.st.SecretVault != "" {
		sel = 1
	}
	c.vaultName = widgets.NewTextField(c.st.SecretVault, "Vault name", nil)
	c.vaultName.SetEnabled(available && sel == 1)
	changed := func() {
		if c.chosen != i {
			c.radios[i].SetSelected(true) // picks it
		} else if c.onPick != nil {
			c.onPick()
		}
	}
	c.vaultPick = widgets.NewRadioGroup([]string{def, "Custom"}, sel, func(n int) {
		c.vaultName.SetEnabled(n == 1)
		changed()
	})
	c.vaultName.OnInput = func(string) { changed() }
	for _, rb := range c.vaultPick.Buttons() {
		rb.SetEnabled(available)
	}
	hint := wrapLabel("A vault secretvault does not have yet, it offers to make: it asks you, and you choose its passphrase there.")
	hint.Tone = widgets.ToneMuted
	return widgets.NewColumn(c.vaultPick, c.vaultName, hint).WithGap(6)
}

// vault is the secretvault vault chosen: "" for its default.
func (c *storeChoices) vault() string {
	if c.vaultPick == nil || c.vaultPick.Selected() != 1 {
		return ""
	}
	return strings.TrimSpace(c.vaultName.Text)
}

// customUnnamed is Custom chosen with no name typed.
func (c *storeChoices) customUnnamed() bool {
	return c.vaultPick != nil && c.vaultPick.Selected() == 1 && c.vault() == ""
}

// pick makes option i the choice.
func (c *storeChoices) pick(i int) {
	c.chosen = i
	for j, other := range c.radios {
		if j != i && other.Selected {
			other.SetSelected(false)
		}
	}
	c.passBox.SetVisible(c.moving() && c.opts[i].kind == mailcore.StoreEncrypted)
	c.passBox.RequestLayout()
	if c.onPick != nil {
		c.onPick()
	}
}

// moving says a place other than the one in use is picked: another
// store, or with secretvault another of its vaults.
func (c *storeChoices) moving() bool {
	if c.chosen < 0 {
		return false
	}
	kind := c.opts[c.chosen].kind
	if kind == mailcore.StoreSecretVault && c.customUnnamed() {
		return false // a name first
	}
	if kind != c.st.Store {
		return true
	}
	return kind == mailcore.StoreSecretVault && c.vault() != c.st.SecretVault
}

// passphrase is what the passphrase fields hold for the place picked, or
// what is wrong with it.
func (c *storeChoices) passphrase() (string, string) {
	if c.opts[c.chosen].kind != mailcore.StoreEncrypted {
		return "", ""
	}
	p := c.first.Text
	switch {
	case c.shared && c.st.EncryptedOpen:
		return "", ""
	case c.shared:
		if p == "" {
			return "", "Enter the encrypted file's passphrase."
		}
		return p, ""
	case len([]rune(p)) < mailcore.MinPassphrase:
		return "", "Choose a passphrase of at least 8 characters."
	case p != c.again.Text:
		return "", "The two passphrases are not the same."
	}
	return p, ""
}

// move takes the secrets to the place picked: the passwords'; see
// moveWith.
func (c *storeChoices) move(a *app.Application, cli *mailcore.Client, from widget.Component, btn *widgets.Button, done func()) {
	c.moveWith(a, from, btn, func(kind, p, vault string) error { return cli.UseStoreIn(kind, p, vault) }, done)
}

// moveWith has use take the secrets to the place picked, with its
// passphrase and vault. btn is off while it works; done runs on the UI
// goroutine once they have moved. Warnings show over from.
func (c *storeChoices) moveWith(a *app.Application, from widget.Component, btn *widgets.Button, use func(kind, passphrase, vault string) error, done func()) {
	if !c.moving() || !btn.Enabled() {
		return
	}
	kind := c.opts[c.chosen].kind
	p, problem := c.passphrase()
	if problem != "" {
		widgets.Warn(from, "Passphrase", problem, nil)
		return
	}
	vault := ""
	if kind == mailcore.StoreSecretVault {
		vault = c.vault()
	}
	btn.SetEnabled(false)
	runAsync(a, func() (any, error) {
		return nil, use(kind, p, vault)
	}, func(_ any, err error) {
		if err != nil {
			btn.SetEnabled(true)
			widgets.Warn(from, "Keep passwords", err.Error(), nil)
			return
		}
		if done != nil {
			done()
		}
	})
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
	c := newStoreChoices(st, "passwords", false, nil)
	var okBtn *widgets.Button
	submit := func() {
		c.move(a, cli, win.Content(), okBtn, func() {
			win.Close()
			if done != nil {
				done()
			}
		})
	}
	c.onPick = func() { okBtn.SetEnabled(true) }
	c.first.OnSubmit = func(string) { submit() }
	c.again.OnSubmit = func(string) { submit() }
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
	body := widgets.NewColumn(wrapLabel(intro), c.list).WithGap(14)
	body.AddFlex(widgets.NewSpacer(), 1)
	scroll := widgets.NewScrollView(body)
	col := widgets.NewColumn(scroll, c.passBox).WithGap(10)
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
	passUnlock     passMode = iota // unlock the encrypted file for this run
	passChange                     // replace its passphrase
	passUnlockKeys                 // unlock it for comms-mail's own keys of a format
)

// openPassphrase asks for the encrypted file's passphrase: to unlock it,
// or to change it. done runs on the UI goroutine once it worked.
func openPassphrase(a *app.Application, cli *mailcore.Client, mode passMode, done func()) *app.Window {
	return openPassphraseFor(a, cli, mode, "", done)
}

// openUnlockKeys asks for the encrypted file's passphrase, to use
// comms-mail's own keys of format kept in it.
func openUnlockKeys(a *app.Application, cli *mailcore.Client, format string, done func()) *app.Window {
	return openPassphraseFor(a, cli, passUnlockKeys, format, done)
}

func openPassphraseFor(a *app.Application, cli *mailcore.Client, mode passMode, format string, done func()) *app.Window {
	title := map[passMode]string{passUnlock: "Unlock comms-mail", passChange: "Change passphrase", passUnlockKeys: "Unlock your keys"}[mode]
	height := map[passMode]int{passUnlock: 330, passChange: 380, passUnlockKeys: 300}[mode]
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
	case passUnlockKeys:
		text = "comms-mail keeps your own keys in the encrypted file, locked with your passphrase. Enter it to sign, encrypt and open mail with them."
		fields = labelled("Passphrase", first)
		focus, okText = first, "Unlock"
	case passChange:
		text = "Everything the encrypted file keeps is locked again with the new passphrase; the old one stops working."
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
			switch mode {
			case passUnlock:
				return nil, cli.UnlockSecrets(p)
			case passUnlockKeys:
				return nil, cli.UnlockKeys(format, []byte(p))
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
		"Without the passphrase nothing in the encrypted file can be read. Starting over deletes it: the saved passwords, every sign-in, "+
			"and comms-mail's own keys if they are kept there — mail encrypted to those keys can then be opened only from a backup. "+
			"Your accounts and mail stay, and each account needs its password again (the Accounts tab in Settings).",
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
// asks for its passphrase, the desktop keyring and secretvault show their
// own prompts (comms-mail never sees secretvault's passphrase). For
// secretvault it is also how to be asked again after saying no.
func unlockStore(a *app.Application, cli *mailcore.Client, st mailcore.SecretsStatus, win *app.Window, then func()) *app.Window {
	if st.Store == mailcore.StoreEncrypted {
		return openPassphrase(a, cli, passUnlock, then)
	}
	runAsync(a, func() (any, error) { return nil, cli.UnlockSecrets("") }, func(_ any, err error) {
		if err != nil {
			if win != nil {
				title, text := "Keyring", "The desktop keyring stayed locked, so comms-mail cannot connect: "
				if st.Store == mailcore.StoreSecretVault {
					title, text = "secretvault", "secretvault stayed locked, so comms-mail is waiting: "
				}
				widgets.Warn(win.Content(), title, text+err.Error(), nil)
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
		case st.Locked && st.Store == mailcore.StoreSecretVault:
			// comms-mail waits for secretvault rather than asking it to
			// unlock; Fetch, or Settings › Security › Passwords, asks.
			s.mark("Waiting for secretvault to unlock")
		case st.Locked:
			s.promptUnlock()
		case st.Store == "" && st.PlainSecrets:
			openStoreChooser(s.app, s.cli, plainIntro(st), st, func() { s.mark("Passwords moved") })
		case st.Problem != "":
			widgets.Warn(s.win.Content(), "Passwords",
				"comms-mail cannot read your saved passwords from "+mailcore.StoreLabel(st.Store)+": "+st.Problem+
					"\n\nSecurity › Passwords in Settings can move them elsewhere once it can reach them again.", nil)
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
