package messageaction

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestEachConversationGetsTheCapabilitiesOfItsOwnChannel(t *testing.T) {
	channels := Channels{
		Adapters: conversation.NewAdapterRegistry(&actingChannel{entryType: shared.EntryTypeTelegram}, reactOnlyChannel{}),
		Official: reactOnlyChannel{},
	}
	cases := map[shared.EntryType]conversation.MessageCapabilities{
		shared.EntryTypeTelegram:  {React: true, Edit: true, Delete: true, EditWindowSeconds: 900, DeleteWindowSeconds: 172800},
		shared.EntryTypeInstagram: {React: true},
		shared.EntryTypeWhatsApp:  {React: true},
		shared.EntryTypeWebchat:   {},
		"pigeon":                  {},
	}
	for entryType, want := range cases {
		t.Run(string(entryType), func(t *testing.T) {
			if got := channels.MessageCapabilities(entryType); got != want {
				t.Fatalf("capabilities = %+v, want %+v", got, want)
			}
		})
	}
}

func TestWithoutChannelsNothingIsAllowed(t *testing.T) {
	if got := (Channels{}).MessageCapabilities(shared.EntryTypeTelegram); got != (conversation.MessageCapabilities{}) {
		t.Fatalf("capabilities = %+v, want none", got)
	}
	if got := (Channels{}).MessageCapabilities(shared.EntryTypeWhatsApp); got != (conversation.MessageCapabilities{}) {
		t.Fatalf("capabilities = %+v, want none", got)
	}
}
