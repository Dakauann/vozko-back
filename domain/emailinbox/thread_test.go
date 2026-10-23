package emailinbox

import (
	"strings"
	"testing"
)

func TestThreadAfter(t *testing.T) {
	cases := []struct {
		name     string
		conv     Conversation
		msg      InboundMessage
		subject  string
		key      string
		last     string
		chainEnd string
	}{
		{
			name:     "first message opens the thread",
			msg:      InboundMessage{Subject: "Re: Pedido 12", MessageID: "<m1@x>", InReplyTo: "<out1@vozko>", References: []string{"<out1@vozko>"}},
			subject:  "Pedido 12",
			key:      "out1@vozko",
			last:     "m1@x",
			chainEnd: "m1@x",
		},
		{
			name:     "later message keeps key and subject when blank",
			conv:     Conversation{Subject: "Pedido 12", ThreadKey: "out1@vozko", LastInboundMessageID: "m1@x", References: []string{"out1@vozko", "m1@x"}},
			msg:      InboundMessage{MessageID: "<m2@x>", InReplyTo: "<m1@x>"},
			subject:  "Pedido 12",
			key:      "out1@vozko",
			last:     "m2@x",
			chainEnd: "m2@x",
		},
		{
			name:     "missing message id keeps the last one",
			conv:     Conversation{Subject: "A", ThreadKey: "k@x", LastInboundMessageID: "m1@x", References: []string{"k@x", "m1@x"}},
			msg:      InboundMessage{Subject: "B"},
			subject:  "B",
			key:      "k@x",
			last:     "m1@x",
			chainEnd: "m1@x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.conv.ThreadAfter(tc.msg)
			if got.Subject != tc.subject || got.ThreadKey != tc.key || got.LastInboundMessageID != tc.last {
				t.Fatalf("ThreadAfter = %+v", got)
			}
			if got.References[len(got.References)-1] != tc.chainEnd {
				t.Fatalf("chain = %s", strings.Join(got.References, " "))
			}
		})
	}
}

func TestExternalMessageID(t *testing.T) {
	cases := []struct{ header, provider, want string }{
		{"<a@x>", "ses-1", "a@x"},
		{"", "ses-1", "provider:ses-1"},
		{"broken id", "ses-1", "provider:ses-1"},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := ExternalMessageID(tc.header, tc.provider); got != tc.want {
			t.Errorf("ExternalMessageID(%q, %q) = %q, want %q", tc.header, tc.provider, got, tc.want)
		}
	}
	if AttachmentRef("a@x", 2) != "a@x:att2" {
		t.Error("attachment refs are the message id plus the index")
	}
}
