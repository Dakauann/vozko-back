package ws

import (
	"fmt"
	"log"
	"testing"

	"vozko/domain/calls/calllist"
)

func TestACallListItemThatCannotBeDialedAnswersItsOwnCode(t *testing.T) {
	h := NewCallSessionWSHandler(&fakeStartUseCase{}, &fakeEndUseCase{}, nil, allowAllAuthorizer{}, log.Default(), noopWSMetricsRecorder{})
	for _, err := range []error{
		calllist.ErrItemNotReserved, calllist.ErrItemMismatch, calllist.ErrItemNotFound, calllist.ErrListNotActive,
		calllist.ErrNotAssignee, fmt.Errorf("check: %w", calllist.ErrListBuilding),
	} {
		var sent []*WSOutgoingMessage
		h.sendStartCallError(func(m *WSOutgoingMessage) { sent = append(sent, m) }, err)
		if len(sent) != 1 {
			t.Fatalf("%v: sent %d messages", err, len(sent))
		}
		payload, ok := sent[0].Payload.(ErrorPayload)
		if !ok || payload.Code != "call_list_item_unavailable" {
			t.Errorf("%v: sent %+v, want call_list_item_unavailable", err, sent[0])
		}
	}
}
