package ws

import (
	"testing"

	"vozko/domain/conversation"
)

type providerBlockedHistory struct{ aroundHistoryProvider }

func (p *providerBlockedHistory) GetWindowStatusForEntry(string, string) conversation.WindowState {
	return conversation.ClosedByProvider("blocked_by_buyer")
}

func TestSubscribedCarriesTheProviderDetailOfAClosedWindow(t *testing.T) {
	hub, conn, _ := historyHub(t, true)
	hub.SetHistoryProvider(&providerBlockedHistory{})

	hub.handleSubscribe(conn, payloadOf(t, SubscribePayload{EntryID: "e1", EntryType: "instagram"}))

	got := nextEvent(t, conn, WSEventSubscribed)
	if got["window_open"] != false || got["window_closed_reason"] != "provider_blocked" || got["window_closed_detail"] != "blocked_by_buyer" {
		t.Fatalf("subscribed = %v", got)
	}
}

func TestSubscribedOmitsAnEmptyDetail(t *testing.T) {
	hub, conn, _ := historyHub(t, true)

	hub.handleSubscribe(conn, payloadOf(t, SubscribePayload{EntryID: "e1", EntryType: "instagram"}))

	if got := nextEvent(t, conn, WSEventSubscribed); got["window_closed_detail"] != nil {
		t.Fatalf("window_closed_detail = %v, want absent", got["window_closed_detail"])
	}
}
