package mail

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codemodify/uitoolkit"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// FirstRunPrompt is the empty-account modal copy (Yes / No).
const FirstRunPrompt = "There are no accounts, want to add one?"

// OpenAddAccount opens the IMAP/POP3 / SMTP / OAuth setup window.
func OpenAddAccount(a *app.Application, cli *Client, onSaved func()) (*app.Window, error) {
	win, err := a.NewWindow(platform.WindowOptions{
		Title: "Add account", Width: 600, Height: 720, MinWidth: 460, MinHeight: 520,
	})
	if err != nil {
		return nil, err
	}
	win.SetContent(AddAccountAppOn(a, win, cli, onSaved))
	return win, nil
}

// AddAccountApp is the first-run / File → Add Account form.
func AddAccountApp(win *app.Window, cli *Client, onSaved func()) widget.Component {
	return AddAccountAppOn(nil, win, cli, onSaved)
}

// AddAccountAppOn is AddAccountApp with the Application that owns win.
// Connection probes run on a background goroutine and their results touch
// widgets, so they are applied through a.Post; pass nil (tests, headless)
// to apply them inline.
func AddAccountAppOn(a *app.Application, win *app.Window, cli *Client, onSaved func()) widget.Component {
	name := widgets.NewTextField("", "Display name", nil)
	addr := widgets.NewTextField("", "you@example.com", nil)
	incoming := widgets.NewTextField("imap.example.com:993", "imap.host:993 or pop.host:995", nil)
	smtpHost := widgets.NewTextField("smtp.example.com:587", "smtp.host:587", nil)
	user := widgets.NewTextField("", "Username (defaults to address)", nil)
	pass := widgets.NewPasswordField("Password or app password", nil)
	clientID := widgets.NewTextField("", "OAuth client id (required for Google/Microsoft)", nil)
	clientSecret := widgets.NewPasswordField("OAuth client secret (optional for public clients)", nil)
	inLabel := widgets.NewLabel("IMAP host")
	hint := widgets.NewLabel("Type an email — IMAP is guessed from the domain. Switch to POP3 or use Test connection.")
	oauthNote := widgets.NewLabel(
		"Password is stored in mail.json (mode 0600) — temporary plaintext until a secret store.\n" +
			"OAuth (Google / Microsoft) is IMAP + SMTP only.\n" +
			"Redirect: http://127.0.0.1:<port>/oauth/callback (loopback) or device code.\n" +
			"Or set UITK_MAIL_OAUTH_GOOGLE_CLIENT_ID / UITK_MAIL_OAUTH_MS_CLIENT_ID.\n" +
			"Tokens: encrypted files under the data dir (AES-GCM; secret-tool if present).\n" +
			"Optional: UITK_MAIL_PASS / passEnv still works if the password field is empty.",
	)
	status := widgets.NewStatusBar("IMAP or POP3 · Test connection · typed password or OAuth", ConfigPath(), "v"+uitoolkit.Version)

	var applyingProbe bool
	var detectGen atomic.Uint64
	var proto *widgets.RadioGroup

	selectedProto := func() string {
		if proto != nil && proto.Selected() == 1 {
			return ProtoPOP3
		}
		return ProtoIMAP
	}
	updateIncomingLabel := func() {
		if selectedProto() == ProtoPOP3 {
			inLabel.SetText("POP3 host")
		} else {
			inLabel.SetText("IMAP host")
		}
	}
	applyGuess := func(email string) {
		g := GuessMailHosts(email)
		if selectedProto() == ProtoPOP3 {
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
	applyProbe := func(res ProbeResult) {
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
		if res.Protocol == ProtoPOP3 {
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
		req := ProbeRequest{
			Address:  email,
			User:     strings.TrimSpace(user.Text),
			Password: pass.Text,
			Protocol: selectedProto(),
			Host:     strings.TrimSpace(incoming.Text),
			Auto:     auto,
		}
		if auto {
			req.IMAP = GuessMailHosts(email).IMAP
			req.POP = GuessMailHosts(email).POP
		}
		status.Set(0, "Testing connection…")
		go func() {
			res, err := cli.TestAccount(req)
			if fromDetect != 0 && detectGen.Load() != fromDetect {
				return
			}
			if err != nil {
				res = ProbeResult{Error: err.Error()}
			}
			// applyProbe touches widgets: hand it to the UI goroutine.
			postUI(a, func() { applyProbe(res) })
		}()
	}
	maybeAutoDetect := func() {
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

	proto = widgets.NewRadioGroup([]string{"IMAP", "POP3"}, 0, func(i int) {
		_ = i
		updateIncomingLabel()
		if !applyingProbe {
			if strings.Contains(addr.Text, "@") {
				applyGuess(addr.Text)
			}
		}
	})
	addr.OnChange = func(s string) {
		if strings.Contains(s, "@") {
			applyGuess(s)
			maybeAutoDetect()
		}
	}
	pass.OnChange = func(string) { maybeAutoDetect() }

	savePass := func() {
		host := strings.TrimSpace(incoming.Text)
		cfg := AccountConfig{
			Name:     strings.TrimSpace(name.Text),
			Address:  strings.TrimSpace(addr.Text),
			Protocol: selectedProto(),
			SMTP: ServerConfig{
				Host: strings.TrimSpace(smtpHost.Text),
				User: strings.TrimSpace(user.Text),
				Pass: pass.Text,
			},
		}
		in := ServerConfig{
			Host: host,
			User: strings.TrimSpace(user.Text),
			Pass: pass.Text,
		}
		if cfg.Protocol == ProtoPOP3 {
			cfg.POP = in
		} else {
			cfg.IMAP = in
		}
		acct, err := cli.PutAccount(cfg)
		if err != nil {
			widgets.Warn(win.Content(), "Add account", err.Error(), nil)
			return
		}
		if onSaved != nil {
			onSaved()
		}
		widgets.Info(win.Content(), "Account saved",
			fmt.Sprintf("%s <%s>\nProtocol: %s\n\nPassword is in %s (mode 0600). Fetch to connect.",
				acct.Name, acct.Address, ProtocolLabel(acct), ConfigPath()),
			func() { win.Close() })
	}

	runOAuth := func(provider, flow string) {
		email := strings.TrimSpace(addr.Text)
		if email == "" {
			widgets.Warn(win.Content(), "OAuth", "Enter the email address first.", nil)
			return
		}
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

	google := widgets.NewButton("Sign in with Google", func() { runOAuth("google", "loopback") })
	ms := widgets.NewButton("Sign in with Microsoft", func() { runOAuth("microsoft", "loopback") })
	device := widgets.NewButton("Device code…", func() {
		p := providerForAddress(addr.Text)
		if p == "" {
			p = "google"
		}
		runOAuth(p, "device")
	})
	test := widgets.NewButton("Test connection", func() { runProbe(false, 0) })
	save := widgets.NewButton("Save account", savePass)
	save.Primary = true
	cancel := widgets.NewButton("Cancel", func() { win.Close() })

	form := widgets.NewColumn(
		widgets.NewTitle("Add account"),
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
	// Two rows, so a narrow window still shows Cancel and Save: the sign-in
	// choices, then testing and the dialog's own buttons.
	signIn := widgets.NewRow(google, ms, device).WithGap(8)
	box := widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(save, widgets.RoleAccept)
	actions := widgets.NewRow(test, box).WithGap(8)
	actions.AddFlex(box, 1)
	tools := widgets.NewPad(8, widgets.NewColumn(signIn, actions).WithGap(8))
	chrome := widgets.NewTitleBar("Add account", "IMAP or POP3 · Test connection · typed password or OAuth · mail.json 0600")
	pad := widgets.NewPad(12, form)
	root := widgets.NewColumn(chrome, pad, tools, status).WithGap(0)
	root.AddFlex(pad, 1)
	return root
}

func labeled(title string, field widget.Component) widget.Component {
	return widgets.NewColumn(widgets.NewLabel(title), field).WithGap(2)
}

func confirmRemoveAccount(from widget.Component, acct Account, do func()) {
	if from == nil || acct.ID == "" {
		return
	}
	widgets.Confirm(from, "Remove account?",
		fmt.Sprintf("Remove %s <%s> (%s)?\n\nThis deletes the account from mail.json and the local cache. Messages on the server are not deleted.",
			acct.Name, acct.Address, ProtocolLabel(acct)),
		func(yes bool) {
			if yes && do != nil {
				do()
			}
		})
}
