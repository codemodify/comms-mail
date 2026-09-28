package mailcore

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestInlineImagesByContentID(t *testing.T) {
	pic := tinyPNG(t)
	raw := "From: a@example.com\r\nSubject: logo\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=\"rel\"\r\n\r\n" +
		"--rel\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p><img src=\"cid:logo@x\"></p>\r\n" +
		"--rel\r\nContent-Type: image/png\r\nContent-ID: <logo@x>\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(pic) + "\r\n" +
		"--rel\r\nContent-Type: application/pdf\r\nContent-ID: <doc@x>\r\n\r\n%PDF\r\n" +
		"--rel--\r\n"
	imgs := InlineImages([]byte(raw))
	if len(imgs) != 1 || imgs[0].CID != "logo@x" || imgs[0].MIME != "image/png" || !bytes.Equal(imgs[0].Data, pic) {
		t.Fatalf("inline images %+v", imgs)
	}
	if InlineImages([]byte("From: a\r\n\r\nplain")) != nil {
		t.Fatal("a plain message has no inline images")
	}
}

// Remote images are fetched from public addresses only: a message cannot
// make the user's machine call something on it or its network.
func TestFetchRemoteImagesRefusesLocalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer srv.Close()
	got := FetchRemoteImages(context.Background(), []string{srv.URL + "/a.png", "file:///etc/passwd", "cid:x"})
	if got[0].Data != nil || !strings.Contains(got[0].Error, "private") {
		t.Fatalf("loopback fetch: %+v", got[0])
	}
	if got[1].Error == "" || got[2].Error == "" {
		t.Fatalf("non-http sources: %+v", got[1:])
	}
	for _, ip := range []string{"10.1.2.3", "192.168.1.1", "172.16.0.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "0.0.0.0"} {
		if publicIP(net.ParseIP(ip)) {
			t.Fatalf("%s counted as public", ip)
		}
	}
	if !publicIP(net.ParseIP("93.184.216.34")) {
		t.Fatal("a public address was refused")
	}
}

func TestFetchRemoteImagesLimits(t *testing.T) {
	pic := tinyPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pic)
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		case "/huge":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(make([]byte, maxRemoteImageBytes+10))
		case "/missing":
			http.NotFound(w, r)
		case "/redirect":
			http.Redirect(w, r, "ftp://example.com/x.png", http.StatusFound)
		}
	}))
	defer srv.Close()
	imageAddrAllowed = func(net.IP) bool { return true }
	defer func() { imageAddrAllowed = publicIP }()

	got := FetchRemoteImages(context.Background(), []string{
		srv.URL + "/ok.png", srv.URL + "/page", srv.URL + "/huge", srv.URL + "/missing", srv.URL + "/redirect",
		"//" + strings.TrimPrefix(srv.URL, "http://") + "/ok.png",
	})
	if !bytes.Equal(got[0].Data, pic) || got[0].MIME != "image/png" || got[0].Error != "" {
		t.Fatalf("ok: %+v", got[0])
	}
	for i, want := range []string{"not an image", "too large", "404", "redirect"} {
		if !strings.Contains(got[i+1].Error, want) || got[i+1].Data != nil {
			t.Fatalf("%s: %+v", want, got[i+1].Error)
		}
	}
	// A protocol-relative URL is fetched over https; the test server
	// speaks http, so it fails — but it was tried as https, not refused.
	if got[5].Error == "" || strings.Contains(got[5].Error, "not an http") {
		t.Fatalf("protocol-relative: %+v", got[5].Error)
	}

	many := make([]string, maxRemoteImages+20)
	for i := range many {
		many[i] = fmt.Sprintf("%s/ok.png?%d", srv.URL, i)
	}
	if n := len(FetchRemoteImages(context.Background(), many)); n != maxRemoteImages {
		t.Fatalf("fetched %d images, cap is %d", n, maxRemoteImages)
	}
}

func TestRemoteImageSendersPersist(t *testing.T) {
	dir := t.TempDir()
	st := newTestLocalStore(t, dir)
	if err := st.AllowRemoteImages("News <News@Example.com>", true); err != nil {
		t.Fatal(err)
	}
	if err := st.AllowRemoteImages("not an address", true); err == nil {
		t.Fatal("accepted a non-address")
	}
	again := newTestLocalStore(t, dir)
	if got := again.RemoteImageSenders(); len(got) != 1 || got[0] != "news@example.com" {
		t.Fatalf("after restart %v", got)
	}
	_ = again.AllowRemoteImages("news@example.com", false)
	if got := again.RemoteImageSenders(); len(got) != 0 {
		t.Fatalf("after removing %v", got)
	}
}

// messages.getRaw hands back the message byte for byte (Save As .eml).
func TestRawIsByteExact(t *testing.T) {
	cli := demoInviteClient(t)
	raw, err := cli.Raw(DemoInviteID)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := NewDemoStore().GetRaw(DemoInviteID)
	if !bytes.Equal(raw, want) {
		t.Fatalf("raw differs: %d bytes vs %d", len(raw), len(want))
	}
}
