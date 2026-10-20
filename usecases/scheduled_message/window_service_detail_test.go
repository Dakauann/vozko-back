package scheduled_message_usecase

import (
	"testing"

	"vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
)

type providerClosedWindows struct{ state conversation.WindowState }

func (w providerClosedWindows) GetWindowStatusForEntry(string, string) conversation.WindowState {
	return w.state
}

func TestWindowStateCarriesTheProviderDetail(t *testing.T) {
	svc, err := newWindowService(providerClosedWindows{state: conversation.ClosedByProvider("blocked_by_buyer")}, sm.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}

	state := svc.State("conv-1", "instagram")
	if state.Open || state.ClosedReason != string(conversation.WindowReasonProviderBlocked) || state.ClosedDetail != "blocked_by_buyer" {
		t.Fatalf("state = %+v", state)
	}
}
