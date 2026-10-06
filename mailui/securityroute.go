package mailui

import (
	"strconv"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
	"github.com/codemodify/paintengine2d"
	"github.com/codemodify/uitoolkit/a11y"
	"github.com/codemodify/uitoolkit/layout"
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
)

// The way a message came, drawn as Settings › Security › Educate draws
// it: each server a disc, each hop an arrow down to the next — green with
// TLS, red in the clear, grey inside one organisation or where the server
// did not say — and by it the protocol, the TLS and cipher, and when, how
// long after the hop before (mailcore.RouteHop).

// routeView is the drawing: the hops the headers say, and the last, from
// your mailbox to you, as comms-mail fetches it (fetch, nil for none).
type routeView struct {
	widget.Base
	hops  []mailcore.RouteHop
	fetch *mailcore.ConnectionInfo
}

func newRouteView(hops []mailcore.RouteHop, fetch *mailcore.ConnectionInfo) *routeView {
	v := &routeView{hops: hops, fetch: fetch}
	v.Init(v)
	return v
}

// edges are the arrows: the hops, then the fetch as one more.
func (v *routeView) edges() []mailcore.RouteHop {
	out := append([]mailcore.RouteHop(nil), v.hops...)
	if f := v.fetch; f != nil && len(v.hops) > 0 {
		h := mailcore.RouteHop{With: f.Protocol, Version: f.Version, Cipher: f.Cipher, TLS: mailcore.HopTLS}
		if f.Mode == string(mailcore.TLSPlain) {
			h.TLS = mailcore.HopClear
		}
		out = append(out, h)
	}
	return out
}

// fetchWhen is the line under the fetch's arrow: the server, and the
// certificate it showed.
func (v *routeView) fetchWhen() string {
	f := v.fetch
	s := "comms-mail fetches it from " + f.Server
	switch {
	case f.Live && f.Issuer != "":
		s += ", certified by " + f.Issuer
	case !f.Live && f.Mode == string(mailcore.TLSStartTLS):
		s += " (STARTTLS)"
	}
	return s
}

// The drawing's measures, 1x: a disc's side, the room beside it, and an
// arrow's length — two lines of words beside it.
const (
	hopDisc = 24
	hopGap  = 10
)

func (v *routeView) rowH() float32 {
	return max(style.Dip(v.Look(), hopDisc), v.Look().Font().Height())
}

func (v *routeView) edgeH() float32 { return 2*v.Look().Font().Height() + style.Dip(v.Look(), 10) }

// nodes are the discs: where it started, then each server that took it.
func (v *routeView) nodes() []routeNode {
	if len(v.hops) == 0 {
		return nil
	}
	first := v.hops[0]
	start := routeNode{name: first.From, ip: first.FromIP, person: first.Auth}
	if start.name == "" {
		start.name = "where it started"
	}
	if first.Auth {
		start.name = "the sender's app"
		if first.FromIP != "" {
			start.ip = first.FromIP
		}
	}
	if start.ip == start.name {
		start.ip = ""
	}
	out := []routeNode{start}
	for i, h := range v.hops {
		name := h.By
		if name == "" {
			name = "a server that does not name itself"
		}
		n := routeNode{name: name}
		if i == len(v.hops)-1 {
			n.note = "your mailbox"
		} else if h.Yours && !v.hops[i+1].Yours {
			n.note = "your provider's"
		}
		out = append(out, n)
	}
	if v.fetch != nil {
		out = append(out, routeNode{name: "you, in comms-mail", person: true})
	}
	return out
}

type routeNode struct {
	name, ip, note string
	person         bool
}

func (v *routeView) Measure(c layout.Constraints) paintengine2d.Point {
	w := style.Dip(v.Look(), 300)
	if c.HasMaxW() {
		w = c.MaxW
	}
	n := len(v.nodes())
	h := float32(n)*v.rowH() + float32(max(n-1, 0))*v.edgeH()
	return c.Constrain(paintengine2d.Pt(w, h))
}

func (v *routeView) Arrange(r paintengine2d.Rect) { v.SetBounds(r) }

// hopInk is the colour of a hop's arrow.
func hopInk(lk style.LookAndFeel, h mailcore.RouteHop) paintengine2d.Color {
	in := inksOf(lk)
	switch {
	case h.TLS == mailcore.HopTLS:
		return in.good
	case h.TLS == mailcore.HopClear && !h.Inside:
		return in.bad
	}
	return in.muted
}

// hopWords are the two lines beside a hop's arrow: how, and when.
func hopWords(h mailcore.RouteHop, before time.Time) (how, when string) {
	var parts []string
	if h.With != "" {
		parts = append(parts, h.With)
	}
	switch {
	case h.TLS == mailcore.HopTLS:
		tls := "TLS"
		if h.Version != "" {
			tls = h.Version
		}
		if h.Cipher != "" {
			tls += " " + h.Cipher
		}
		parts = append(parts, tls)
	case h.Inside:
		parts = append(parts, "inside one organisation")
	case h.TLS == mailcore.HopClear:
		parts = append(parts, "in the clear")
	default:
		parts = append(parts, "TLS not noted")
	}
	if h.Auth {
		parts = append(parts, "signed in")
	}
	how = strings.Join(parts, " · ")
	if !h.At.IsZero() {
		when = h.At.Local().Format("Mon 02 Jan 15:04:05")
		if !before.IsZero() {
			when += ", " + laterWords(h.At.Sub(before))
		}
	}
	return how, when
}

// laterWords is how long after the hop before, in words.
func laterWords(d time.Duration) string {
	switch {
	case d < 0:
		return "before the hop before it (a clock is off)"
	case d < time.Second:
		return "at once"
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + " s later"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " min later"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " h later"
	}
	return strconv.Itoa(int(d.Hours()/24)) + " days later"
}

func (v *routeView) Paint(ctx *paintengine2d.Context) {
	lk := v.Look()
	in := inksOf(lk)
	f := lk.Font()
	b := v.LocalBounds()
	d := style.Dip(lk, hopDisc)
	cx := b.Min.X + d/2 + style.Dip(lk, 1)
	tx := b.Min.X + d + style.Dip(lk, hopGap)
	room := b.Max.X - tx
	fit := func(s string) string {
		if f.Advance(s) > room {
			return f.Fit(s, room)
		}
		return s
	}
	y := b.Min.Y
	nodes := v.nodes()
	for i, n := range nodes {
		c := paintengine2d.Pt(cx, y+v.rowH()/2)
		ctx.DrawCircle(c, d/2, paintengine2d.Fill(in.disc))
		ctx.DrawCircle(c, d/2, paintengine2d.StrokePaint(in.discEdge, style.Dip(lk, 1.25)))
		p := pictServer
		if n.person {
			p = pictPerson
		}
		sz := d * 0.56
		drawPict(ctx, lk, p, paintengine2d.XYWH(c.X-sz/2, c.Y-sz/2, sz, sz), in.text)
		ty := c.Y - f.Height()/2
		name := fit(n.name)
		f.Draw(ctx, name, paintengine2d.Pt(tx, ty), in.text)
		x := tx + f.Advance(name)
		for _, extra := range []string{n.ip, n.note} {
			if extra == "" {
				continue
			}
			s := "  " + extra
			if x+f.Advance(s) > b.Max.X {
				break
			}
			f.Draw(ctx, s, paintengine2d.Pt(x, ty), in.muted)
			x += f.Advance(s)
		}
		y += v.rowH()
		if i == len(nodes)-1 {
			break
		}
		h := v.edges()[i]
		ink := hopInk(lk, h)
		a, z := paintengine2d.Pt(cx, y+style.Dip(lk, 2)), paintengine2d.Pt(cx, y+v.edgeH()-style.Dip(lk, 2))
		if h.Inside && h.TLS != mailcore.HopTLS {
			pen := paintengine2d.StrokePaint(ink, style.Dip(lk, 1.75))
			pen.Stroke.Dash = []float32{style.Dip(lk, 4), style.Dip(lk, 3)}
			ctx.DrawLine(a, z.Sub(paintengine2d.Pt(0, style.Dip(lk, 4))), pen)
			drawArrow(ctx, lk, z.Sub(paintengine2d.Pt(0, style.Dip(lk, 5))), z, ink)
		} else {
			drawArrow(ctx, lk, a, z, ink)
		}
		var before time.Time
		if i > 0 {
			before = v.hops[i-1].At
		}
		how, when := hopWords(h, before)
		if i == len(v.hops) {
			when = v.fetchWhen()
		}
		ly := y + (v.edgeH()-2*f.Height())/2
		howInk := in.text
		if ink == in.bad {
			howInk = in.bad
		}
		f.Draw(ctx, fit(how), paintengine2d.Pt(tx, ly), howInk)
		f.Draw(ctx, fit(when), paintengine2d.Pt(tx, ly+f.Height()), in.muted)
		y += v.edgeH()
	}
}

// Describe says the route for a screen reader: each server, and each hop
// in words.
func (v *routeView) Describe(n *a11y.Node) {
	n.Role = a11y.RoleLabel
	var parts []string
	nodes := v.nodes()
	edges := v.edges()
	for i, nd := range nodes {
		parts = append(parts, nd.name)
		if i < len(edges) {
			how, _ := hopWords(edges[i], time.Time{})
			parts = append(parts, "then, "+how+", to")
		}
	}
	n.Name = "The way it came: " + strings.Join(parts, " ")
}

// routeSummary is the route chip's tone and words: the hops across the
// internet, how many had TLS. ok is false with none to say.
func routeSummary(hops []mailcore.RouteHop) (tone secTone, text string, ok bool) {
	var across, tls, clear int
	for _, h := range hops {
		if h.Inside {
			continue
		}
		across++
		switch h.TLS {
		case mailcore.HopTLS:
			tls++
		case mailcore.HopClear:
			clear++
		}
	}
	switch {
	case across == 0:
		return secNeutral, "", false
	case clear > 0:
		return secWarn, pluralize(clear, "hop") + " in the clear", true
	case tls == across:
		return secGood, "TLS on every hop", true
	}
	return secNeutral, "TLS on " + strconv.Itoa(tls) + " of " + strconv.Itoa(across) + " hops", true
}

// routeRows are the Way it came section: the drawing, and what can be
// believed of it.
func routeRows(r mailcore.SecurityReport) []widget.Component {
	if len(r.Route) == 0 {
		return []widget.Component{iconLine(style.IconInfo, "No server wrote down how it took it")}
	}
	rows := []widget.Component{newRouteView(r.Route, r.Fetched)}
	theirs := 0
	known := false
	for _, h := range r.Route {
		if h.Yours {
			known = true
		} else {
			theirs++
		}
	}
	switch {
	case known && theirs > 0:
		rows = append(rows, iconLine(style.IconInfo, "Your provider's servers wrote the last "+pluralize(len(r.Route)-theirs, "hop")+
			"; the first "+pluralize(theirs, "hop")+" came with the message, as the sender's servers — or anyone — wrote them"))
	case !known:
		rows = append(rows, iconLine(style.IconInfo, "Each server wrote its own hop at the top; only those your provider's servers wrote can be believed, and this message has no account to tell them by"))
	}
	for _, h := range r.Route {
		if h.TLS == mailcore.HopTLS && (strings.HasPrefix(h.Version, "TLS 1.0") || strings.HasPrefix(h.Version, "TLS 1.1") || strings.HasPrefix(h.Version, "SSL")) {
			rows = append(rows, iconLine(style.IconWarning, h.By+" took it over "+h.Version+", which is broken and retired"))
		}
	}
	if f := r.Fetched; f != nil && f.Mode == string(mailcore.TLSPlain) {
		rows = append(rows, iconLine(style.IconError, "comms-mail fetches this account's mail in the clear: anyone on the network can read it, and your password — Settings › Accounts"))
	}
	rows = append(rows, iconLine(style.IconInfo, "TLS hides a hop from the network, not from the servers: each one reads the message unless it is encrypted end to end"))
	return rows
}
