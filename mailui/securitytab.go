package mailui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/a11y"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/platform"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// The reading pane's say on a message's security, in two places: a row of
// chips under From — the sender, the signature, the encryption at a
// glance, green, amber or red — and the Security tab, with all there is:
// who checked the sender and what each check found, what the servers that
// forwarded it saw (ARC), the domains that signed it (DKIM), its
// signatures and encryption (security.go). A chip opens the tab at its
// section.

// secTone is what a chip means: nothing either way, good, worth a look,
// worth stopping for.
type secTone int

const (
	secNeutral secTone = iota
	secGood
	secWarn
	secBad
)

// ink is the tone's colour in lk.
func (t secTone) ink(lk style.LookAndFeel) paintengine2d.Color {
	p := lk.Palette()
	switch t {
	case secGood:
		return p.SuccessInk()
	case secWarn:
		return p.WarningInk()
	case secBad:
		return p.DangerInk()
	}
	return p.TextMuted
}

// The Security tab's sections, which the chips open it at.
const (
	secSectionSender  = "sender"
	secSectionRoute   = "route"
	secSectionForward = "forward"
	secSectionDKIM    = "dkim"
	secSectionCrypto  = "crypto"
)

// secChip is one chip: a mark and a few words in a capsule of its tone; a
// click, or Return or Space, opens its section.
type secChip struct {
	widget.Base
	Text    string
	Icon    style.ToolIcon
	Tone    secTone
	Section string
	Tip     string
	OnClick func()
	down    bool
}

func newSecChip(tone secTone, icon style.ToolIcon, text, section string) *secChip {
	c := &secChip{Text: text, Icon: icon, Tone: tone, Section: section}
	c.Init(c)
	c.SetWantsFocus(true)
	c.SetFocusVisibleOnly(true)
	return c
}

func (c *secChip) pad() float32 { return style.Dip(c.Look(), 6) }

// iconSize is the mark's side: a little under the text's height.
func (c *secChip) iconSize() float32 { return c.Look().Font().Height() * 0.85 }

func (c *secChip) Measure(k layout.Constraints) paintengine2d.Point {
	f := c.Look().Font()
	h := f.Height() + style.Dip(c.Look(), 8)
	w := 2*c.pad() + c.iconSize() + style.Dip(c.Look(), 4) + f.Advance(c.Text)
	return k.Constrain(paintengine2d.Pt(float32(int(w+0.999)), h))
}

func (c *secChip) Arrange(r paintengine2d.Rect) { c.SetBounds(r) }

func (c *secChip) Paint(ctx *paintengine2d.Context) {
	lk := c.Look()
	b := c.LocalBounds()
	ink := c.Tone.ink(lk)
	alpha := float32(0.12)
	if c.Hovered() || c.down {
		alpha = 0.22
	}
	rad := b.Dy() / 2
	ctx.DrawRoundRect(b, rad, rad, paintengine2d.Fill(ink.WithAlpha(alpha)))
	ctx.DrawRoundRect(b.Inset(0.5), rad, rad, paintengine2d.StrokePaint(ink.WithAlpha(0.7), 1))
	f := lk.Font()
	sz := c.iconSize()
	x := b.Min.X + c.pad()
	style.DrawToolIcon(ctx, paintengine2d.XYWH(x, b.Min.Y+(b.Dy()-sz)/2, sz, sz), c.Icon, ink, style.IconSetOf(lk))
	x += sz + style.Dip(lk, 4)
	f.Draw(ctx, c.Text, paintengine2d.Pt(x, b.Min.Y+(b.Dy()-f.Height())/2), lk.Palette().Text)
	if c.State()&style.StateFocused != 0 {
		lk.DrawFocusRing(ctx, b)
	}
}

func (c *secChip) MousePress(e widget.MouseEvent) bool {
	if e.Button == platform.ButtonRight {
		return false
	}
	c.MarkPointerFocus()
	c.down = true
	c.Invalidate()
	return true
}

func (c *secChip) MouseRelease(e widget.MouseEvent) bool {
	was := c.down
	c.down = false
	c.Invalidate()
	if was && c.LocalBounds().Contains(e.Pos) {
		c.fire()
	}
	return true
}

func (c *secChip) KeyPress(e widget.KeyEvent) bool {
	c.MarkKeyboardFocus()
	if e.Key == platform.KeyReturn || e.Key == platform.KeySpace {
		c.fire()
		return true
	}
	return false
}

func (c *secChip) fire() {
	if c.OnClick != nil {
		c.OnClick()
	}
}

func (c *secChip) Describe(n *a11y.Node) {
	n.Role = a11y.RoleButton
	n.Name = c.Text
	n.Description = "Shows the Security tab"
	if c.Tip != "" {
		n.Description = c.Tip
	}
	n.Actions = n.Actions.With(a11y.ActionDefault)
}

func (c *secChip) AccessibleAction(_ int, a a11y.Action) bool {
	if a != a11y.ActionDefault {
		return false
	}
	c.fire()
	return true
}

// securityView is the chips and the tab: rebuilt from the report and what
// the signatures and encryption came to, as each comes in.
type securityView struct {
	chips  *widgets.Wrap
	scroll *widgets.ScrollView
	body   *widgets.FlexBox
	// sections are the tab's, by name; crypto is the lines security.go
	// keeps (securityPart.lines), shown in its section.
	sections map[string]widget.Component
	crypto   widget.Component
	// report and sec are what has come in for the message showing.
	report *mailcore.SecurityReport
	sec    *mailcore.MessageSecurity
	// open selects the tab; jump is the section to show once it is laid
	// out. lookup looks the sender's domain up, looking while it does.
	open    func()
	jump    string
	lookup  func(domain string)
	looking bool
	box     *jumpBox
}

func newSecurityView(crypto widget.Component) *securityView {
	v := &securityView{crypto: crypto, sections: map[string]widget.Component{}}
	v.chips = widgets.NewWrap()
	v.chips.Gap, v.chips.LineGap = 5, 4
	v.chips.SetVisible(false)
	v.body = widgets.NewColumn().WithGap(14)
	v.box = newJumpBox(v)
	v.scroll = widgets.NewScrollView(widgets.NewPad(10, v.box))
	return v
}

// clear is no message.
func (v *securityView) clear() {
	v.report, v.sec, v.jump, v.looking = nil, nil, "", false
	v.chips.ClearChildren()
	v.chips.SetVisible(false)
	v.body.ClearChildren()
	v.box.changed()
	v.scroll.ScrollTo(0)
}

// jumpBox is the tab's sections, and scrolls the tab to the one a chip
// asked for once they are laid out.
//
// It is measured over and over — the reading pane's splitter asks how
// narrow the tab can be on every layout, and the tab view measures it
// besides — and a message with many links has hundreds of lines to wrap
// each time: switching messages crawled while the tab was showing. So it
// answers how narrow it can be itself, keeps what it measured until its
// sections change (or the font does), and lays them out again only when
// its size changes, not when it scrolls.
type jumpBox struct {
	widget.Base
	v        *securityView
	measured map[layout.Constraints]paintengine2d.Point
	fontH    float32
	laid     paintengine2d.Point // the size the sections were laid out at
}

// changed is new sections: what was measured and laid out is gone.
func (b *jumpBox) changed() {
	b.measured, b.laid = nil, paintengine2d.Point{}
}

// MinWidth is as narrow as the tab's lines are let wrap.
func (b *jumpBox) MinWidth() float32 { return style.Dip(b.Look(), 200) }

func newJumpBox(v *securityView) *jumpBox {
	b := &jumpBox{v: v}
	b.Init(b)
	b.Add(v.body)
	return b
}

// Measure, unbounded, is at a narrow width: the lines wrap, and the
// reading pane can be as narrow as its other tabs let it.
func (b *jumpBox) Measure(c layout.Constraints) paintengine2d.Point {
	if !c.HasMaxW() {
		c.MaxW = style.Dip(b.Look(), 280)
	}
	if fh := b.Look().Font().Height(); fh != b.fontH {
		b.fontH = fh
		b.changed()
	}
	if p, ok := b.measured[c]; ok {
		return p
	}
	p := b.v.body.Measure(c)
	if b.measured == nil {
		b.measured = map[layout.Constraints]paintengine2d.Point{}
	}
	b.measured[c] = p
	return p
}

func (b *jumpBox) Arrange(r paintengine2d.Rect) {
	b.SetBounds(r)
	if size := paintengine2d.Pt(r.Dx(), r.Dy()); size != b.laid {
		b.laid = size
		b.v.body.Arrange(paintengine2d.Rect{Min: paintengine2d.Pt(0, 0), Max: size})
	}
	if sec := b.v.sections[b.v.jump]; sec != nil && b.v.jump != "" && r.Dy() > 0 {
		b.v.jump = ""
		top := float32(0)
		for p := widget.Component(sec); p != nil && p != widget.Component(b.v.scroll); p = p.Parent() {
			top += p.Bounds().Min.Y
		}
		b.v.scroll.ScrollTo(b.v.scroll.OffsetY + top - style.Dip(b.Look(), 4))
	}
}

// show rebuilds the chips and the tab for m.
func (v *securityView) show(m mailcore.Message) {
	v.chips.ClearChildren()
	for _, c := range v.chipsFor(m) {
		c.OnClick = func() {
			v.jump = c.Section
			if v.open != nil {
				v.open()
			}
			v.scroll.RequestLayout()
		}
		v.chips.Add(c)
	}
	v.chips.SetVisible(len(v.chips.Children()) > 0)
	v.chips.RequestLayout()

	v.body.ClearChildren()
	v.box.changed()
	v.sections = map[string]widget.Component{}
	section := func(name, title string, rows ...widget.Component) {
		col := widgets.NewColumn(widgets.NewTitle(title)).WithGap(4)
		for _, r := range rows {
			col.Add(r)
		}
		v.sections[name] = col
		v.body.Add(col)
	}
	if v.report != nil {
		section(secSectionSender, "Who sent it", senderRows(m, *v.report)...)
		section(secSectionRoute, "The way it came", routeRows(*v.report)...)
		if rows := forwardRows(*v.report); len(rows) > 0 {
			section(secSectionForward, "Forwarded on the way", rows...)
		}
		section(secSectionDKIM, "Domain signatures (DKIM)", dkimRows(*v.report)...)
		section(secSectionDomain, "The sender's domain", domainRows(fromDomain(m), v.report.Domain, v.looking, v.lookup)...)
	}
	if c, ok := contentOf(m, v.report, v.sec); ok {
		section(secSectionLinks, "Links", linkRows(c)...)
		section(secSectionRemote, "Images and what it would load", remoteRows(c)...)
		if rows := attachRows(m, c); len(rows) > 0 {
			section(secSectionAttach, "Attachments", rows...)
		}
		if rows := headerRows(m, c); len(rows) > 0 {
			section(secSectionHeaders, "Other headers", rows...)
		}
	}
	if v.crypto.Parent() != nil {
		if p, ok := v.crypto.Parent().(*widgets.FlexBox); ok {
			p.Remove(v.crypto)
		}
	}
	section(secSectionCrypto, "Signature and encryption", append([]widget.Component{v.crypto}, cryptoRows(m, v.sec, v.report)...)...)
	v.body.RequestLayout()
}

// fromDomain is the domain of m's From, as a person reads it.
func fromDomain(m mailcore.Message) string {
	addr := strings.ToLower(mailcore.ExtractAddr(m.From))
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return addr[i+1:]
	}
	return ""
}

// chipsFor are the chips for m, as few as say it: its sender — the
// warnings about it, when there are any — its signature and encryption,
// risky links, tracking images, a form, risky attachments, the TLS of its
// way here, and a weak signature by From's domain. The rest is the tab's.
func (v *securityView) chipsFor(m mailcore.Message) []*secChip {
	var out []*secChip
	add := func(tone secTone, icon style.ToolIcon, text, section string) {
		out = append(out, newSecChip(tone, icon, text, section))
	}
	dom := fromDomain(m)
	if dom == "" {
		dom = "The sender"
	}
	if r := v.report; r != nil {
		switch n := len(r.Sender.Warnings); {
		case n > 0:
			add(secBad, style.IconWarning, pluralize(n, "warning")+" about the sender", secSectionSender)
		case r.Sender.Auth == mailcore.AuthPass:
			add(secGood, style.IconCheck, dom+" confirmed", secSectionSender)
		case r.Sender.Auth == mailcore.AuthFail:
			add(secBad, style.IconError, dom+" not confirmed", secSectionSender)
		case r.Trust == mailcore.AuthByOther:
			add(secWarn, style.IconWarning, "Sender check not your provider's", secSectionSender)
		default:
			add(secNeutral, style.IconInfo, dom+" not checked", secSectionSender)
		}
	}
	switch {
	case !m.Signed && !m.Encrypted:
		add(secNeutral, style.IconInfo, "Not signed or encrypted", secSectionCrypto)
	default:
		if m.Signed || v.sec != nil && len(v.sec.Signatures) > 0 {
			tone, text := signatureChip(v.sec)
			icon := map[secTone]style.ToolIcon{secGood: style.IconCheck, secWarn: style.IconQuestion, secBad: style.IconError, secNeutral: style.IconInfo}[tone]
			add(tone, icon, text, secSectionCrypto)
		} else {
			add(secNeutral, style.IconInfo, "Not signed", secSectionCrypto)
		}
		switch {
		case !m.Encrypted:
			add(secNeutral, style.IconInfo, "Not encrypted", secSectionCrypto)
		case v.sec == nil:
			add(secNeutral, style.IconLock, "Encrypted: opening…", secSectionCrypto)
		case v.sec.Decrypted:
			add(secGood, style.IconLock, "Encrypted", secSectionCrypto)
		default:
			add(secWarn, style.IconLock, "Encrypted, not opened", secSectionCrypto)
		}
	}
	if c, ok := contentOf(m, v.report, v.sec); ok {
		contentChips(c, add)
	}
	if r := v.report; r != nil {
		if tone, text, ok := routeSummary(newRouteView(r.Route, r.Fetched).edges()); ok {
			icon := map[secTone]style.ToolIcon{secGood: style.IconLock, secWarn: style.IconWarning, secNeutral: style.IconInfo}[tone]
			add(tone, icon, text, secSectionRoute)
		}
		for _, d := range r.DKIM {
			// Only From's domain's, which is what vouches for the sender.
			if d.Aligned && d.Result == "pass" && (d.Weak != "" || d.Length > 0) {
				add(secWarn, style.IconWarning, "Weak domain signature", secSectionDKIM)
				break
			}
		}
	}
	// Worst first: what is worth stopping for, then a look, then what
	// holds, then what is simply not there.
	rank := map[secTone]int{secBad: 0, secWarn: 1, secGood: 2, secNeutral: 3}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Tone] < rank[out[j].Tone] })
	return out
}

// signatureChip is the signature chip's tone and words, from what the
// signatures came to (nil: not yet).
func signatureChip(sec *mailcore.MessageSecurity) (secTone, string) {
	switch {
	case sec == nil:
		return secNeutral, "Signed: checking…"
	case len(sec.Signatures) == 0:
		return secNeutral, "Signed, not checked"
	}
	sig := sec.Signatures[0]
	who := sig.Signer
	if who == "" {
		who = "an unknown signer"
	}
	switch sig.Status {
	case "valid":
	case "bad":
		return secBad, "Signature broken"
	case "unknown-key":
		return secWarn, "Signed, unknown key"
	default:
		return secBad, "Signature not trusted"
	}
	switch sig.Trust {
	case "verified":
		return secGood, "Signed by " + who
	case "suspicious":
		return secBad, "Signed with another's key"
	}
	return secWarn, "Signed, key not verified"
}

// forwarders are the servers that passed the message on and said so
// (ARC), other than the one that checked it on arrival.
func forwarders(r mailcore.SecurityReport) []string {
	var out []string
	for _, a := range r.ARC {
		name := a.Server
		if name == "" {
			name = a.Signer
		}
		if name == "" || name == r.Server || a.Signer != "" && containsFold(r.Provider, orgOf(a.Signer)) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// orgOf is d's last two labels: enough to tell a provider's servers by.
func orgOf(d string) string {
	labels := strings.Split(strings.Trim(d, "."), ".")
	if len(labels) <= 2 {
		return d
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// resultIcon is the mark for a check's result.
func resultIcon(result string) style.ToolIcon {
	switch result {
	case "pass":
		return style.IconCheck
	case "fail":
		return style.IconError
	case "softfail", "permerror", "temperror", "policy":
		return style.IconWarning
	}
	return style.IconInfo
}

// senderRows are the Who sent it section: who checked the sender, and
// what each check found, then the sender check's warnings and notes.
func senderRows(m mailcore.Message, r mailcore.SecurityReport) []widget.Component {
	rows := []widget.Component{iconLine(style.IconMail, "From "+m.From)}
	server := r.Server
	if server == "?" {
		server = "a server that does not name itself (Microsoft 365's do not)"
	}
	switch {
	case r.Server == "":
		rows = append(rows, iconLine(style.IconInfo, "No server added a check of the sender to this message"))
	case r.Trust == mailcore.AuthByProvider:
		rows = append(rows, iconLine(style.IconCheck, "Checked by your provider's server, "+server))
	case r.Trust == mailcore.AuthByOther:
		rows = append(rows, iconLine(style.IconWarning, "Checked by "+server+", which is not your provider's: these results came with the message, and prove nothing"))
	default:
		rows = append(rows, iconLine(style.IconInfo, "Checked by "+server+" (imported mail: there is no account to tell your provider by)"))
	}
	for _, c := range r.Checks {
		rows = append(rows, iconLine(resultIcon(c.Result), checkWords(c)))
	}
	if r.Sender.Auth == mailcore.AuthPass {
		rows = append(rows, iconLine(style.IconCheck, "So it comes from "+fromDomain(m)+": the domain, not the person — a signature proves the person"))
	}
	for _, w := range r.Sender.Warnings {
		rows = append(rows, iconLine(style.IconWarning, w))
	}
	for _, n := range r.Sender.Notes {
		rows = append(rows, iconLine(style.IconInfo, n))
	}
	return rows
}

// checkWords says one check in a few words: the method, its result, what
// that means, and whether the domain it checked is From's.
func checkWords(c mailcore.AuthCheck) string {
	d := c.Domain
	if d == "" {
		d = "the domain"
	}
	result := strings.ToUpper(c.Method) + " " + c.Result
	unchecked := result + ": it could not be checked"
	var says string
	switch c.Method {
	case "spf":
		says = map[string]string{
			"pass":     "sent from a server " + d + " allows",
			"fail":     "sent from a server " + d + " does not allow",
			"softfail": d + " says this server probably may not send for it",
			"neutral":  d + " says nothing either way",
			"none":     d + " lists no servers",
		}[c.Result]
	case "dkim":
		says = map[string]string{
			"pass": "signed by " + d,
			"fail": "the signature by " + d + " does not hold: changed on the way, or forged",
			"none": "no domain signature",
		}[c.Result]
		if c.Selector != "" && c.Result == "pass" {
			says += " (key " + c.Selector + ")"
		}
	case "dmarc":
		says = map[string]string{
			"pass": d + "'s policy is met",
			"fail": d + "'s policy is not met",
			"none": d + " publishes no policy",
		}[c.Result]
		if p := policyWords(c.Policy); p != "" && c.Result != "none" {
			says += "; it asks to " + p
		}
		if c.Disposition != "" && c.Disposition != "none" && c.Result == "fail" {
			says += "; the server did: " + c.Disposition
		}
	case "arc":
		says = map[string]string{
			"pass": "the chain of servers that forwarded it holds",
			"fail": "the chain of servers that forwarded it is broken",
			"none": "not forwarded with ARC",
		}[c.Result]
	case "compauth":
		says = "Microsoft's overall verdict"
		if c.Reason != "" {
			says += " (reason " + c.Reason + ")"
		}
	case "iprev":
		says = map[string]string{
			"pass": "the sending server's address names it back",
			"fail": "the sending server's name and address do not match",
		}[c.Result]
		if c.Address != "" && says != "" {
			says += " (" + c.Address + ")"
		}
	case "bimi":
		says = map[string]string{
			"pass": d + " has a verified logo",
		}[c.Result]
	case "auth":
		if c.Result == "pass" {
			says = "sent by someone signed in to the server"
			if c.User != "" {
				says += " (" + c.User + ")"
			}
		}
	}
	if says == "" {
		if c.Result == "temperror" || c.Result == "permerror" {
			return unchecked
		}
		return result
	}
	out := result + ": " + says
	if c.Domain != "" && (c.Method == "spf" || c.Method == "dkim") && c.Result == "pass" {
		if c.Aligned {
			out += " — From's domain"
		} else {
			out += " — not From's domain"
		}
	}
	return out
}

// policyWords is what a DMARC policy asks of mail that fails it.
func policyWords(p string) string {
	switch p {
	case "reject":
		return "reject what fails"
	case "quarantine":
		return "put what fails in spam"
	case "none":
		return "only report what fails"
	}
	return ""
}

// forwardRows are the Forwarded on the way section: what each server
// that passed the message on saw, as it said (ARC).
func forwardRows(r mailcore.SecurityReport) []widget.Component {
	var rows []widget.Component
	by := map[string]bool{}
	for _, f := range forwarders(r) {
		by[f] = true
	}
	for _, a := range r.ARC {
		name := a.Server
		if name == "" {
			name = a.Signer
		}
		if !by[name] {
			continue
		}
		var saw []string
		for _, c := range a.Checks {
			if c.Method == "arc" {
				continue
			}
			s := strings.ToUpper(c.Method) + " " + c.Result
			if c.Domain != "" {
				s += " for " + c.Domain
			}
			saw = append(saw, s)
		}
		text := strconv.Itoa(a.Instance) + ". " + name
		if len(saw) > 0 {
			text += " saw " + strings.Join(saw, ", ")
		}
		icon := style.IconForward
		if a.Seal == "fail" {
			icon = style.IconWarning
			text += "; the chain was already broken when it got it"
		}
		rows = append(rows, iconLine(icon, text))
	}
	if len(rows) > 0 {
		rows = append(rows, iconLine(style.IconInfo, "A forwarder or a mailing list changes a message on the way, which can break its checks; what it saw on arrival is said here, signed by it"))
	}
	return rows
}

// dkimRows are the Domain signatures section: each DKIM-Signature, what
// the server found of it, and what makes one weak.
func dkimRows(r mailcore.SecurityReport) []widget.Component {
	if len(r.DKIM) == 0 {
		return []widget.Component{iconLine(style.IconInfo, "No domain signed it")}
	}
	var rows []widget.Component
	for _, d := range r.DKIM {
		parts := []string{d.Domain}
		if d.Selector != "" {
			parts = append(parts, "key "+d.Selector)
		}
		if d.Algorithm != "" {
			parts = append(parts, d.Algorithm)
		}
		if d.Aligned {
			parts = append(parts, "From's domain")
		} else {
			parts = append(parts, "not From's domain")
		}
		if !d.Signed.IsZero() {
			parts = append(parts, "signed "+d.Signed.Local().Format("02 Jan 2006 15:04"))
		}
		icon, held := resultIcon(d.Result), ""
		switch d.Result {
		case "pass":
			held = "holds"
		case "fail":
			held = "does not hold"
		case "":
			icon, held = style.IconInfo, "not checked by your server"
		default:
			held = d.Result
		}
		rows = append(rows, iconLine(icon, strings.Join(parts, " · ")+": "+held))
		if d.Weak != "" {
			rows = append(rows, iconLine(style.IconWarning, "Its algorithm uses "+d.Weak))
		}
		if d.Length > 0 {
			rows = append(rows, iconLine(style.IconWarning, "It signs only the first "+strconv.Itoa(d.Length)+" bytes of the text: more can be added after them, and it still holds"))
		}
		if !d.Expires.IsZero() && d.Expires.Before(time.Now()) {
			rows = append(rows, iconLine(style.IconInfo, "It stopped holding on "+d.Expires.Local().Format("02 Jan 2006")))
		}
	}
	return rows
}

// cryptoRows are the Signature and encryption section's lines besides
// security.go's: each signature's key, what checked it, and what is not
// done and what follows.
func cryptoRows(m mailcore.Message, sec *mailcore.MessageSecurity, r *mailcore.SecurityReport) []widget.Component {
	var rows []widget.Component
	if sec != nil {
		for _, sig := range sec.Signatures {
			rows = append(rows, signatureDetailRows(sig)...)
		}
		for _, enc := range sec.Encryptions {
			rows = append(rows, encryptionRows(enc)...)
		}
		switch sec.Engine {
		case mailcore.EngineSecretVault:
			vault := sec.Vault
			if vault == "" {
				vault = "its default vault"
			} else {
				vault = "the vault " + vault
			}
			rows = append(rows, iconLine(style.IconInfo, "Checked by secretvault, with "+vault))
		case mailcore.EngineOwn:
			rows = append(rows, iconLine(style.IconInfo, "Checked by comms-mail, with the keys in Settings › Security › Keys"))
		}
		if sec.Subject != "" {
			rows = append(rows, iconLine(style.IconInfo, "The subject shown is the one inside the encryption"))
		}
	}
	if !m.Signed {
		rows = append(rows, iconLine(style.IconInfo, "Not signed: nothing proves who wrote it"))
	}
	if !m.Encrypted {
		rows = append(rows, iconLine(style.IconInfo, "Not encrypted: each server on its way could read it, and both providers keep it readable"))
	}
	if m.Autocrypt || r != nil && r.Autocrypt {
		rows = append(rows, iconLine(style.IconInfo, "It carries the sender's OpenPGP key (Autocrypt), kept for writing back encrypted"))
	}
	return rows
}

// day is a date as the tab says it.
func day(t time.Time) string { return t.Local().Format("02 Jan 2006") }

// signatureDetailRows are what is known of how a signature was made: its
// key, when, with what, and an S/MIME signer's certificate.
func signatureDetailRows(sig mailcore.SignatureCheck) []widget.Component {
	var rows []widget.Component
	format := "OpenPGP"
	if sig.Format == "smime" {
		format = "S/MIME"
	}
	if sig.Fingerprint != "" {
		key := format + " key " + groupFingerprint(sig.Fingerprint)
		if sig.KeyBits > 0 {
			key += ", " + strconv.Itoa(sig.KeyBits) + "-bit"
		}
		if !sig.KeyCreated.IsZero() {
			key += ", made " + day(sig.KeyCreated)
		}
		if !sig.KeyExpires.IsZero() {
			key += ", expires " + day(sig.KeyExpires)
		}
		rows = append(rows, iconLine(style.IconLock, key))
	}
	if !sig.SignedAt.IsZero() || sig.Hash != "" {
		made := "Signed"
		if !sig.SignedAt.IsZero() {
			made += " " + sig.SignedAt.Local().Format("Mon 02 Jan 2006 15:04")
		}
		switch {
		case sig.Hash != "" && sig.Algorithm != "":
			made += " with " + sig.Hash + " and " + sig.Algorithm
		case sig.Hash != "":
			made += " with " + sig.Hash
		}
		rows = append(rows, iconLine(style.IconInfo, made))
	}
	if h := strings.ToUpper(strings.ReplaceAll(sig.Hash, "-", "")); h == "SHA1" || h == "MD5" {
		rows = append(rows, iconLine(style.IconWarning, sig.Hash+" is broken: a signature made with it can be forged"))
	}
	if strings.HasPrefix(strings.ToUpper(sig.Algorithm), "RSA") && sig.KeyBits > 0 && sig.KeyBits < 2048 {
		rows = append(rows, iconLine(style.IconWarning, "A "+strconv.Itoa(sig.KeyBits)+"-bit RSA key is too short to be safe"))
	}
	if !sig.KeyExpires.IsZero() && sig.KeyExpires.Before(time.Now()) {
		rows = append(rows, iconLine(style.IconWarning, "The key expired on "+day(sig.KeyExpires)))
	}
	if sig.Issuer != "" {
		cert := "Certificate from " + certName(sig.Issuer)
		if !sig.NotBefore.IsZero() && !sig.NotAfter.IsZero() {
			cert += ", valid " + day(sig.NotBefore) + " to " + day(sig.NotAfter)
		}
		rows = append(rows, iconLine(style.IconInfo, cert))
		if !sig.NotAfter.IsZero() && sig.NotAfter.Before(time.Now()) {
			rows = append(rows, iconLine(style.IconWarning, "The certificate expired on "+day(sig.NotAfter)))
		}
	}
	if len(sig.Chain) > 0 {
		var names []string
		for _, c := range sig.Chain {
			names = append(names, certName(c))
		}
		rows = append(rows, iconLine(style.IconInfo, "Chain: "+strings.Join(names, " → ")))
	}
	return rows
}

// encryptionRows are whom an encrypted layer is encrypted to — and which
// of them opened it — and with what.
func encryptionRows(enc mailcore.EncryptionCheck) []widget.Component {
	var rows []widget.Component
	if n := len(enc.Recipients); n > 0 {
		var who []string
		for _, r := range enc.Recipients {
			var s string
			switch {
			case r.Passphrase:
				s = "a passphrase"
			case r.KeyID == "hidden":
				s = "a key it does not name"
			case r.Fingerprint != "":
				s = groupFingerprint(r.Fingerprint)
			case r.KeyID != "":
				s = groupFingerprint(r.KeyID)
			case r.Serial != "":
				s = "certificate " + r.Serial
				if r.Issuer != "" {
					s += " from " + certName(r.Issuer)
				}
			case r.SubjectKeyID != "":
				s = "certificate key " + groupFingerprint(r.SubjectKeyID)
			default:
				s = "a key"
			}
			if r.Algorithm != "" {
				s += " (" + r.Algorithm + ")"
			}
			if r.Opened {
				s += ", which opened it"
			}
			who = append(who, s)
		}
		rows = append(rows, iconLine(style.IconLock, "Encrypted to "+pluralize(n, "key")+": "+strings.Join(who, "; ")))
	}
	switch enc.Integrity {
	case "aead":
		cipher := enc.Cipher
		if cipher == "" {
			cipher = "Its cipher"
		}
		rows = append(rows, iconLine(style.IconCheck, cipher+", with authenticated encryption: any change on the way would show"))
	case "mdc":
		rows = append(rows, iconLine(style.IconInfo, strings.TrimSpace(enc.Cipher+" with the older integrity check (MDC): a change would show, but AEAD is the newer way")))
	case "none":
		rows = append(rows, iconLine(style.IconWarning, "Nothing keeps it from being changed on the way: it has no integrity check"))
	default:
		if enc.Cipher != "" {
			rows = append(rows, iconLine(style.IconInfo, "Cipher: "+enc.Cipher))
		}
	}
	return rows
}
