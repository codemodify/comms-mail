package mailcore

import (
	"bytes"
	"net/mail"
	"testing"
)

// Each server's way of saying how it took a message: where from, which
// server, the protocol, TLS and its cipher where noted, whether the sender
// signed in, and when.
func TestReadReceived(t *testing.T) {
	for _, c := range []struct {
		name string
		v    string
		want RouteHop
	}{
		{"gmail", "from mail.example.com (mail.example.com. [203.0.113.5])\r\n        by mx.google.com with ESMTPS id x12si1\r\n        for <me@gmail.com>\r\n        (version=TLS1_3 cipher=TLS_AES_256_GCM_SHA384 bits=256/256);\r\n        Mon, 05 Oct 2026 03:00:02 -0700 (PDT)",
			RouteHop{From: "mail.example.com", FromIP: "203.0.113.5", By: "mx.google.com", With: "ESMTPS", TLS: HopTLS, Version: "TLS 1.3", Cipher: "TLS_AES_256_GCM_SHA384"}},
		{"postfix", "from out.example.net (out.example.net [198.51.100.7])\r\n\t(using TLSv1.3 with cipher TLS_AES_256_GCM_SHA384 (256/256 bits)\r\n\t key-exchange X25519 server-signature RSA-PSS (2048 bits) server-digest SHA256)\r\n\t(No client certificate requested)\r\n\tby mx.example.org (Postfix) with ESMTPS id 4ABCD\r\n\tfor <me@example.org>; Mon,  5 Oct 2026 10:00:00 +0000 (UTC)",
			RouteHop{From: "out.example.net", FromIP: "198.51.100.7", By: "mx.example.org", With: "ESMTPS", TLS: HopTLS, Version: "TLS 1.3", Cipher: "TLS_AES_256_GCM_SHA384"}},
		{"submission", "from [192.168.1.5] (unknown [198.51.100.9])\r\n\t(Authenticated sender: ann@example.com)\r\n\tby mail.example.com (Postfix) with ESMTPSA id 1;\r\n\tMon, 5 Oct 2026 09:59:58 +0000 (UTC)",
			RouteHop{From: "192.168.1.5", FromIP: "192.168.1.5", By: "mail.example.com", With: "ESMTPSA", TLS: HopTLS, Auth: true}},
		{"microsoft", "from DM6PR11MB1234.namprd11.prod.outlook.com (2603:10b6:5:1c0::12) by\r\n BN6PR11MB5678.namprd11.prod.outlook.com (2603:10b6:405:6a::30) with\r\n Microsoft SMTP Server (version=TLS1_2,\r\n cipher=TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384) id 15.20.6838.25; Mon, 5 Oct\r\n 2026 10:00:01 +0000",
			RouteHop{From: "dm6pr11mb1234.namprd11.prod.outlook.com", FromIP: "2603:10b6:5:1c0::12", By: "bn6pr11mb5678.namprd11.prod.outlook.com", With: "Microsoft SMTP Server", TLS: HopTLS, Version: "TLS 1.2", Cipher: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", Inside: true}},
		{"exim", "from [203.0.113.8] (helo=smtp.example.com)\r\n\tby mx.example.org with esmtps  (TLS1.3) tls TLS_AES_128_GCM_SHA256\r\n\t(Exim 4.96) (envelope-from <a@example.com>) id 1abc-000; Mon, 05 Oct 2026 10:00:00 +0000",
			RouteHop{From: "203.0.113.8", FromIP: "203.0.113.8", By: "mx.example.org", With: "ESMTPS", TLS: HopTLS, Version: "TLS 1.3", Cipher: "TLS_AES_128_GCM_SHA256"}},
		{"clear", "from old.example.com (old.example.com [192.0.2.1]) by mx.example.org with SMTP id 9; Mon, 5 Oct 2026 10:00:00 +0000",
			RouteHop{From: "old.example.com", FromIP: "192.0.2.1", By: "mx.example.org", With: "SMTP", TLS: HopClear}},
		{"google inside", "by 2002:a05:7300:d311:b0:1c5:f6de:a6c with SMTP id k4csp1;\r\n        Mon, 5 Oct 2026 03:00:03 -0700 (PDT)",
			RouteHop{By: "2002:a05:7300:d311:b0:1c5:f6de:a6c", With: "SMTP", TLS: HopClear, Inside: true}},
		{"local", "from localhost (localhost [127.0.0.1]) by mx.example.org (Postfix) with LMTP id 5; Mon, 5 Oct 2026 10:00:04 +0000",
			RouteHop{From: "localhost", FromIP: "127.0.0.1", By: "mx.example.org", With: "LMTP", TLS: HopClear, Inside: true}},
	} {
		raw := lf2crlf("Received: " + c.v + "\nFrom: a@example.com\n\nx\n")
		m, err := mail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		hops := routeOf(m.Header, nil)
		if len(hops) != 1 {
			t.Fatalf("%s: %d hops", c.name, len(hops))
		}
		got := hops[0]
		if got.At.IsZero() {
			t.Errorf("%s: no time", c.name)
		}
		got.At = c.want.At
		if got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

// The route reads bottom up, first hop first; the hops your provider's
// servers wrote — down to the one that took it in from outside — are
// yours, those under it came with the message.
func TestRouteOrderAndWhoseHops(t *testing.T) {
	raw := lf2crlf("Received: by 2002:a05:7300:d311:b0:1c5:f6de:a6c with SMTP id k; Mon, 5 Oct 2026 10:00:05 +0000\n" +
		"Received: from mail.example.com (mail.example.com [203.0.113.5]) by mx.google.com with ESMTPS id x (version=TLS1_3 cipher=TLS_AES_256_GCM_SHA384); Mon, 5 Oct 2026 10:00:03 +0000\n" +
		"Received: from [10.0.0.2] (unknown [198.51.100.9]) (Authenticated sender: ann@example.com) by mail.example.com with ESMTPSA id 1; Mon, 5 Oct 2026 10:00:00 +0000\n" +
		"From: ann@example.com\n\nx\n")
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	hops := routeOf(m.Header, map[string]bool{"google.com": true, "gmail.com": true})
	if len(hops) != 3 || hops[0].By != "mail.example.com" || !hops[0].Auth || hops[1].By != "mx.google.com" || hops[2].From != "" {
		t.Fatalf("route %+v", hops)
	}
	if hops[0].Yours || !hops[1].Yours || !hops[2].Yours {
		t.Fatalf("yours: %v %v %v", hops[0].Yours, hops[1].Yours, hops[2].Yours)
	}
	if hops[1].Inside || !hops[2].Inside {
		t.Fatalf("inside: %v %v", hops[1].Inside, hops[2].Inside)
	}
	if !hops[0].At.Before(hops[1].At) {
		t.Fatalf("times %v %v", hops[0].At, hops[1].At)
	}
}
