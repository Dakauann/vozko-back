package ws

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
)

type leadInfoHistoryProvider struct {
	statusTestHistoryProvider
	info conversation.EntryInfo
}

func (p *leadInfoHistoryProvider) GetEntryInfo(string, string) (conversation.EntryInfo, error) {
	return p.info, nil
}

func subscribedLeadFields(t *testing.T, info conversation.EntryInfo) map[string]json.RawMessage {
	t.Helper()
	hub := newMultiOpenHub(t)
	hub.SetHistoryProvider(&leadInfoHistoryProvider{info: info})
	conn := openConn("conn-1", "user-1")
	registerConn(hub, conn)

	payload, err := json.Marshal(SubscribePayload{EntryID: "entry-a", EntryType: "whatsapp"})
	require.NoError(t, err)
	hub.handleSubscribe(conn, payload)

	env := drainEvent(t, conn)
	require.Equal(t, WSEventSubscribed, env.Type)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(env.Payload, &fields))
	return fields
}

func TestSubscribedCarriesTheLeadBlockStateWithItsVersion(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		fields := subscribedLeadFields(t, conversation.EntryInfo{LeadID: "lead-1", LeadVersion: 5, Blocked: blocked})

		assert.JSONEq(t, `"lead-1"`, string(fields["lead_id"]))
		assert.JSONEq(t, `5`, string(fields["lead_version"]))
		require.Contains(t, fields, "blocked", "the block state must be sent even when false, so the answer can vouch for it")
		want, _ := json.Marshal(blocked)
		assert.JSONEq(t, string(want), string(fields["blocked"]))
	}
}
