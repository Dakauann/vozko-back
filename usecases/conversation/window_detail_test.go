package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type providerBlockedAdapter struct{ fakeWindowAdapter }

func (a *providerBlockedAdapter) WindowState(context.Context, *conversation.EntryContext) (conversation.WindowState, error) {
	return conversation.ClosedByProvider("blocked_by_buyer"), nil
}

func (a *providerBlockedAdapter) WindowStates(_ context.Context, entryIDs []string) (map[string]conversation.WindowState, error) {
	out := map[string]conversation.WindowState{}
	for _, id := range entryIDs {
		out[id] = conversation.ClosedByProvider("blocked_by_buyer")
	}
	return out, nil
}

func TestInboxEntryCarriesTheProviderDetail(t *testing.T) {
	svc := &HistoryProviderService{messageRepo: inboxWindowMessages{entry: conversation.EntryWithLastMessage{
		EntryID: "entry", EntryType: shared.EntryTypeInstagram, LastMessageType: conversation.MessageTypeSystem,
	}}}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&providerBlockedAdapter{fakeWindowAdapter{entryType: shared.EntryTypeInstagram}}))

	entry, err := svc.GetInboxEntry("entry", string(shared.EntryTypeInstagram))
	if err != nil {
		t.Fatal(err)
	}
	if entry.WindowOpen || entry.WindowClosedReason != string(conversation.WindowReasonProviderBlocked) || entry.WindowClosedDetail != "blocked_by_buyer" {
		t.Fatalf("entry window = open %v reason %q detail %q", entry.WindowOpen, entry.WindowClosedReason, entry.WindowClosedDetail)
	}
}
