package ses

import (
	"net/netip"
	"strings"
	"testing"
)

func TestConnectingIPReadsTheReceivedHeaderSESAdds(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "gmail over ipv4, folded as SES writes it",
			raw: "Return-Path: <ana@gmail.com>\r\n" +
				"Received: from mail-ed1-f48.google.com (mail-ed1-f48.google.com [209.85.208.48])\r\n" +
				" by inbound-smtp.us-east-1.amazonaws.com with SMTP id 4lqu8kk0nkd1mfd1bu8i4h33r8hmfa1c2k9rtd01\r\n" +
				" for reply+abc@joana.envios.test;\r\n" +
				" Tue, 21 Oct 2026 12:00:00 +0000 (UTC)\r\n" +
				"X-SES-Spam-Verdict: PASS\r\n" +
				"X-SES-Virus-Verdict: PASS\r\n" +
				"Received: by mail-ed1-f48.google.com with SMTP id a640c23a62f3a-abc for <reply+abc@joana.envios.test>; Tue, 21 Oct 2026 05:00:00 -0700 (PDT)\r\n" +
				"From: Ana <ana@gmail.com>\r\n\r\nOi",
			want: "209.85.208.48",
		},
		{
			name: "outlook over ipv6",
			raw: "Received: from EUR05-AM6-obe.outbound.protection.outlook.com (mail-am6eur05on2101.outbound.protection.outlook.com [IPv6:2a01:111:f400:7e1b::101])\r\n" +
				" by inbound-smtp.sa-east-1.amazonaws.com with SMTP id x for reply@reply.loja.com.br;\r\n" +
				" Tue, 21 Oct 2026 12:00:00 +0000 (UTC)\r\n\r\nbody",
			want: "2a01:111:f400:7e1b::101",
		},
		{
			name: "a sender without reverse dns",
			raw:  "Received: from [203.0.113.7] (unknown [203.0.113.7]) by inbound-smtp.us-east-1.amazonaws.com with SMTP id y for reply@x.test; Tue, 21 Oct 2026 12:00:00 +0000\r\n\r\n",
			want: "203.0.113.7",
		},
		{
			name: "an ipv4 mapped address counts as the ipv4 sender",
			raw:  "Received: from h (h [IPv6:::ffff:198.51.100.4]) by inbound-smtp.us-east-1.amazonaws.com with SMTP id z; Tue, 21 Oct 2026 12:00:00 +0000\r\n\r\n",
			want: "198.51.100.4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ReceivedHeaders{}.ConnectingIP([]byte(tc.raw))
			if !ok || got != netip.MustParseAddr(tc.want) {
				t.Fatalf("ConnectingIP = %v, %v; want %s", got, ok, tc.want)
			}
		})
	}
}

func TestConnectingIPNeverGuesses(t *testing.T) {
	cases := map[string]string{
		"no received header":             "From: a@b.com\r\n\r\nhi",
		"the top header is not from SES": "Received: from evil (evil [192.0.2.1]) by mx.attacker.test with SMTP; Tue, 21 Oct 2026 12:00:00 +0000\r\nReceived: from x (x [203.0.113.7]) by inbound-smtp.us-east-1.amazonaws.com with SMTP\r\n\r\n",
		"no bracketed address":           "Received: from mail.example.com by inbound-smtp.us-east-1.amazonaws.com with SMTP\r\n\r\n",
		"garbage in the brackets":        "Received: from h (h [not-an-ip]) by inbound-smtp.us-east-1.amazonaws.com with SMTP\r\n\r\n",
		"empty message":                  "",
		"binary noise":                   string([]byte{0xff, 0x00, 0x13, '\n', '\n'}),
		"an address only after by":       "Received: from h by inbound-smtp.us-east-1.amazonaws.com ([10.0.0.1]) with SMTP\r\n\r\n",
		"a truncated header":             "Received: from h (h [203.0.113.7",
		"huge header":                    "Received: from " + strings.Repeat("a", 1<<16) + "\r\n\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if got, ok := (ReceivedHeaders{}).ConnectingIP([]byte(raw)); ok {
				t.Fatalf("ConnectingIP = %v, want no address", got)
			}
		})
	}
}
