package emailinbox

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailtoken"
)

func TestTheHTMLOfAnInboundEmailIsStoredAsAnEmailBody(t *testing.T) {
	w := newWorld(t)
	msg := reply("ana@cliente.com", "m1@cliente.com")
	msg.HTML = "<p>Quero saber o preço</p>"
	if err := w.receive(msg, w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	assertOnlyEmailBodies(t, w.media.inputs, 1)
}

func TestTheHTMLOfAnOutboundEmailIsStoredAsAnEmailBody(t *testing.T) {
	w := newWorld(t)
	email := sentEmail(conversation.SentByWorkflow("wf-1"))
	email.HTML = "<p>Oi Ana</p>"
	if _, err := w.transcript().Transcribe(context.Background(), email); err != nil {
		t.Fatal(err)
	}
	assertOnlyEmailBodies(t, w.media.inputs, 1)
}

func assertOnlyEmailBodies(t *testing.T, stored []conversation.StoreMediaInput, want int) {
	t.Helper()
	if len(stored) != want {
		t.Fatalf("stored = %d, want %d", len(stored), want)
	}
	for _, in := range stored {
		if in.MimeType != em.HTMLMimeType || in.Purpose != conversation.MediaPurposeEmailBody {
			t.Fatalf("stored %+v, want the html kept as an email body", in)
		}
	}
}
