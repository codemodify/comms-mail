package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
)

var wordDec = &mime.WordDecoder{
	CharsetReader: charsetReader,
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	charset = strings.TrimSpace(charset)
	if charset == "" || strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "us-ascii") {
		return input, nil
	}
	if e, err := ianaindex.MIME.Encoding(charset); err == nil && e != nil {
		return e.NewDecoder().Reader(input), nil
	}
	if e, err := htmlindex.Get(charset); err == nil && e != nil {
		return e.NewDecoder().Reader(input), nil
	}
	return input, nil
}

func decodeRFC2047(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	out, err := wordDec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// rawAsText is RFC822 bytes as a TextArea string (UTF-8, else Latin-1).
func rawAsText(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	runes := make([]rune, len(raw))
	for i, c := range raw {
		runes[i] = rune(c)
	}
	return string(runes)
}

// ParseRFC822 fills a Message from a raw message. Attachments stay as Parts.
func ParseRFC822(raw []byte, folder FolderID, accountID string) (Message, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	h := msg.Header
	out := Message{
		Folder:       folder,
		AccountID:    accountID,
		From:         decodeRFC2047(h.Get("From")),
		To:           decodeRFC2047(h.Get("To")),
		Cc:           decodeRFC2047(h.Get("Cc")),
		Bcc:          decodeRFC2047(h.Get("Bcc")),
		ReplyTo:      decodeRFC2047(h.Get("Reply-To")),
		Subject:      decodeRFC2047(h.Get("Subject")),
		Size:         len(raw),
		RFCMessageID: h.Get("Message-Id"),
		InReplyTo:    h.Get("In-Reply-To"),
		References:   h.Get("References"),
	}
	out.ThreadID = ThreadIDOf(out)
	if t, err := mail.ParseDate(h.Get("Date")); err == nil {
		out.Date = t
	}
	ct := h.Get("Content-Type")
	media, params, _ := mime.ParseMediaType(ct)
	if media == "" {
		media = "text/plain"
	}
	body, _ := io.ReadAll(io.LimitReader(msg.Body, maxPartBytes))
	if strings.HasPrefix(strings.ToLower(media), "multipart/") {
		parseMultipart(&out, media, params["boundary"], body, "")
	} else {
		decoded := decodeTransfer(body, h.Get("Content-Transfer-Encoding"))
		decoded = decodeCharset(decoded, params["charset"])
		applyTextPart(&out, media, decoded, params["name"], false, "1")
		out.Parts = append(out.Parts, Part{
			ID: "1", MIMEType: media, Size: len(decoded), Charset: params["charset"],
			Filename: params["name"],
		})
	}
	if out.Snippet == "" {
		out.Snippet = snippetOf(out.Body)
	}
	return out, nil
}

// parseMultipart walks a multipart body. Part ids follow RFC 3501 section
// numbering: top-level parts are "1", "2", … and nested parts are
// "<parent>.<n>". The old code re-based the first nested multipart onto the
// top level, so a mixed[alternative[plain,html], pdf] message produced the
// ids 1, 2, 2 — the attachment collided with the HTML part.
func parseMultipart(out *Message, media, boundary string, body []byte, prefix string) {
	if boundary == "" {
		out.Body = string(body)
		return
	}
	r := multipart.NewReader(bytes.NewReader(body), boundary)
	i := 0
	for {
		p, err := r.NextPart()
		if err != nil {
			break
		}
		i++
		id := strconv.Itoa(i)
		if prefix != "" {
			id = prefix + "." + id
		}
		pct := p.Header.Get("Content-Type")
		pmedia, params, _ := mime.ParseMediaType(pct)
		if pmedia == "" {
			pmedia = "text/plain"
		}
		raw, _ := io.ReadAll(io.LimitReader(p, maxPartBytes))
		disp, dparams, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		filename := dparams["filename"]
		if filename == "" {
			filename = params["name"]
		}
		filename = decodeRFC2047(filename)
		if strings.HasPrefix(strings.ToLower(pmedia), "multipart/") {
			parseMultipart(out, pmedia, params["boundary"], raw, id)
			continue
		}
		decoded := decodeTransfer(raw, p.Header.Get("Content-Transfer-Encoding"))
		inline := !strings.EqualFold(disp, "attachment")
		if strings.HasPrefix(strings.ToLower(pmedia), "text/") && filename == "" {
			decoded = decodeCharset(decoded, params["charset"])
			applyTextPart(out, pmedia, decoded, filename, strings.EqualFold(disp, "attachment"), id)
		} else if filename != "" || strings.EqualFold(disp, "attachment") {
			out.HasAttach = true
			if filename != "" && !containsStr(out.Attachments, filename) {
				out.Attachments = append(out.Attachments, filename)
			}
		}
		out.Parts = append(out.Parts, Part{
			ID: id, MIMEType: pmedia, Filename: filename, Size: len(decoded),
			Charset: params["charset"], Inline: inline && filename == "",
		})
	}
}

// maxPartBytes caps one decoded MIME part. A hostile message (or a zip-bomb
// style nesting) cannot make the daemon allocate without bound.
const maxPartBytes = 64 << 20

// PartFromRaw decodes one MIME section out of a complete RFC822 message.
// partID uses the same numbering as Message.Parts ("1", "2.1", …); an empty
// id returns the displayable text body.
func PartFromRaw(raw []byte, partID string) (PartData, bool) {
	msg, err := ParseRFC822(raw, "", "")
	if err != nil {
		return PartData{}, false
	}
	if partID == "" {
		return PartData{
			Part: Part{ID: "1", MIMEType: "text/plain", Size: len(msg.Body)},
			Data: []byte(msg.Body),
		}, true
	}
	var want Part
	found := false
	for _, p := range msg.Parts {
		if p.ID == partID {
			want = p
			found = true
			break
		}
	}
	if !found {
		if partID == "1" && len(msg.Parts) == 0 {
			return PartData{
				Part: Part{ID: "1", MIMEType: "text/plain", Size: len(msg.Body)},
				Data: []byte(msg.Body),
			}, true
		}
		return PartData{}, false
	}
	data, ok := sectionBytes(raw, partID)
	if !ok {
		return PartData{Part: want}, true
	}
	want.Size = len(data)
	return PartData{Part: want, Data: data}, true
}

// sectionBytes returns the decoded bytes of one numbered section.
func sectionBytes(raw []byte, partID string) ([]byte, bool) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	media, params, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	body, _ := io.ReadAll(io.LimitReader(m.Body, maxPartBytes))
	if !strings.HasPrefix(strings.ToLower(media), "multipart/") {
		if partID != "1" {
			return nil, false
		}
		dec := decodeTransfer(body, m.Header.Get("Content-Transfer-Encoding"))
		return dec, true
	}
	return walkSection(body, params["boundary"], "", partID)
}

func walkSection(body []byte, boundary, prefix, want string) ([]byte, bool) {
	if boundary == "" {
		return nil, false
	}
	r := multipart.NewReader(bytes.NewReader(body), boundary)
	i := 0
	for {
		p, err := r.NextPart()
		if err != nil {
			return nil, false
		}
		i++
		id := strconv.Itoa(i)
		if prefix != "" {
			id = prefix + "." + id
		}
		media, params, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		raw, _ := io.ReadAll(io.LimitReader(p, maxPartBytes))
		if strings.HasPrefix(strings.ToLower(media), "multipart/") {
			if b, ok := walkSection(raw, params["boundary"], id, want); ok {
				return b, true
			}
			continue
		}
		if id != want {
			continue
		}
		dec := decodeTransfer(raw, p.Header.Get("Content-Transfer-Encoding"))
		if strings.HasPrefix(strings.ToLower(media), "text/") {
			dec = decodeCharset(dec, params["charset"])
		}
		return dec, true
	}
}
func applyTextPart(out *Message, media string, data []byte, filename string, attach bool, id string) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	low := strings.ToLower(media)
	if attach && filename != "" {
		out.HasAttach = true
		if !containsStr(out.Attachments, filename) {
			out.Attachments = append(out.Attachments, filename)
		}
		return
	}
	if strings.Contains(low, "html") {
		if out.HTML == "" {
			out.HTML = SanitizeHTML(text)
		}
		if out.Body == "" {
			out.Body = HTMLToText(text)
		}
		return
	}
	if out.Body == "" {
		out.Body = text
	}
}

func decodeTransfer(b []byte, enc string) []byte {
	enc = strings.ToLower(strings.TrimSpace(enc))
	switch enc {
	case "base64":
		s := bytes.Join(bytes.Fields(b), nil)
		if dec, err := decodeBase64(s); err == nil {
			return dec
		}
	case "quoted-printable":
		if dec, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(b))); err == nil {
			return dec
		}
	}
	return b
}

func decodeBase64(s []byte) ([]byte, error) {
	out := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
	n, err := base64.StdEncoding.Decode(out, s)
	if err != nil && n > 0 {
		// Truncated / padded-early payloads still yield the leading bytes.
		return out[:n], nil
	}
	return out[:n], err
}
func decodeCharset(b []byte, charset string) []byte {
	if utf8.Valid(b) && (charset == "" || strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "us-ascii")) {
		return b
	}
	r, err := charsetReader(charset, bytes.NewReader(b))
	if err != nil {
		return b
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return b
	}
	return out
}

func snippetOf(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	runes := []rune(s)
	if len(runes) > 140 {
		return string(runes[:140]) + "…"
	}
	return s
}

func containsStr(in []string, s string) bool {
	for _, x := range in {
		if x == s {
			return true
		}
	}
	return false
}

// DisplayBody is the text-only message view: prefer text/plain, else strip
// HTML tags. There is no HTML engine — the UI never paints markup.
func DisplayBody(m Message) string {
	if m.Body != "" && !looksLikeHTML(m.Body) {
		return m.Body
	}
	if m.HTML != "" {
		return HTMLToText(m.HTML)
	}
	if m.Body != "" {
		return HTMLToText(m.Body)
	}
	return ""
}

func looksLikeHTML(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "<html") || strings.Contains(low, "<body") ||
		strings.Contains(low, "<div") || strings.Contains(low, "<p>") ||
		strings.Contains(low, "<br") || strings.Contains(low, "<span") ||
		strings.Contains(low, "<table") || strings.Contains(low, "<!doctype")
}

func plainMessage(m Message) Message {
	m.Body = DisplayBody(m)
	m.HTML = ""
	if m.Snippet == "" {
		m.Snippet = snippetOf(m.Body)
	}
	return m
}

// HTMLToText is a conservative tag stripper (no JS execution).
// HTMLToText is a conservative tag stripper (no JS execution, no engine).
func HTMLToText(html string) string {
	html = stripBlocks(html, "script")
	html = stripBlocks(html, "style")
	var b strings.Builder
	inTag := false
	for i := 0; i < len(html); i++ {
		c := html[i]
		if c == '<' {
			if !looksLikeTagStart(html[i:]) {
				// A bare "1 < 2" is text, not markup: emitting it as a tag
				// used to swallow the rest of the line.
				b.WriteByte(c)
				continue
			}
			inTag = true
			switch tagName(html[i:]) {
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote":
				b.WriteByte('\n')
			}
			continue
		}
		if inTag {
			if c == '>' {
				inTag = false
			}
			continue
		}
		b.WriteByte(c)
	}
	out := decodeHTMLEntities(b.String())
	// Block-level open and close tags each emit a newline; collapse the
	// resulting runs so a paragraph break is one blank line, not five.
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

// looksLikeTagStart is true for "<tag", "</tag", "<!" and "<?".
func looksLikeTagStart(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[1]
	if c == '!' || c == '?' || c == '/' {
		return true
	}
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// decodeHTMLEntities resolves the named entities that actually turn up in
// mail plus every numeric reference (&#39;, &#x20AC;).
func decodeHTMLEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], ';')
		if end < 0 || end > 12 {
			b.WriteByte('&')
			i++
			continue
		}
		ent := s[i+1 : i+end]
		if r, ok := entityRune(ent); ok {
			b.WriteRune(r)
			i += end + 1
			continue
		}
		b.WriteByte('&')
		i++
	}
	return b.String()
}

func entityRune(ent string) (rune, bool) {
	if ent == "" {
		return 0, false
	}
	if ent[0] == '#' {
		num := ent[1:]
		base := 10
		if len(num) > 1 && (num[0] == 'x' || num[0] == 'X') {
			base, num = 16, num[1:]
		}
		n, err := strconv.ParseInt(num, base, 32)
		if err != nil || n <= 0 || n > 0x10FFFF {
			return 0, false
		}
		return rune(n), true
	}
	if r, ok := namedEntities[strings.ToLower(ent)]; ok {
		return r, true
	}
	return 0, false
}

var namedEntities = map[string]rune{
	"amp": '&', "lt": '<', "gt": '>', "quot": '"', "apos": '\'',
	"nbsp": ' ', "copy": '©', "reg": '®', "trade": '™', "hellip": '…',
	"mdash": '—', "ndash": '–', "lsquo": '\u2018', "rsquo": '\u2019',
	"ldquo": '\u201c', "rdquo": '\u201d', "bull": '•', "middot": '·',
	"euro": '€', "pound": '£', "yen": '¥', "cent": '¢', "sect": '§',
	"deg": '°', "plusmn": '±', "times": '×', "divide": '÷', "laquo": '«',
	"raquo": '»', "dagger": '†', "permil": '‰', "ne": '≠', "le": '≤', "ge": '≥',
}

func SanitizeHTML(html string) string {
	html = stripBlocks(html, "script")
	html = stripBlocks(html, "style")
	html = stripBlocks(html, "iframe")
	html = stripBlocks(html, "object")
	var b strings.Builder
	inTag := false
	var tag strings.Builder
	for i := 0; i < len(html); i++ {
		c := html[i]
		if c == '<' {
			inTag = true
			tag.Reset()
			tag.WriteByte(c)
			continue
		}
		if inTag {
			tag.WriteByte(c)
			if c == '>' {
				inTag = false
				cleaned := stripEventAttrs(tag.String())
				if cleaned != "" {
					b.WriteString(cleaned)
				}
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func stripBlocks(s, tag string) string {
	low := strings.ToLower(s)
	open := "<" + tag
	close := "</" + tag + ">"
	var b strings.Builder
	for {
		i := strings.Index(low, open)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		restLow := low[i:]
		j := strings.Index(restLow, close)
		if j < 0 {
			return b.String()
		}
		skip := j + len(close)
		s = s[i+skip:]
		low = low[i+skip:]
	}
}

func tagName(s string) string {
	if len(s) < 2 || s[0] != '<' {
		return ""
	}
	i := 1
	if i < len(s) && s[i] == '/' {
		i++
	}
	start := i
	for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	return strings.ToLower(s[start:i])
}

func stripEventAttrs(tag string) string {
	low := strings.ToLower(tag)
	if strings.HasPrefix(low, "<script") || strings.HasPrefix(low, "<iframe") {
		return ""
	}
	var b strings.Builder
	i := 0
	for i < len(tag) {
		if i+3 < len(tag) && (tag[i] == ' ' || tag[i] == '\t') && (tag[i+1] == 'o' || tag[i+1] == 'O') && (tag[i+2] == 'n' || tag[i+2] == 'N') {
			j := i + 3
			for j < len(tag) && tag[j] != '=' && tag[j] != '>' && tag[j] != ' ' {
				j++
			}
			if j < len(tag) && tag[j] == '=' {
				j++
				if j < len(tag) && (tag[j] == '"' || tag[j] == '\'') {
					q := tag[j]
					j++
					for j < len(tag) && tag[j] != q {
						j++
					}
					if j < len(tag) {
						j++
					}
				} else {
					for j < len(tag) && tag[j] != ' ' && tag[j] != '>' {
						j++
					}
				}
				i = j
				continue
			}
		}
		b.WriteByte(tag[i])
		i++
	}
	return b.String()
}

// BuildRFC822 writes a text or multipart message for SMTP / IMAP APPEND.
// BuildRFC822 writes a text or multipart message for SMTP / IMAP APPEND.
// It never fails: values that cannot be represented are sanitised. Prefer
// BuildRFC822Strict on the send path so a bad address is reported instead
// of silently rewritten.
func BuildRFC822(msg Message, ident Identity, files []AttachedFile) []byte {
	raw, err := BuildRFC822Strict(msg, ident, files)
	if err != nil {
		msg.To = sanitizeHeaderValue(msg.To)
		msg.Cc = sanitizeHeaderValue(msg.Cc)
		msg.Bcc = sanitizeHeaderValue(msg.Bcc)
		msg.From = sanitizeHeaderValue(msg.From)
		msg.Subject = sanitizeHeaderValue(msg.Subject)
		msg.RFCMessageID = sanitizeHeaderValue(msg.RFCMessageID)
		msg.InReplyTo = sanitizeHeaderValue(msg.InReplyTo)
		msg.References = sanitizeHeaderValue(msg.References)
		raw, err = BuildRFC822Strict(msg, ident, files)
		if err != nil {
			return nil
		}
	}
	return raw
}

// BuildRFC822Strict serialises msg for the wire.
//
// Two things it deliberately does NOT do:
//
//   - It never emits a Bcc header. Bcc recipients are passed to SMTP as
//     RCPT TO only; writing the header disclosed the blind list to every
//     recipient (and to the Sent copy on the server).
//   - It never copies a raw header value through. Every value is validated
//     for CR/LF (ErrHeaderInjection) and RFC 2047-encoded, so a To: field
//     containing "\r\nX-Evil: 1" cannot inject a header or a body.
func BuildRFC822Strict(msg Message, ident Identity, files []AttachedFile) ([]byte, error) {
	from := strings.TrimSpace(msg.From)
	if from == "" && ident.Address != "" {
		from = ident.DisplayFrom()
	}
	body := msg.Body
	if ident.Signature != "" && !strings.Contains(body, ident.Signature) {
		body = strings.TrimRight(body, "\n") + "\n\n-- \n" + ident.Signature + "\n"
	}

	type field struct {
		name  string
		value string
	}
	var fields []field
	add := func(name, v string, enc func(string, string) (string, error)) error {
		out, err := enc(name, v)
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) == "" {
			return nil
		}
		fields = append(fields, field{name, out})
		return nil
	}
	if err := add("From", from, encodeAddressList); err != nil {
		return nil, err
	}
	if err := add("To", msg.To, encodeAddressList); err != nil {
		return nil, err
	}
	if err := add("Cc", msg.Cc, encodeAddressList); err != nil {
		return nil, err
	}
	// Bcc is intentionally absent — see the doc comment.
	if err := add("Subject", msg.Subject, encodeUnstructured); err != nil {
		return nil, err
	}
	date := msg.Date
	if date.IsZero() {
		date = time.Now()
	}
	fields = append(fields, field{"Date", date.Format(time.RFC1123Z)})
	mid := strings.TrimSpace(msg.RFCMessageID)
	if mid == "" {
		mid = fmt.Sprintf("<%d.%s@uitoolkit>", date.UnixNano(), safeID(ident.Address))
	}
	if err := add("Message-ID", mid, encodeMsgIDList); err != nil {
		return nil, err
	}
	if err := add("In-Reply-To", msg.InReplyTo, encodeMsgIDList); err != nil {
		return nil, err
	}
	if err := add("References", msg.References, encodeMsgIDList); err != nil {
		return nil, err
	}
	fields = append(fields, field{"MIME-Version", "1.0"})

	var b bytes.Buffer
	writeFields := func(extra ...field) {
		for _, f := range append(fields, extra...) {
			fmt.Fprintf(&b, "%s: %s\r\n", f.name, f.value)
		}
	}
	if len(files) == 0 {
		writeFields(
			field{"Content-Type", `text/plain; charset="utf-8"`},
			field{"Content-Transfer-Encoding", "8bit"},
		)
		b.WriteString("\r\n")
		b.WriteString(toCRLF(body))
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\r\n")
		}
		return b.Bytes(), nil
	}
	boundary := fmt.Sprintf("uitk-%d-%s", date.UnixNano(), randID(6))
	writeFields(field{"Content-Type", `multipart/mixed; boundary="` + boundary + `"`})
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(toCRLF(body))
	b.WriteString("\r\n")
	for _, f := range files {
		ct := f.MIME
		if ct == "" {
			ct = "application/octet-stream"
		}
		if hasCTL(ct) {
			ct = "application/octet-stream"
		}
		base := attachFileName(f.Name)
		name, err := paramValue("filename", base)
		if err != nil {
			return nil, err
		}
		encName, err := encodeUnstructured("filename", name)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		fmt.Fprintf(&b, "Content-Type: %s; name=\"%s\"\r\n", ct, encName)
		fmt.Fprintf(&b, "Content-Disposition: attachment; filename=\"%s\"\r\n", encName)
		fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n\r\n")
		enc := base64.StdEncoding.EncodeToString(f.Data)
		for i := 0; i < len(enc); i += 76 {
			end := i + 76
			if end > len(enc) {
				end = len(enc)
			}
			b.WriteString(enc[i:end])
			b.WriteString("\r\n")
		}
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

// toCRLF normalises line endings and dot-stuffs nothing: net/smtp's DataWriter
// handles dot-stuffing, and IMAP APPEND counts the literal we pass verbatim.
func toCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// AttachedFile is a compose attachment. It is JSON-serialisable so a message
// composed offline keeps its files in the outbox queue.
type AttachedFile struct {
	Name string `json:"name"`
	MIME string `json:"mime,omitempty"`
	Data []byte `json:"data,omitempty"`
}

func guessMIME(name string) string {
	ext := strings.ToLower(name)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		if t := mime.TypeByExtension(ext[i:]); t != "" {
			return t
		}
	}
	return "application/octet-stream"
}
