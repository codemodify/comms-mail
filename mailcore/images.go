package mailcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Images in HTML mail come two ways. An inline image is a part of the
// message itself, named by its Content-ID and referenced as `cid:…`: it is
// safe to show, nothing leaves the machine. A remote image is a URL, and
// loading it tells the sender the message was opened (and from where) — so
// it is fetched only when the user asks, for this message or for every
// message from its sender.

// InlineImage is one image part of a message, by Content-ID.
type InlineImage struct {
	CID  string `json:"cid"`
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

// Limits on what one message's images may cost.
const (
	maxInlineImages     = 64
	maxInlineImageBytes = 32 << 20 // all of them together
	maxRemoteImages     = 100
	maxRemoteImageBytes = 8 << 20  // one image
	maxRemoteTotalBytes = 48 << 20 // all of one request's
	remoteImageTimeout  = 15 * time.Second
)

// InlineImages returns a raw message's image parts that carry a
// Content-ID, decoded, within the limits above.
func InlineImages(raw []byte) []InlineImage {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	media, params, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if !strings.HasPrefix(strings.ToLower(media), "multipart/") {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(m.Body, maxPartBytes))
	var out []InlineImage
	total := 0
	var walk func(body []byte, boundary string, depth int)
	walk = func(body []byte, boundary string, depth int) {
		if boundary == "" || depth > 8 {
			return
		}
		r := multipart.NewReader(bytes.NewReader(body), boundary)
		for len(out) < maxInlineImages {
			p, err := r.NextPart()
			if err != nil {
				return
			}
			pmedia, pparams, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
			pmedia = strings.ToLower(pmedia)
			data, _ := io.ReadAll(io.LimitReader(p, maxPartBytes))
			if strings.HasPrefix(pmedia, "multipart/") {
				walk(data, pparams["boundary"], depth+1)
				continue
			}
			cid := strings.Trim(strings.TrimSpace(p.Header.Get("Content-Id")), "<>")
			if cid == "" || !strings.HasPrefix(pmedia, "image/") {
				continue
			}
			dec := decodeTransfer(data, p.Header.Get("Content-Transfer-Encoding"))
			if total+len(dec) > maxInlineImageBytes {
				return
			}
			total += len(dec)
			out = append(out, InlineImage{CID: cid, MIME: pmedia, Data: dec})
		}
	}
	walk(body, params["boundary"], 0)
	return out
}

// RemoteImage is one fetched image, or why it could not be.
type RemoteImage struct {
	URL   string `json:"url"`
	MIME  string `json:"mime,omitempty"`
	Data  []byte `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// errPrivateAddress refuses a fetch that would reach this machine or its
// network: an image URL in a message must not be a way to make the user's
// computer call their router, a local service or a cloud metadata address.
var errPrivateAddress = errors.New("images: refusing a local or private-network address")

// imageDialer connects only to public addresses. The check runs on the
// address actually dialled, after DNS, so a name that resolves (or
// re-resolves) to 127.0.0.1 is refused too.
var imageDialer = &net.Dialer{
	Timeout: 10 * time.Second,
	Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || !imageAddrAllowed(ip) {
			return errPrivateAddress
		}
		return nil
	},
}

// imageAddrAllowed decides which addresses images may come from (tests
// widen it to reach their local server).
var imageAddrAllowed = publicIP

// publicIP reports whether ip is a routable internet address.
func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.Equal(net.IPv4bcast) || cgnat.Contains(ip))
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// imageClient fetches remote images: public addresses only, no cookies,
// a bounded number of redirects that stay on http(s).
var imageClient = &http.Client{
	Timeout: remoteImageTimeout,
	Transport: &http.Transport{
		DialContext:           imageDialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConnsPerHost:   2,
		Proxy:                 nil, // a proxy would dial for us, past the address check
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("images: too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("images: redirect away from http")
		}
		return nil
	},
}

// imageURL is u as something to fetch: http or https (a protocol-relative
// //host/x is https), or "" for anything else.
func imageURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return ""
	}
	return p.String()
}

// FetchRemoteImages downloads the given image URLs, a few at a time, each
// bounded in size and time and all of them in total. The order of the
// result follows urls; a URL that could not be fetched says why.
func FetchRemoteImages(ctx context.Context, urls []string) []RemoteImage {
	if len(urls) > maxRemoteImages {
		urls = urls[:maxRemoteImages]
	}
	out := make([]RemoteImage, len(urls))
	var mu sync.Mutex
	total := 0
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, raw := range urls {
		out[i].URL = raw
		u := imageURL(raw)
		if u == "" {
			out[i].Error = "not an http(s) image"
			continue
		}
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mimeType, data, err := fetchImage(ctx, u)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				out[i].Error = err.Error()
			case total+len(data) > maxRemoteTotalBytes:
				out[i].Error = "images: this message's images are too large"
			default:
				total += len(data)
				out[i].MIME, out[i].Data = mimeType, data
			}
		}(i, u)
	}
	wg.Wait()
	return out
}

func fetchImage(ctx context.Context, u string) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp;q=0.9,*/*;q=0.1")
	resp, err := imageClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("images: %s", resp.Status)
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.HasPrefix(ct, "image/") && ct != "application/octet-stream" {
		return "", nil, fmt.Errorf("images: not an image (%s)", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) > maxRemoteImageBytes {
		return "", nil, errors.New("images: image too large")
	}
	return ct, data, nil
}

// ---- per-sender permission -------------------------------------------------

// RemoteImageSenders lists the senders whose remote images load without
// asking.
func (f *featureHost) RemoteImageSenders() []string {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.imageSenders...)
}

// AllowRemoteImages adds (or, with allow false, removes) a sender whose
// remote images load without asking.
func (f *featureHost) AllowRemoteImages(address string, allow bool) error {
	if f == nil {
		return fmt.Errorf("mail: no feature host")
	}
	a := canonAddr(address)
	if a == "" || !strings.Contains(a, "@") {
		return fmt.Errorf("mail: %q is not an address", address)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.imageSenders[:0]
	for _, x := range f.imageSenders {
		if x != a {
			out = append(out, x)
		}
	}
	if allow {
		out = append(out, a)
	}
	f.imageSenders = out
	return nil
}
