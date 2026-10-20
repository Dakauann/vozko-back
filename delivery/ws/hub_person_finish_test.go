package ws

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type refusingFinish struct {
	err   error
	calls []shared.EntryRef
}

func (f *refusingFinish) Finish(_ context.Context, _ shared.Person, _ string, entry shared.EntryRef, _ string) error {
	f.calls = append(f.calls, entry)
	return f.err
}

func TestFinishingFromTheSocketGoesThroughThePersonFinishRule(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{entryAccess: map[string]bool{"user-1": true}}
	hub := NewConversationHub(authorizer, nil, nil, nil, "test-replica", "")
	hub.statusUpdater = newMockStatusUpdater()
	finish := &refusingFinish{err: conversation.ErrStatusChangeForbidden}
	hub.SetPersonFinish(finish)
	conn := &WSConnection{ID: "c", UserID: "user-1", WorkspaceID: "ws-1", Send: make(chan []byte, 5)}

	payload, _ := json.Marshal(SetConversationStatusPayload{EntryID: "entry-1", EntryType: "telegram", Status: "finished"})
	hub.handleSetConversationStatus(conn, payload)

	require.Equal(t, []shared.EntryRef{{EntryID: "entry-1", EntryType: shared.EntryTypeTelegram}}, finish.calls)
	require.Equal(t, "forbidden", drainErrorPayload(t, conn).Code)
}
