package emailinbox

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emailsending"
)

func TestDeliveryObserverNeverTouchesTheInboxForAPISends(t *testing.T) {
	for _, kind := range []emailsending.EventKind{emailsending.EventDelivery, emailsending.EventBounce, emailsending.EventReject} {
		w := newObserverWorld()
		w.recipientConversation(t, "ws-1", "ana@cliente.com", "conv-7")
		w.transcriptRow("conv-7", "ses-9", conversation.DeliveryStatusSent)
		w.transcriptRow("send-1", "ses-9", conversation.DeliveryStatusSent)

		err := w.observer.OnMessageEvent(context.Background(),
			emailsending.Message{WorkspaceID: "ws-1", Address: "ana@cliente.com", RunKind: emailsending.RunAPI, RunID: "send-1", ProviderMessageID: "ses-9"},
			emailsending.DeliveryEvent{Kind: kind},
			emailsending.EventEffect{Changed: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(w.messages.saved) != 0 || len(w.announcer.updated) != 0 {
			t.Fatalf("%s of an API send changed the inbox: saved %d, announced %v", kind, len(w.messages.saved), w.announcer.updated)
		}
	}
}
