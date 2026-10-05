package mailui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/uitoolkit/app"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// keysSection is Settings › Security › Keys: OpenPGP and S/MIME, one at a
// time. For each, where its keys are — the places the passwords can be,
// chosen apart from them: in Secret Vault, secretvault keeps them and does
// the work, never letting comms-mail hold a private key; anywhere else,
// comms-mail does it — then your keys for each address you send from,
// and, with comms-mail's own, other people's keys it knows. refresh asks
// again.
func keysSection(a *app.Application, cli *mailcore.Client) (section widget.Component, refresh func()) {
	formats := []string{mailcore.FormatOpenPGP, mailcore.FormatSMIME}
	cur := 0
	body := widgets.NewColumn().WithGap(12)
	tabs := widgets.NewSegmented([]string{"OpenPGP", "S/MIME"}, 0, nil)
	type data struct {
		st   mailcore.SecretsStatus
		view mailcore.KeysView
	}
	refresh = func() {
		format := formats[cur]
		runAsync(a, func() (any, error) {
			st, err := cli.SecretsStatus()
			if err != nil {
				return nil, err
			}
			v, err := cli.KeysView(format)
			return data{st, v}, err
		}, func(v any, err error) {
			body.ClearChildren()
			if err != nil {
				body.Add(wrapLabel("Could not ask comms-maild: " + err.Error()))
			} else {
				d := v.(data)
				k := &keysPage{a: a, cli: cli, format: format, st: d.st, view: d.view, refresh: refresh, from: body}
				if d.st.Supported {
					body.Add(k.placeChooser())
					body.Add(widgets.NewSeparator())
				}
				body.Add(k.yourKeys())
				if d.view.Place.Engine == mailcore.EngineOwn {
					body.Add(widgets.NewSeparator())
					body.Add(k.othersKeys())
				}
			}
			body.RequestLayout()
			body.Invalidate()
		})
	}
	tabs.OnChange = func(i int) {
		cur = i
		refresh()
	}
	refresh()
	return widgets.NewColumn(tabs, body).WithGap(12), refresh
}

// keysPage is one format's page.
type keysPage struct {
	a       *app.Application
	cli     *mailcore.Client
	format  string
	st      mailcore.SecretsStatus
	view    mailcore.KeysView
	refresh func()
	from    widget.Component // where warnings show
}

func (k *keysPage) name() string {
	if k.format == mailcore.FormatSMIME {
		return "S/MIME"
	}
	return "OpenPGP"
}

func (k *keysPage) own() bool { return k.view.Place.Engine == mailcore.EngineOwn }

// placeChooser is where the format's keys are: Secret Vault, where
// secretvault keeps them and does the work, or a place of comms-mail's,
// where it does. Apply makes it so.
func (k *keysPage) placeChooser() widget.Component {
	place := k.view.Place
	st := k.st
	st.Store, st.SecretVault = place.Store, place.Vault
	st.PlainFile = k.st.KeysFile
	st.Ready, st.Locked, st.Problem = place.Ready, place.Locked, place.Problem
	under := map[string]widget.Component{}
	var extra []widget.Component
	if place.Locked || place.Problem != "" {
		text, icon := "Locked now.", style.IconLock
		if !place.Locked {
			text, icon = "It cannot be read now: "+place.Problem, style.IconWarning
		}
		label := "Unlock…"
		if place.Store == mailcore.StoreSecretVault {
			label = "Unlock secretvault…"
		}
		extra = append(extra, iconLine(icon, text), foldRow(newButton(label, func() { k.unlock() })))
	}
	if place.Store == mailcore.StoreEncrypted {
		extra = append(extra, foldRow(newButton("Change passphrase…", func() { openPassphrase(k.a, k.cli, passChange, k.refresh) })))
	}
	if len(extra) > 0 {
		under[place.Store] = widgets.NewColumn(extra...).WithGap(6)
	}
	places := newStoreChoices(st, k.format, true, under)
	apply := newButton("Apply", nil)
	apply.Tip = "Keep the " + k.name() + " keys where picked"
	apply.SetEnabled(false)
	places.onPick = func() { apply.SetEnabled(places.moving()) }
	apply.OnClick = func() {
		if !places.moving() {
			return
		}
		kind := places.opts[places.chosen].kind
		p, problem := places.passphrase()
		if problem != "" {
			widgets.Warn(k.from, "Passphrase", problem, nil)
			return
		}
		vault := ""
		if kind == mailcore.StoreSecretVault {
			vault = places.vault()
		}
		use := func() {
			apply.SetEnabled(false)
			runAsync(k.a, func() (any, error) {
				return nil, k.cli.UseKeys(k.format, kind, vault, []byte(p))
			}, func(_ any, err error) {
				if err != nil {
					apply.SetEnabled(true)
					widgets.Warn(k.from, k.name(), err.Error(), nil)
					return
				}
				k.refresh()
			})
		}
		if title, text := k.leftBehind(kind, vault); text != "" {
			widgets.Confirm(k.from, title, text, func(yes bool) {
				if yes {
					use()
				}
			})
			return
		}
		use()
	}
	return widgets.NewColumn(places.list, places.passBox, foldRow(apply)).WithGap(10)
}

// leftBehind is what to ask before the keys' place changes to kind (and
// vault) when that leaves keys in secretvault behind: keys in secretvault
// never leave it, and it cannot yet move them from one of its vaults to
// another (asked of it: BACKLOG.md). Nothing is lost — choosing the vault
// again finds them. "" when nothing is left behind.
func (k *keysPage) leftBehind(kind, vault string) (title, text string) {
	place := k.view.Place
	if place.Engine != mailcore.EngineSecretVault {
		return "", ""
	}
	n := 0
	for _, ak := range k.view.Addresses {
		if ak.PGP != nil {
			n++
		}
		n += len(ak.SMIME)
	}
	if n == 0 {
		return "", ""
	}
	named := func(v string) string {
		if v == "" {
			v = k.st.SecretVaultDefault
		}
		return v
	}
	from, to := named(place.Vault), named(vault)
	if kind == mailcore.StoreSecretVault && (from == to || place.Vault == vault) {
		return "", "" // the same vault, by its name
	}
	what, stay := "Your "+k.name()+" key stays", "it"
	if k.format == mailcore.FormatSMIME {
		what = "Your S/MIME certificate stays"
	}
	if n > 1 {
		what, stay = "Your "+strconv.Itoa(n)+" "+k.name()+" keys stay", "them"
		if k.format == mailcore.FormatSMIME {
			what = "Your " + strconv.Itoa(n) + " S/MIME certificates stay"
		}
	}
	in := "secretvault’s vault “" + from + "”"
	if from == "" {
		in = "secretvault’s default vault"
	}
	if kind == mailcore.StoreSecretVault {
		target := "“" + to + "”"
		if to == "" {
			target = "the default vault"
		}
		return "Leave the keys behind?",
			what + " in " + in + ": secretvault cannot move keys from one of its vaults to another yet. " +
				"Choosing " + quoteVault(from) + " again finds " + stay + ". Use " + target + " anyway?"
	}
	return "Leave the keys behind?",
		what + " in " + in + ": keys kept by secretvault never leave it. comms-mail uses keys of its own there instead. " +
			"Choosing Secret Vault and " + quoteVault(from) + " again finds " + stay + ". Use " + mailcore.StoreLabel(kind) + " anyway?"
}

// quoteVault is a vault's name in quotes, or "its default vault".
func quoteVault(v string) string {
	if v == "" {
		return "its default vault"
	}
	return "“" + v + "”"
}

// unlock opens where comms-mail keeps the format's keys: the encrypted file
// with its passphrase, the keyring and secretvault with their own prompts.
func (k *keysPage) unlock() {
	if k.view.Place.Store == mailcore.StoreEncrypted {
		openUnlockKeys(k.a, k.cli, k.format, k.refresh)
		return
	}
	runAsync(k.a, func() (any, error) { return nil, k.cli.UnlockKeys(k.format, nil) }, func(_ any, err error) {
		if err != nil {
			widgets.Warn(k.from, k.name(), "It stayed locked: "+err.Error(), nil)
		}
		k.refresh()
	})
}

// yourKeys are your keys for each address you send from, and what can be
// done with them.
func (k *keysPage) yourKeys() widget.Component {
	v := k.view
	col := widgets.NewColumn(widgets.NewTitle("Your keys")).WithGap(10)
	switch {
	case v.Locked:
		col.Add(wrapLabel("secretvault is locked, so your keys cannot be shown."))
		unlock := newButton("Unlock secretvault…", nil)
		unlock.OnClick = func() {
			runAsync(k.a, func() (any, error) { return nil, k.cli.UnlockKeys(k.format, nil) }, func(_ any, err error) {
				if err != nil {
					widgets.Warn(unlock, "secretvault", "secretvault stayed locked: "+err.Error(), nil)
					return
				}
				k.refresh()
			})
		}
		col.Add(foldRow(unlock))
		return col
	case !v.Available || v.Why != "":
		col.Add(wrapLabel(v.Why))
		return col
	case len(v.Addresses) == 0:
		col.Add(wrapLabel("Add an account first: keys are made for the addresses you send from."))
		return col
	}
	for _, ak := range v.Addresses {
		col.Add(k.addressKeys(ak))
	}
	var global []widget.Component
	if k.format == mailcore.FormatSMIME {
		imp := newButton("Import S/MIME…", nil)
		imp.Icon = style.IconOpen
		imp.Tip = "Bring in a certificate and its key from a .p12 or .pfx file"
		imp.OnClick = func() { k.importP12(imp) }
		global = append(global, imp)
	} else if k.own() {
		imp := newButton("Import a key…", nil)
		imp.Icon = style.IconOpen
		imp.Tip = "Bring in an OpenPGP key from a file: yours (its private half), or someone else's"
		imp.OnClick = func() { k.importFile(imp) }
		global = append(global, imp)
	}
	if len(global) > 0 {
		col.Add(foldRow(global...))
	}
	return col
}

// addressKeys is one address: its key or certificates, and what can be
// done.
func (k *keysPage) addressKeys(ak mailcore.AddressKeys) widget.Component {
	col := widgets.NewColumn(widgets.NewLabel(ak.Address)).WithGap(3)
	line := func(icon style.ToolIcon, text string) { col.Add(iconLine(icon, text)) }
	var buttons []widget.Component
	if k.format == mailcore.FormatOpenPGP {
		if key := ak.PGP; key != nil {
			text := "OpenPGP key " + groupFingerprint(key.Fingerprint)
			if !key.Created.IsZero() {
				text += ", made " + key.Created.Format("2 Jan 2006")
			}
			if !key.Expires.IsZero() {
				text += ", until " + key.Expires.Format("2 Jan 2006")
			}
			line(style.IconLock, text)
			if key.PublicKey != "" {
				cp := newButton("Copy public key", nil)
				cp.Icon = style.IconCopy
				cp.Tip = "Copy your public key, to give to the people who write to you"
				cp.OnClick = func() {
					platform.ClipboardSet(key.PublicKey)
					cp.Text = "Copied"
					cp.Invalidate()
				}
				buttons = append(buttons, cp)
			}
			if k.own() {
				buttons = append(buttons, k.backupButton(key.Fingerprint), k.removeButton(key.Fingerprint, "your OpenPGP key "+groupFingerprint(key.Fingerprint)))
			}
		} else {
			line(style.IconInfo, "No OpenPGP key")
			mk := newButton("Make an OpenPGP key", nil)
			mk.Icon = style.IconPlus
			mk.Tip = "Make a key for this address and keep it"
			if !k.own() {
				mk.Tip = "secretvault makes a key for this address and keeps it"
			}
			mk.OnClick = func() {
				k.work(mk, func() error {
					_, err := k.cli.MakePGPKey(ak.Address)
					return err
				})
			}
			buttons = append(buttons, mk)
		}
	} else {
		for _, c := range ak.SMIME {
			text := "S/MIME certificate from " + certName(c.Issuer)
			if !c.NotAfter.IsZero() {
				text += ", valid until " + c.NotAfter.Format("2 Jan 2006")
			}
			line(style.IconLock, text)
			for _, w := range c.Warnings {
				line(style.IconWarning, w)
			}
			if k.own() {
				buttons = append(buttons, k.backupButton(c.SHA256), k.removeButton(c.SHA256, "your S/MIME certificate from "+certName(c.Issuer)))
			}
		}
		if len(ak.SMIME) == 0 {
			line(style.IconInfo, "No S/MIME certificate")
		}
	}
	if len(buttons) > 0 {
		col.Add(foldRow(buttons...))
	}
	return col
}

// work runs do with btn off, then asks again; a failure is said.
func (k *keysPage) work(btn *widgets.Button, do func() error) {
	btn.SetEnabled(false)
	runAsync(k.a, func() (any, error) { return nil, do() }, func(_ any, err error) {
		btn.SetEnabled(true)
		if err != nil {
			widgets.Warn(k.from, k.name(), err.Error(), nil)
			return
		}
		k.refresh()
	})
}

// backupButton saves your key id, private half and all, locked with a
// passphrase, to a file you choose.
func (k *keysPage) backupButton(id string) *widgets.Button {
	b := newButton("Save a backup…", nil)
	b.Icon = style.IconSave
	b.Tip = "Save your key, locked with a passphrase, to keep somewhere safe: without it, encrypted mail cannot be opened again if this computer is lost"
	b.OnClick = func() {
		askSecret(b, "Save a backup", "The backup is locked with a passphrase you choose now. Keep both safe, apart.", true, func(pass []byte) {
			runAsync(k.a, func() (any, error) {
				data, name, err := k.cli.KeyBackup(k.format, id, pass)
				return [2]any{data, name}, err
			}, func(v any, err error) {
				if err != nil {
					widgets.Warn(k.from, "Save a backup", err.Error(), nil)
					return
				}
				r := v.([2]any)
				data, name := r[0].([]byte), r[1].(string)
				home, _ := os.UserHomeDir()
				widgets.ShowFileDialog(k.from, widgets.FileDialogOptions{
					Title: "Save the backup", Mode: widgets.FileSave, Path: home, Name: name,
					OnPick: func(path string) {
						defer clear(data)
						if err := os.WriteFile(path, data, 0o600); err != nil {
							widgets.Warn(k.from, "Save a backup", err.Error(), nil)
						}
					},
					OnCancel: func() { clear(data) },
				})
			})
		})
	}
	return b
}

// removeButton forgets one of your keys, for good, once you say so.
func (k *keysPage) removeButton(id, what string) *widgets.Button {
	b := newButton("Remove…", nil)
	b.Icon = style.IconTrash
	b.OnClick = func() {
		widgets.Confirm(k.from, "Remove "+what+"?",
			"Its private half is deleted. Mail encrypted to it can no longer be opened, unless you have a backup.",
			func(yes bool) {
				if yes {
					k.work(b, func() error { return k.cli.RemoveKey(k.format, id, true) })
				}
			})
	}
	return b
}

// importP12 picks a .p12 / .pfx file and asks for its password.
func (k *keysPage) importP12(from *widgets.Button) {
	home, _ := os.UserHomeDir()
	widgets.ShowFileDialog(from, widgets.FileDialogOptions{
		Title: "Choose your certificate (.p12, .pfx)", Mode: widgets.FileOpen, Path: home,
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
				k.work(from, func() error {
					defer clear(data)
					_, err := k.cli.ImportSMIME(data, password)
					return err
				})
			}, func() { clear(data) })
		},
	})
}

// importFile picks a file of keys or certificates and brings them in,
// asking for a passphrase when one opens them.
func (k *keysPage) importFile(from *widgets.Button) {
	home, _ := os.UserHomeDir()
	widgets.ShowFileDialog(from, widgets.FileDialogOptions{
		Title: "Choose a file of " + k.name() + " keys", Mode: widgets.FileOpen, Path: home,
		OnPick: func(path string) {
			if strings.TrimSpace(path) == "" {
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				widgets.Warn(from, "Import", err.Error(), nil)
				return
			}
			k.importData(from, filepath.Base(path), data, nil)
		},
	})
}

// importData brings in data, asking for the passphrase that opens it when
// it needs one.
func (k *keysPage) importData(from *widgets.Button, name string, data, passphrase []byte) {
	from.SetEnabled(false)
	runAsync(k.a, func() (any, error) {
		return k.cli.ImportKeys(k.format, data, passphrase)
	}, func(v any, err error) {
		from.SetEnabled(true)
		if err != nil && strings.Contains(err.Error(), mailcore.ErrPassphraseNeeded.Error()) {
			askSecret(from, "Import", "The key in "+name+" is locked with a passphrase:", false, func(p []byte) {
				k.importData(from, name, data, p)
			})
			return
		}
		clear(data)
		if err != nil {
			widgets.Warn(from, "Import", err.Error(), nil)
			return
		}
		if n, _ := v.(int); n == 0 {
			widgets.Warn(from, "Import", "There was nothing in "+name+" to bring in.", nil)
		}
		k.refresh()
	})
}

// othersKeys are other people's keys comms-mail knows: how each came,
// and bringing more in or taking one out.
func (k *keysPage) othersKeys() widget.Component {
	type row struct{ addr, name, how, seen, id string }
	var rows []row
	for _, c := range k.view.Contacts {
		how := map[string]string{"autocrypt": "Autocrypt", "attached": "attached", "signed": "signed mail", "imported": "imported by you"}[c.Source]
		for _, a := range c.Addresses {
			rows = append(rows, row{a, c.Name, how, c.LastSeen.Format("2 Jan 2006"), c.ID})
		}
	}
	col := widgets.NewColumn(widgets.NewTitle("Other people's keys")).WithGap(8)
	if len(rows) == 0 {
		col.Add(wrapLabel("None yet. Keys come with signed mail and Autocrypt headers, or bring them in from a file."))
	}
	var table *widgets.TableView
	remove := newButton("Remove", nil)
	if len(rows) > 0 {
		table = widgets.NewTableView([]widgets.TableColumn{
			{Title: "Address", Sortable: true}, {Title: "Name", Width: 120}, {Title: "Came", Width: 110}, {Title: "Last seen", Width: 100},
		}, len(rows), func(r, c int) string {
			if r < 0 || r >= len(rows) {
				return ""
			}
			return [4]string{rows[r].addr, rows[r].name, rows[r].how, rows[r].seen}[c]
		}, nil)
		table.Selected = 0
		col.Add(widgets.NewHeightBox(float32(min(len(rows), 6)+1)*28+4, table))
	}
	remove.SetEnabled(len(rows) > 0)
	remove.OnClick = func() {
		if table == nil || table.Selected < 0 || table.Selected >= len(rows) {
			return
		}
		r := rows[table.Selected]
		k.work(remove, func() error { return k.cli.RemoveKey(k.format, r.id, false) })
	}
	imp := newButton("Import…", nil)
	imp.Icon = style.IconOpen
	imp.Tip = "Bring in someone's public key or certificate from a file"
	imp.OnClick = func() { k.importFile(imp) }
	col.Add(foldRow(imp, remove))
	return col
}

// askSecret asks for a passphrase in a field that holds bytes — typed
// twice when twice is set — and hands a copy to done, which the caller
// wipes; the fields wipe their own.
func askSecret(from widget.Component, title, text string, twice bool, done func([]byte)) {
	first := widgets.NewSecretField("Passphrase")
	again := widgets.NewSecretField("Type it again")
	var ov *widgets.Overlay
	finish := func() {
		first.Wipe()
		again.Wipe()
		widget.DismissOverlay(ov)
	}
	ok := newButton("OK", nil)
	ok.Primary = true
	ok.OnClick = func() {
		p := first.Bytes()
		if twice {
			q := again.Bytes()
			same := string(p) == string(q)
			clear(q)
			if !same {
				clear(p)
				widgets.Warn(from, title, "The two passphrases are not the same.", nil)
				return
			}
		}
		finish()
		done(p)
	}
	cancel := newButton("Cancel", func() { finish() })
	buttons := widgets.NewButtonBox().AddButton(cancel, widgets.RoleReject).AddButton(ok, widgets.RoleAccept)
	col := widgets.NewColumn(wrapLabel(text), first).WithGap(8).WithPad(4)
	if twice {
		col.Add(again)
	}
	col.Add(buttons)
	card := widgets.NewPanel(title, col)
	card.Window = true
	card.Raised = true
	card.OnClose = finish
	ov = widgets.NewOverlay(card)
	ov.Modal = true
	ov.MinCardW = 380
	ov.InitialFocus = first
	widget.ShowOverlay(from, ov)
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
