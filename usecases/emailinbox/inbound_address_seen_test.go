package emailinbox

import (
	"slices"
	"testing"

	"vozko/domain/emailaddress"
	"vozko/domain/emailtoken"
)

func TestTheSenderOfAnInboundEmailIsSeenSoItsDomainCheckStartsBeforeTheReply(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.seen.Seen(), []emailaddress.Address{"ana@cliente.com"}) {
		t.Fatalf("seen %v, want the sender's address", w.seen.Seen())
	}
}
