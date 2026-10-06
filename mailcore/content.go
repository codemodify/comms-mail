package mailcore

import (
	"mime"
	"net"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Content checks: what a message's own text, HTML and attachments do that
// is worth knowing — links that go elsewhere than they say, or to an
// address, a look-alike or an international domain; the images that
// report you opened it, and every server the HTML would reach; forms;
// attachments that run as programs or hide what they are; a read receipt
// asked for; and what its other headers give away. Nothing here is
// fetched or run: the message is read as it came.

// ContentReport is what the checks found.
type ContentReport struct {
	// Links are the links worth a word, and LinkCount how many there are.
	Links     []LinkIssue `json:"links,omitempty"`
	LinkCount int         `json:"linkCount,omitempty"`
	// Trackers are the organisations whose images are there only to tell
	// them you opened it: one pixel, or hidden. Remote are all those the
	// HTML would load something from.
	Trackers []string `json:"trackers,omitempty"`
	Remote   []string `json:"remote,omitempty"`
	// Forms are where its forms send what is typed in them; Password,
	// that one asks for a password.
	Forms    []string `json:"forms,omitempty"`
	Password bool     `json:"password,omitempty"`
	// Attachments are those worth a word.
	Attachments []AttachmentIssue `json:"attachments,omitempty"`
	// Headers are what its other headers say worth knowing.
	Headers []HeaderNote `json:"headers,omitempty"`
}

// LinkIssue is one link worth a word: what it shows, where it goes, and
// why it is worth one (Kind: LinkElsewhere…).
type LinkIssue struct {
	Text string `json:"text,omitempty"`
	Href string `json:"href"`
	Host string `json:"host,omitempty"`
	Kind string `json:"kind"`
	// Like is the domain a look-alike imitates; Shown the domain the
	// text shows, for a link that goes elsewhere; Unicode an
	// international domain as it reads.
	Like    string `json:"like,omitempty"`
	Shown   string `json:"shown,omitempty"`
	Unicode string `json:"unicode,omitempty"`
}

// Link kinds, worst first.
const (
	LinkScript    = "script"    // runs code: javascript:, vbscript:, data:text/html
	LinkLookAlike = "lookalike" // a domain that imitates one you write to
	LinkElsewhere = "elsewhere" // shows one domain, goes to another
	LinkAddress   = "address"   // to an IP address, not a name
	LinkIDN       = "idn"       // an international domain, which can imitate
	LinkShortener = "shortener" // a short link: where it goes is hidden
)

// AttachmentIssue is one attachment worth a word (Kind: AttachRuns…).
type AttachmentIssue struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Kind string `json:"kind"`
}

// Attachment kinds, worst first.
const (
	AttachRuns     = "runs"     // a program, a script, a disk image: opening it runs it
	AttachHidden   = "hidden"   // its name hides its real ending (two endings, right-to-left)
	AttachMacros   = "macros"   // an Office file that can carry macros
	AttachMismatch = "mismatch" // its type and its name disagree
	AttachArchive  = "archive"  // an archive: what it holds is not checked
)

// HeaderNote is one thing a header says worth knowing (Kind:
// HeaderReceipt…), and its value.
type HeaderNote struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

// Header note kinds.
const (
	HeaderReceipt     = "receipt"     // asks for a read receipt
	HeaderReturnPath  = "returnpath"  // bounces go to another domain
	HeaderMessageID   = "messageid"   // made by another domain's software
	HeaderFuture      = "future"      // dated in the future
	HeaderUnsubscribe = "unsubscribe" // a list: how to leave it
	HeaderOneClick    = "oneclick"    // …in one click (RFC 8058)
)

// shorteners are link shorteners: a short link hides where it goes.
var shorteners = map[string]bool{"bit.ly": true, "tinyurl.com": true, "t.co": true, "goo.gl": true, "ow.ly": true,
	"is.gd": true, "buff.ly": true, "rebrand.ly": true, "cutt.ly": true, "shorturl.at": true, "tiny.cc": true,
	"rb.gy": true, "t.ly": true, "s.id": true, "lnkd.in": true, "bl.ink": true, "short.io": true}

// shownDomain finds a domain in a link's text: "example.com",
// "https://www.example.com/x", "Visit PayPal.com".
var shownDomain = regexp.MustCompile(`(?i)(?:https?://)?((?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24})(?:[/:?#]|\b|$)`)

// plainURL finds links in plain text.
var plainURL = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"')\]]+`)

// CheckContent checks a message's content: its HTML as sent (unsanitised:
// what is taken out for showing is part of what it does), its text, its
// parts, and its header (nil: none). knows says whether you write to a
// domain, for look-alikes; nil knows nobody.
func CheckContent(htmlBody, text string, parts []Part, h mail.Header, known map[string]bool) ContentReport {
	var r ContentReport
	seen := map[string]bool{}
	link := func(href, shown string) {
		r.LinkCount++
		if issue, ok := checkLink(href, shown, known); ok && !seen[issue.Kind+issue.Href] {
			seen[issue.Kind+issue.Href] = true
			r.Links = append(r.Links, issue)
		}
	}
	trackers, remote := map[string]bool{}, map[string]bool{}
	if strings.TrimSpace(htmlBody) != "" {
		if doc, err := html.Parse(strings.NewReader(htmlBody)); err == nil {
			walkHTML(doc, &r, link, trackers, remote)
		}
	} else {
		for _, u := range plainURL.FindAllString(text, 200) {
			link(strings.TrimRight(u, ".,;:!?"), "")
		}
	}
	r.Trackers, r.Remote = sortedKeys(trackers), sortedKeys(remote)
	sort.SliceStable(r.Links, func(i, j int) bool { return linkRank[r.Links[i].Kind] < linkRank[r.Links[j].Kind] })
	for _, p := range parts {
		if strings.TrimSpace(p.Filename) == "" || protocolPart[strings.ToLower(p.MIMEType)] {
			continue
		}
		if kind := attachmentKind(p.Filename, p.MIMEType); kind != "" {
			r.Attachments = append(r.Attachments, AttachmentIssue{Name: p.Filename, Type: p.MIMEType, Kind: kind})
		}
	}
	if h != nil {
		r.Headers = headerNotes(h)
	}
	return r
}

var linkRank = map[string]int{LinkScript: 0, LinkLookAlike: 1, LinkElsewhere: 2, LinkAddress: 3, LinkIDN: 4, LinkShortener: 5}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// walkHTML reads the HTML's links, images, styles and forms.
func walkHTML(n *html.Node, r *ContentReport, link func(href, shown string), trackers, remote map[string]bool) {
	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.A, atom.Area:
			if href := strings.TrimSpace(attr(n, "href")); href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(strings.ToLower(href), "mailto:") {
				link(href, collapseSpace(textOf(n)))
			}
		case atom.Img:
			if host := remoteHost(attr(n, "src")); host != "" {
				remote[host] = true
				if hiddenImage(n) {
					trackers[host] = true
				}
			}
		case atom.Link:
			if host := remoteHost(attr(n, "href")); host != "" {
				remote[host] = true
			}
		case atom.Form:
			action := strings.TrimSpace(attr(n, "action"))
			if action == "" {
				action = "(this message)"
			}
			r.Forms = append(r.Forms, action)
		case atom.Input:
			if strings.EqualFold(attr(n, "type"), "password") {
				r.Password = true
			}
		}
		for _, u := range cssURLs(attr(n, "style")) {
			if host := remoteHost(u); host != "" {
				remote[host] = true
			}
		}
		if n.DataAtom == atom.Style && n.FirstChild != nil {
			for _, u := range cssURLs(n.FirstChild.Data) {
				if host := remoteHost(u); host != "" {
					remote[host] = true
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTML(c, r, link, trackers, remote)
	}
}

// textOf is an element's text.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

var cssURL = regexp.MustCompile(`(?i)url\(\s*['"]?([^'")\s]+)`)

func cssURLs(css string) []string {
	var out []string
	for _, m := range cssURL.FindAllStringSubmatch(css, 50) {
		out = append(out, m[1])
	}
	return out
}

// remoteHost is the organisation an http(s) address reaches; "" for
// anything else (cid:, data:).
func remoteHost(u string) string {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil || p.Hostname() == "" || (p.Scheme != "http" && p.Scheme != "https" && p.Scheme != "") {
		return ""
	}
	if p.Scheme == "" && !strings.HasPrefix(u, "//") {
		return ""
	}
	h := strings.ToLower(p.Hostname())
	if net.ParseIP(h) != nil {
		return h
	}
	return registrableDomain(h)
}

// hiddenImage says an image is there to be loaded, not seen: a pixel, or
// not shown at all.
func hiddenImage(n *html.Node) bool {
	w, h := strings.TrimSuffix(attr(n, "width"), "px"), strings.TrimSuffix(attr(n, "height"), "px")
	if w == "0" || w == "1" || h == "0" || h == "1" {
		return true
	}
	style := strings.ToLower(strings.ReplaceAll(attr(n, "style"), " ", ""))
	for _, s := range []string{"display:none", "visibility:hidden", "width:1px", "height:1px", "width:0", "height:0", "opacity:0"} {
		if strings.Contains(style, s) {
			return true
		}
	}
	return false
}

// checkLink says what is worth a word about a link to href showing
// shown, if anything.
func checkLink(href, shown string, known map[string]bool) (LinkIssue, bool) {
	issue := LinkIssue{Text: shown, Href: href}
	low := strings.ToLower(strings.TrimSpace(href))
	if strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "vbscript:") || strings.HasPrefix(low, "data:text/html") {
		issue.Kind = LinkScript
		return issue, true
	}
	u, err := url.Parse(href)
	if err != nil || u.Hostname() == "" {
		return issue, false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	issue.Host = host
	if net.ParseIP(host) != nil {
		issue.Kind = LinkAddress
		return issue, true
	}
	org := registrableDomain(host)
	if known != nil && !known[org] {
		if like := lookAlike(org, known); like != "" {
			issue.Kind, issue.Like = LinkLookAlike, like
			return issue, true
		}
	}
	if m := shownDomain.FindStringSubmatch(shown); m != nil {
		if s := registrableDomain(strings.ToLower(m[1])); s != org && looksLikeDomain(s) {
			issue.Kind, issue.Shown = LinkElsewhere, s
			return issue, true
		}
	}
	if strings.Contains(host, "xn--") || !isASCII(host) {
		issue.Kind, issue.Unicode = LinkIDN, displayDomain(host)
		return issue, true
	}
	if shorteners[org] || shorteners[host] {
		issue.Kind = LinkShortener
		return issue, true
	}
	return issue, false
}

// looksLikeDomain says a word with a dot in a link's text is a domain,
// not a version number or a file: its ending is one domains have.
func looksLikeDomain(d string) bool {
	i := strings.LastIndexByte(d, '.')
	if i < 0 {
		return false
	}
	tld := d[i+1:]
	switch tld {
	case "pdf", "doc", "docx", "xls", "xlsx", "jpg", "jpeg", "png", "gif", "zip", "txt", "html", "htm", "php", "aspx":
		return false
	}
	return len(tld) == 2 || nameTLDs[tld] || tld == "gov" || tld == "edu" || tld == "mil" || tld == "int"
}

// Attachment endings.
var (
	runsExt = map[string]bool{".iso": true, ".img": true, ".vhd": true, ".vhdx": true, ".msix": true, ".appx": true,
		".chm": true, ".cpl": true, ".dll": true, ".ocx": true, ".sys": true, ".xll": true, ".one": true, ".library-ms": true,
		".search-ms": true, ".msp": true, ".gadget": true, ".application": true, ".apk": true, ".dmg": true, ".pkg": true,
		".deb": true, ".rpm": true, ".command": true, ".py": true, ".pl": true, ".rb": true, ".elf": true}
	macroExt = map[string]bool{".docm": true, ".dotm": true, ".xlsm": true, ".xltm": true, ".xlam": true, ".xlsb": true,
		".pptm": true, ".potm": true, ".ppsm": true, ".sldm": true, ".ppam": true, ".doc": true, ".xls": true, ".ppt": true}
	archiveExt = map[string]bool{".zip": true, ".rar": true, ".7z": true, ".gz": true, ".tgz": true, ".tar": true, ".bz2": true,
		".xz": true, ".cab": true, ".arj": true, ".ace": true, ".lzh": true, ".z": true, ".zst": true}
	docExt = map[string]bool{".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".jpg": true, ".jpeg": true,
		".png": true, ".gif": true, ".txt": true, ".rtf": true, ".odt": true, ".ppt": true, ".pptx": true, ".csv": true, ".mp3": true, ".mp4": true}
)

// protocolPart are the types of a signed or encrypted message's own
// parts — its signature, its ciphertext — which are no attachments.
var protocolPart = map[string]bool{"application/pgp-signature": true, "application/pkcs7-signature": true,
	"application/x-pkcs7-signature": true, "application/pgp-encrypted": true, "application/pkcs7-mime": true,
	"application/x-pkcs7-mime": true}

// keyTypes are the types keys come attached as: their names end as they
// like (.asc, .pub, .key, .cer).
var keyTypes = map[string]bool{"application/pgp-keys": true, "application/pkcs7-certificates": true,
	"application/x-x509-ca-cert": true, "application/pkix-cert": true}

// attachmentKind is what is worth a word about an attachment named name
// of type ctype, "" when nothing is.
func attachmentKind(name, ctype string) string {
	if strings.ContainsAny(name, "‮‭‪‫⁦⁧⁨‏") {
		return AttachHidden // right-to-left: "invoice‮fdp.exe" reads "invoiceexe.pdf"
	}
	low := strings.ToLower(strings.TrimSpace(name))
	ext := filepath.Ext(low)
	runs := unsafeAttachmentName(low) || runsExt[ext]
	if inner := filepath.Ext(strings.TrimSuffix(low, ext)); runs && docExt[inner] {
		return AttachHidden // "invoice.pdf.exe"
	}
	switch {
	case runs:
		return AttachRuns
	case macroExt[ext]:
		return AttachMacros
	case archiveExt[ext]:
		return AttachArchive
	}
	declared := strings.ToLower(strings.TrimSpace(strings.SplitN(ctype, ";", 2)[0]))
	if declared == "" || declared == "application/octet-stream" || ext == "" || keyTypes[declared] {
		return ""
	}
	expected, _, _ := mime.ParseMediaType(mime.TypeByExtension(ext))
	if expected == "" {
		return ""
	}
	major := func(t string) string { return strings.SplitN(t, "/", 2)[0] }
	if major(expected) != major(declared) || (expected == "application/pdf") != (declared == "application/pdf") {
		return AttachMismatch
	}
	return ""
}

// headerNotes are what a message's other headers say worth knowing.
func headerNotes(h mail.Header) []HeaderNote {
	var out []HeaderNote
	for _, k := range []string{"Disposition-Notification-To", "Return-Receipt-To", "X-Confirm-Reading-To"} {
		if v := strings.TrimSpace(h.Get(k)); v != "" {
			out = append(out, HeaderNote{Kind: HeaderReceipt, Value: decodeRFC2047(v)})
			break
		}
	}
	fromOrg := registrableDomain(domainOf(ExtractAddr(decodeRFC2047(h.Get("From")))))
	if rp := ExtractAddr(h.Get("Return-Path")); rp != "" && fromOrg != "" {
		if org := registrableDomain(domainOf(rp)); org != "" && org != fromOrg {
			out = append(out, HeaderNote{Kind: HeaderReturnPath, Value: org})
		}
	}
	if id := strings.Trim(strings.TrimSpace(h.Get("Message-Id")), "<>"); id != "" && fromOrg != "" {
		if org := registrableDomain(domainOf(id)); org != "" && strings.Contains(org, ".") && org != fromOrg {
			out = append(out, HeaderNote{Kind: HeaderMessageID, Value: org})
		}
	}
	if t, err := mail.ParseDate(h.Get("Date")); err == nil {
		received := time.Now()
		if all := h["Received"]; len(all) > 0 {
			if hop := readReceived(all[0]); !hop.At.IsZero() {
				received = hop.At
			}
		}
		if t.Sub(received) > 15*time.Minute {
			out = append(out, HeaderNote{Kind: HeaderFuture, Value: t.Format(time.RFC1123Z)})
		}
	}
	if v := strings.TrimSpace(h.Get("List-Unsubscribe")); v != "" {
		kind := HeaderUnsubscribe
		if strings.EqualFold(strings.TrimSpace(h.Get("List-Unsubscribe-Post")), "List-Unsubscribe=One-Click") {
			kind = HeaderOneClick
		}
		out = append(out, HeaderNote{Kind: kind, Value: decodeRFC2047(v)})
	}
	return out
}
