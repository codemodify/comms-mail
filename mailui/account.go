package mailui

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// FirstRunPrompt is the empty-account modal copy (Yes / No).
const FirstRunPrompt = "There are no accounts, want to add one?"

// OpenAddAccount opens the IMAP/POP3 / SMTP / OAuth setup window.
func OpenAddAccount(a *app.Application, cli *mailcore.Client, onSaved func()) (*app.Window, error) {
	return openAccountForm(a, cli, nil, onSaved)
}

// OpenEditAccount opens the same window on an account's settings, as
// cli.AccountConfig gives them.
func OpenEditAccount(a *app.Application, cli *mailcore.Client, base mailcore.AccountConfig, onSaved func()) (*app.Window, error) {
	return openAccountForm(a, cli, &base, onSaved)
}

func openAccountForm(a *app.Application, cli *mailcore.Client, base *mailcore.AccountConfig, onSaved func()) (*app.Window, error) {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: accountFormTitle(base), Width: 600, Height: 720, MinWidth: 460, MinHeight: 520,
	})
	if err != nil {
		return nil, err
	}
	setContent(win, accountFormOn(a, win, cli, base, onSaved))
	return win, nil
}

func accountFormTitle(base *mailcore.AccountConfig) string {
	if base != nil {
		return "Edit account"
	}
	return "Add account"
}

// AddAccountApp is the first-run / File → Add Account form.
func AddAccountApp(win *app.Window, cli *mailcore.Client, onSaved func()) widget.Component {
	return AddAccountAppOn(nil, win, cli, onSaved)
}

// AddAccountAppOn is AddAccountApp with the Application that owns win.
// Connection probes run on a background goroutine and their results touch
// widgets, so they are applied through a.Post; pass nil (tests, headless)
// to apply them inline.
func AddAccountAppOn(a *app.Application, win *app.Window, cli *mailcore.Client, onSaved func()) widget.Component {
	return accountFormOn(a, win, cli, nil, onSaved)
}

// EditAccountAppOn is the form on base, an account's settings: what it
// does not show (connection security, sign-in, identities) stays as it
// is, and so does a saved password while the password is left empty.
func EditAccountAppOn(a *app.Application, win *app.Window, cli *mailcore.Client, base mailcore.AccountConfig, onSaved func()) widget.Component {
	return accountFormOn(a, win, cli, &base, onSaved)
}

func accountFormOn(a *app.Application, win *app.Window, cli *mailcore.Client, base *mailcore.AccountConfig, onSaved func()) widget.Component {
	editing := base != nil
	title := accountFormTitle(base)
	start := mailcore.AccountConfig{IMAP: mailcore.ServerConfig{Host: "imap.example.com:993"},
		SMTP: mailcore.ServerConfig{Host: "smtp.example.com:587"}}
	passHint := "Password or app password"
	hintText := "Type an email — IMAP is guessed from the domain. Switch to POP3 or use Test connection."
	if editing {
		start = *base
		passHint = "Leave empty to keep the saved password"
		hintText = "Leave the password empty to keep the one saved."
	}
	name := widgets.NewTextField(start.Name, "Display name", nil)
	addr := widgets.NewTextField(start.Address, "you@example.com", nil)
	incoming := widgets.NewTextField(start.Incoming().Host, "imap.host:993 or pop.host:995", nil)
	smtpHost := widgets.NewTextField(start.SMTP.Host, "smtp.host:587", nil)
	user := widgets.NewTextField(start.Incoming().User, "Username (defaults to address)", nil)
	pass := widgets.NewPasswordField(passHint, nil)
	clientID := widgets.NewTextField("", "OAuth client id (required for Google/Microsoft)", nil)
	clientSecret := widgets.NewPasswordField("OAuth client secret (optional for public clients)", nil)
	inLabel := widgets.NewLabel("IMAP host")
	hint := widgets.NewLabel(hintText)
	oauthNote := widgets.NewLabel(
		"Passwords and sign-ins are kept encrypted, locked with your passphrase.\n" +
			"OAuth (Google / Microsoft) is IMAP + SMTP only.\n" +
			"Redirect: http://127.0.0.1:<port>/oauth/callback (loopback) or device code.\n" +
			"Or set UITK_MAIL_OAUTH_GOOGLE_CLIENT_ID / UITK_MAIL_OAUTH_MS_CLIENT_ID.\n" +
			"Optional: UITK_MAIL_PASS / passEnv still works if the password field is empty.",
	)
	status := widgets.NewStatusBar("IMAP or POP3 · Test connection · typed password or OAuth", mailcore.ConfigPath(), "v"+uitoolkit.Version)

	var applyingProbe bool
	var detectGen atomic.Uint64
	var proto *widgets.RadioGroup

	selectedProto := func() string {
		if proto != nil && proto.Selected() == 1 {
			return mailcore.ProtoPOP3
		}
		return mailcore.ProtoIMAP
	}
	updateIncomingLabel := func() {
		if selectedProto() == mailcore.ProtoPOP3 {
			inLabel.SetText("POP3 host")
		} else {
			inLabel.SetText("IMAP host")
		}
	}
	applyGuess := func(email string) {
		g := mailcore.GuessMailHosts(email)
		if selectedProto() == mailcore.ProtoPOP3 {
			if g.POP != "" {
				incoming.SetText(g.POP)
			}
		} else if g.IMAP != "" {
			incoming.SetText(g.IMAP)
		}
		if g.SMTP != "" {
			smtpHost.SetText(g.SMTP)
		}
		msg := "Guessed IMAP " + g.IMAP + " · POP3 " + g.POP + " · SMTP " + g.SMTP
		if g.AuthHint != "" {
			msg += "\n" + g.AuthHint
		}
		if g.Provider != "" {
			msg += "  ·  provider " + g.Provider
		}
		hint.SetText(msg)
		if win != nil {
			win.RequestLayout()
		}
	}
	applyProbe := func(res mailcore.ProbeResult) {
		if !res.OK {
			hint.SetText("Test failed: " + res.Error)
			if res.Detail != "" {
				status.Set(0, res.Detail)
			} else {
				status.Set(0, res.Error)
			}
			if win != nil {
				win.RequestLayout()
			}
			return
		}
		applyingProbe = true
		if res.Protocol == mailcore.ProtoPOP3 {
			proto.Select(1)
		} else {
			proto.Select(0)
		}
		updateIncomingLabel()
		if res.Host != "" {
			incoming.SetText(res.Host)
		}
		applyingProbe = false
		line := fmt.Sprintf("Connected: %s %s (%s)", strings.ToUpper(res.Protocol), res.Host, res.TLSMode)
		hint.SetText(line)
		status.Set(0, line)
		if win != nil {
			win.RequestLayout()
		}
	}
	runProbe := func(auto bool, fromDetect uint64) {
		email := strings.TrimSpace(addr.Text)
		req := mailcore.ProbeRequest{
			Address:  email,
			User:     strings.TrimSpace(user.Text),
			Password: pass.Text,
			Protocol: selectedProto(),
			Host:     strings.TrimSpace(incoming.Text),
			Auto:     auto,
		}
		if auto {
			req.IMAP = mailcore.GuessMailHosts(email).IMAP
			req.POP = mailcore.GuessMailHosts(email).POP
		}
		status.Set(0, "Testing connection…")
		go func() {
			res, err := cli.TestAccount(req)
			if fromDetect != 0 && detectGen.Load() != fromDetect {
				return
			}
			if err != nil {
				res = mailcore.ProbeResult{Error: err.Error()}
			}
			// applyProbe touches widgets: hand it to the UI goroutine.
			postUI(a, func() { applyProbe(res) })
		}()
	}
	maybeAutoDetect := func() {
		if editing {
			return // the hosts are known: Test connection checks them
		}
		email := strings.TrimSpace(addr.Text)
		if !strings.Contains(email, "@") || strings.TrimSpace(pass.Text) == "" {
			return
		}
		gen := detectGen.Add(1)
		time.AfterFunc(700*time.Millisecond, func() {
			if detectGen.Load() != gen {
				return
			}
			postUI(a, func() { runProbe(true, gen) })
		})
	}

	first := 0
	if start.IsPOP3() {
		first = 1
	}
	proto = widgets.NewRadioGroup([]string{"IMAP", "POP3"}, first, func(i int) {
		_ = i
		updateIncomingLabel()
		if applyingProbe {
			return
		}
		// Editing, the account's own server for that protocol, if it has one.
		if editing {
			known := base.IMAP.Host
			if selectedProto() == mailcore.ProtoPOP3 {
				known = base.POP.Host
			}
			if known != "" {
				incoming.SetText(known)
				return
			}
		}
		if strings.Contains(addr.Text, "@") {
			applyGuess(addr.Text)
		}
	})
	updateIncomingLabel()
	addr.OnChange = func(s string) {
		if strings.Contains(s, "@") && !editing {
			applyGuess(s)
			maybeAutoDetect()
		}
	}
	pass.OnChange = func(string) { maybeAutoDetect() }

	formConfig := func() mailcore.AccountConfig {
		if editing {
			return editedAccount(*base, accountEdits{
				Name:     strings.TrimSpace(name.Text),
				Address:  strings.TrimSpace(addr.Text),
				Protocol: selectedProto(),
				Incoming: strings.TrimSpace(incoming.Text),
				SMTP:     strings.TrimSpace(smtpHost.Text),
				User:     strings.TrimSpace(user.Text),
				Password: pass.Text,
			})
		}
		host := strings.TrimSpace(incoming.Text)
		cfg := mailcore.AccountConfig{
			Name:     strings.TrimSpace(name.Text),
			Address:  strings.TrimSpace(addr.Text),
			Protocol: selectedProto(),
			SMTP: mailcore.ServerConfig{
				Host: strings.TrimSpace(smtpHost.Text),
				User: strings.TrimSpace(user.Text),
				Pass: pass.Text,
			},
		}
		in := mailcore.ServerConfig{
			Host: host,
			User: strings.TrimSpace(user.Text),
			Pass: pass.Text,
		}
		if cfg.Protocol == mailcore.ProtoPOP3 {
			cfg.POP = in
		} else {
			cfg.IMAP = in
		}
		return cfg
	}
	savePass := func() {
		cfg := formConfig()
		// The password is kept in the vault: a passphrase is set (or the
		// daemon unlocked) first.
		withVault(a, cli, win, accountIntro, func() {
			acct, err := cli.PutAccount(cfg)
			if err != nil {
				widgets.Warn(win.Content(), title, err.Error(), nil)
				return
			}
			if onSaved != nil {
				onSaved()
			}
			msg := fmt.Sprintf("%s <%s>\nProtocol: %s", acct.Name, acct.Address, mailcore.ProtocolLabel(acct))
			if !editing {
				msg += "\n\nIts password is locked with your passphrase. Fetch to connect."
			}
			widgets.Info(win.Content(), "Account saved", msg, func() { win.Close() })
		})
	}

	var startSignIn func(provider, flow, email string)
	runOAuth := func(provider, flow string) {
		email := strings.TrimSpace(addr.Text)
		if email == "" {
			widgets.Warn(win.Content(), "OAuth", "Enter the email address first.", nil)
			return
		}
		// The sign-in's tokens are kept in the vault: a passphrase is set
		// (or the daemon unlocked) first.
		withVault(a, cli, win, accountIntro, func() { startSignIn(provider, flow, email) })
	}
	startSignIn = func(provider, flow, email string) {
		st, err := cli.StartOAuth(provider, email, strings.TrimSpace(name.Text),
			strings.TrimSpace(clientID.Text), strings.TrimSpace(clientSecret.Text), flow)
		if err != nil {
			widgets.Warn(win.Content(), "OAuth", err.Error()+"\n\nSee docs/mail.md — you must register an app and supply a client id.", nil)
			return
		}
		hint.SetText(st.Message + "\n" + st.AuthURL)
		status.Set(0, "Waiting for "+provider+"…")
		go func() {
			deadline := time.Now().Add(3 * time.Minute)
			for time.Now().Before(deadline) {
				poll, err := cli.PollOAuth(st.SessionID)
				if err != nil {
					return
				}
				if poll.Pending {
					time.Sleep(800 * time.Millisecond)
					continue
				}
				if poll.Error != "" {
					return
				}
				if poll.Done {
					if onSaved != nil {
						onSaved()
					}
					return
				}
			}
		}()
		msg := st.Message
		if st.UserCode != "" {
			msg += "\nUser code: " + st.UserCode
		}
		widgets.Info(win.Content(), "Sign in with "+provider, msg+"\n\n"+st.AuthURL, func() {
			if onSaved != nil {
				onSaved()
			}
		})
	}

	google := newButton("Sign in with Google", func() { runOAuth("google", "loopback") })
	ms := newButton("Sign in with Microsoft", func() { runOAuth("microsoft", "loopback") })
	device := newButton("Device code…", func() {
		p := mailcore.ProviderForAddress(addr.Text)
		if p == "" {
			p = "google"
		}
		runOAuth(p, "device")
	})
	test := newButton("Test connection", func() { runProbe(false, 0) })
	save := newButton("Save account", savePass)
	save.Primary = true
	cancel := newButton("Cancel", func() { win.Close() })

	form := widgets.NewColumn(
		widgets.NewTitle(title),
		labeled("Name", name),
		labeled("Email", addr),
		widgets.NewLabel("Incoming protocol"),
		proto,
		hint,
		widgets.NewColumn(inLabel, incoming).WithGap(2),
		labeled("SMTP host", smtpHost),
		labeled("Username", user),
		labeled("Password", pass),
		widgets.NewLabel("OAuth (Google / Microsoft) — IMAP only; your client id, not ours"),
		labeled("OAuth client id", clientID),
		labeled("OAuth client secret", clientSecret),
		oauthNote,
	).WithGap(6)
	// Three rows, so the narrowest window still shows every button: the
	// sign-in choices, device code and testing, then the dialog's own. The
	// form scrolls; the buttons stay put.
	signIn := foldRow(google, ms)
	box := widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept)
	tools := widgets.NewPad(8, widgets.NewColumn(signIn, foldRow(device, test), box).WithGap(8))
	chrome := widgets.NewTitleBar(title, "IMAP or POP3 · Test connection · typed password or OAuth · mail.json 0600")
	scroll := widgets.NewScrollView(widgets.NewPad(12, form))
	root := widgets.NewColumn(chrome, scroll, tools, status).WithGap(0)
	root.AddFlex(scroll, 1)
	return root
}

// accountEdits are what the account form holds.
type accountEdits struct {
	Name, Address, Protocol, Incoming, SMTP, User, Password string
}

// editedAccount is base with the form's edits. What the form does not show
// stays: connection security (unless the host changed, when its port says
// again), the sign-in method and the identities, whose name and address
// follow the account's. An empty password keeps the saved one, which the
// daemon puts back while the server and user are the same. Sending uses
// the incoming login's password and user when it used them before.
func editedAccount(base mailcore.AccountConfig, e accountEdits) mailcore.AccountConfig {
	cfg := base
	cfg.Name, cfg.Address, cfg.Protocol = e.Name, e.Address, e.Protocol
	server := func(prev mailcore.ServerConfig, host, user, pass string) mailcore.ServerConfig {
		next := prev
		if !strings.EqualFold(strings.TrimSpace(prev.Host), host) {
			next.TLSMode, next.TLS, next.StartTLS = "", nil, nil
		}
		next.Host, next.User, next.Pass = host, user, pass
		return next
	}
	oldIn := base.Incoming()
	if e.Protocol == mailcore.ProtoPOP3 {
		cfg.POP = server(base.POP, e.Incoming, e.User, e.Password)
	} else {
		cfg.IMAP = server(base.IMAP, e.Incoming, e.User, e.Password)
	}
	smtpUser, smtpPass := base.SMTP.User, ""
	if base.SMTP.User == "" || strings.EqualFold(base.SMTP.User, oldIn.User) {
		smtpUser, smtpPass = e.User, e.Password
	}
	cfg.SMTP = server(base.SMTP, e.SMTP, smtpUser, smtpPass)
	cfg.Identities = append([]mailcore.Identity(nil), base.Identities...)
	for i, id := range cfg.Identities {
		if strings.EqualFold(strings.TrimSpace(id.Address), strings.TrimSpace(base.Address)) {
			cfg.Identities[i].Address = e.Address
			if id.Name == base.Name {
				cfg.Identities[i].Name = e.Name
			}
		}
	}
	return cfg
}

func labeled(title string, field widget.Component) widget.Component {
	return widgets.NewColumn(widgets.NewLabel(title), field).WithGap(2)
}

func confirmRemoveAccount(from widget.Component, acct mailcore.Account, do func()) {
	if from == nil || acct.ID == "" {
		return
	}
	widgets.Confirm(from, "Remove account?",
		fmt.Sprintf("Remove %s <%s> (%s)?\n\nThis deletes the account from mail.json and the local cache. Messages on the server are not deleted.",
			acct.Name, acct.Address, mailcore.ProtocolLabel(acct)),
		func(yes bool) {
			if yes && do != nil {
				do()
			}
		})
}
