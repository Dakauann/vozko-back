package ws

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type fixedMediaLimits map[shared.EntryType][]conversation.MediaLimit

func (f fixedMediaLimits) MediaLimits(entryType shared.EntryType) ([]conversation.MediaLimit, bool) {
	limits, ok := f[entryType]
	return limits, ok
}

func subscribedFields(t *testing.T, hub *ConversationHub, conn *WSConnection, entryType string) map[string]json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(SubscribePayload{EntryID: "entry-1", EntryType: entryType})
	require.NoError(t, err)
	hub.handleSubscribe(conn, payload)
	msg := awaitSendPayload(t, conn)
	require.Equal(t, WSEventSubscribed, msg.Type)
	raw, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	return fields
}

func TestSubscribingSendsTheChannelMediaLimits(t *testing.T) {
	hub, conn := newActionHub(t, newFakeMessageAction())
	hub.SetMediaLimits(fixedMediaLimits{
		shared.EntryTypeTelegram: {{Kind: channel.MediaImage, MaxBytes: 10 << 20, MIMETypes: []string{"image/jpeg", "image/png"}}},
		shared.EntryTypeWebchat:  {},
	})

	fields := subscribedFields(t, hub, conn, "telegram")
	require.JSONEq(t, `[{"kind":"image","max_bytes":10485760,"mime_types":["image/jpeg","image/png"]}]`, string(fields["media_limits"]))
}

func TestAChannelKnownToTakeNoMediaSendsAnEmptyList(t *testing.T) {
	hub, conn := newActionHub(t, newFakeMessageAction())
	hub.SetMediaLimits(fixedMediaLimits{shared.EntryTypeWebchat: {}})

	fields := subscribedFields(t, hub, conn, "webchat")
	require.JSONEq(t, `[]`, string(fields["media_limits"]))
}

func TestUnknownMediaLimitsAreLeftOut(t *testing.T) {
	hub, conn := newActionHub(t, newFakeMessageAction())
	hub.SetMediaLimits(fixedMediaLimits{})
	_, present := subscribedFields(t, hub, conn, "telegram")["media_limits"]
	require.False(t, present, "a channel without known limits must not claim any")

	bare, bareConn := newActionHub(t, newFakeMessageAction())
	_, present = subscribedFields(t, bare, bareConn, "telegram")["media_limits"]
	require.False(t, present, "a hub without a limits reader must not claim any")
}
