package leadexit_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/leadexit"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type fakeUnofficialEntries struct {
	latest map[string]*uwc.Entry
	err    error
	calls  int
}

func (f *fakeUnofficialEntries) FindLatestByConversationID(conversationID string) (*uwc.Entry, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if e, ok := f.latest[conversationID]; ok {
		return e, nil
	}
	return nil, uwc.ErrEntryNotFound
}

type fakeStoredByID struct {
	byID map[string]*conversation.Message
	err  error
}

func (f fakeStoredByID) GetByID(id string) (*conversation.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	if m, ok := f.byID[id]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

var campaignSentAt = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

func sentEntry() *uwc.Entry {
	return &uwc.Entry{ID: "entry-u", CampaignID: "camp-u", ConversationID: "conv-1", MessageID: "msg-camp", SentAt: &campaignSentAt}
}

func campaignMessage(campaignID, entryID string) *conversation.Message {
	return &conversation.Message{
		ID: "msg-camp", EntryID: entryID, MessageType: conversation.MessageTypeOperator,
		SentBy: conversation.SentByCampaign(campaignID), Text: "Oi Ana! Responda /sair para não receber mais mensagens.",
	}
}

func campaignMessagesOf(t *testing.T, entries *fakeUnofficialEntries, messages fakeStoredByID) *UnofficialCampaignMessages {
	t.Helper()
	found, err := NewUnofficialCampaignMessages(UnofficialCampaignMessagesDeps{Entries: entries, Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestTheLastUnofficialCampaignMessageIsTheStoredTextOfTheLatestSentEntry(t *testing.T) {
	entries := &fakeUnofficialEntries{latest: map[string]*uwc.Entry{"conv-1": sentEntry()}}
	messages := fakeStoredByID{byID: map[string]*conversation.Message{"msg-camp": campaignMessage("camp-u", "conv-1")}}

	got, err := campaignMessagesOf(t, entries, messages).LastCampaignMessage(context.Background(), unofficialEntry, "conv-1")

	want := leadexit.SentCampaignMessage{MessageID: "msg-camp", CampaignID: "camp-u", Text: "Oi Ana! Responda /sair para não receber mais mensagens."}
	if err != nil || got == nil || *got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAnAudioCampaignShowsTheExitTextSentAfterIt(t *testing.T) {
	audio := sentEntry()
	audio.MessageID, audio.ExitMessageID = "msg-audio", "msg-exit"
	exit := campaignMessage("camp-u", "conv-1")
	exit.ID, exit.Text = "msg-exit", "Responda /sair para não receber mais"
	voice := campaignMessage("camp-u", "conv-1")
	voice.ID, voice.Text = "msg-audio", ""
	entries := &fakeUnofficialEntries{latest: map[string]*uwc.Entry{"conv-1": audio}}
	messages := fakeStoredByID{byID: map[string]*conversation.Message{"msg-exit": exit, "msg-audio": voice}}

	got, err := campaignMessagesOf(t, entries, messages).LastCampaignMessage(context.Background(), unofficialEntry, "conv-1")

	if err != nil || got == nil || got.MessageID != "msg-exit" || got.Text != "Responda /sair para não receber mais" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestNoUsableCampaignMessageMeansNone(t *testing.T) {
	unsent := sentEntry()
	unsent.SentAt = nil
	unrecorded := sentEntry()
	unrecorded.MessageID = ""
	cases := []struct {
		name     string
		entry    *uwc.Entry
		messages map[string]*conversation.Message
	}{
		{"no entry for the conversation", nil, nil},
		{"an entry not sent yet", unsent, map[string]*conversation.Message{"msg-camp": campaignMessage("camp-u", "conv-1")}},
		{"a send without its stored message", unrecorded, map[string]*conversation.Message{"msg-camp": campaignMessage("camp-u", "conv-1")}},
		{"a stored message that is gone", sentEntry(), nil},
		{"a message from another campaign", sentEntry(), map[string]*conversation.Message{"msg-camp": campaignMessage("camp-other", "conv-1")}},
		{"a message in another conversation", sentEntry(), map[string]*conversation.Message{"msg-camp": campaignMessage("camp-u", "conv-2")}},
	}
	for _, tc := range cases {
		entries := &fakeUnofficialEntries{latest: map[string]*uwc.Entry{}}
		if tc.entry != nil {
			entries.latest["conv-1"] = tc.entry
		}
		got, err := campaignMessagesOf(t, entries, fakeStoredByID{byID: tc.messages}).LastCampaignMessage(context.Background(), unofficialEntry, "conv-1")
		if err != nil || got != nil {
			t.Errorf("%s: got %+v, %v", tc.name, got, err)
		}
	}
}

func TestOtherChannelsHaveNoUnofficialCampaignMessage(t *testing.T) {
	entries := &fakeUnofficialEntries{latest: map[string]*uwc.Entry{"conv-1": sentEntry()}}

	got, err := campaignMessagesOf(t, entries, fakeStoredByID{}).LastCampaignMessage(context.Background(), "whatsapp", "conv-1")

	if err != nil || got != nil || entries.calls != 0 {
		t.Fatalf("got %+v, %v after %d lookups", got, err, entries.calls)
	}
}

func TestAFailedLookupIsAnError(t *testing.T) {
	broken := &fakeUnofficialEntries{err: errStoreDown}
	if _, err := campaignMessagesOf(t, broken, fakeStoredByID{}).LastCampaignMessage(context.Background(), unofficialEntry, "conv-1"); !errors.Is(err, errStoreDown) {
		t.Fatalf("entries down: got %v", err)
	}
	entries := &fakeUnofficialEntries{latest: map[string]*uwc.Entry{"conv-1": sentEntry()}}
	if _, err := campaignMessagesOf(t, entries, fakeStoredByID{err: errStoreDown}).LastCampaignMessage(context.Background(), unofficialEntry, "conv-1"); !errors.Is(err, errStoreDown) {
		t.Fatalf("messages down: got %v", err)
	}
}

func TestTheCampaignMessagesNeedTheirStores(t *testing.T) {
	if _, err := NewUnofficialCampaignMessages(UnofficialCampaignMessagesDeps{Entries: &fakeUnofficialEntries{}}); !errors.Is(err, leadexit.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}
