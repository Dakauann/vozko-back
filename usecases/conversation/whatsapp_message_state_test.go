package conversation_usecase

import (
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type wamidMessages struct {
	conversation.MessageRepository
	byWamid map[string]*conversation.Message
	byID    map[string]*conversation.Message
	updated []string
}

func (r *wamidMessages) GetByWhatsAppMessageID(wamid string) (*conversation.Message, error) {
	if m, ok := r.byWamid[wamid]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (r *wamidMessages) GetByEntryAndExternalMessageID(shared.EntryType, string, string) (*conversation.Message, error) {
	return nil, conversation.ErrMessageNotFound
}

func (r *wamidMessages) GetByID(id string) (*conversation.Message, error) {
	if m, ok := r.byID[id]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (r *wamidMessages) Update(messageID string, _ *conversation.Message) error {
	r.updated = append(r.updated, messageID)
	return nil
}

type messageUpdates struct {
	conversation.EventBroadcaster
	updated []*conversation.Message
	entries []string
}

func (b *messageUpdates) BroadcastMessageUpdated(_, _ string, message *conversation.Message) {
	b.updated = append(b.updated, message)
}

func (b *messageUpdates) BroadcastEntryUpdate(entryID, _ string, _ *conversation.Message) {
	b.entries = append(b.entries, entryID)
}

func ourReply(wamid string) *conversation.Message {
	return &conversation.Message{
		ID: "row-" + wamid, EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeOperator, From: "user-1", To: "5511999990000", WhatsAppMessageID: &wamid,
	}
}

func reactionPayload(t *testing.T, from, target, emoji string) *conversation.WhatsAppWebhookPayload {
	t.Helper()
	return locationPayload(t, `{"from":"`+from+`","id":"wamid.react","timestamp":"1792152000","type":"reaction",`+
		`"reaction":{"message_id":"`+target+`","emoji":"`+emoji+`"}}`)
}

func reactionFixture() (*handleWhatsAppMessageUseCase, *wamidMessages, *messageUpdates, *conversation.Message) {
	target := ourReply("wamid.out")
	messages := &wamidMessages{byWamid: map[string]*conversation.Message{"wamid.out": target}}
	hub := &messageUpdates{}
	return &handleWhatsAppMessageUseCase{messageRepo: messages, hub: hub}, messages, hub, target
}

func TestAnOfficialInboundReactionIsFoldedOntoTheMessageItTargets(t *testing.T) {
	uc, messages, hub, target := reactionFixture()

	uc.foldInboundReactions(reactionPayload(t, "5511999990000", "wamid.out", "👍"))

	want := conversation.Reaction{Emoji: "👍", Direction: conversation.ReactionInbound, ActorID: "5511999990000", At: time.Unix(1792152000, 0).UTC()}
	if got := target.Reactions(); len(got) != 1 || got[0] != want {
		t.Fatalf("reactions = %+v, want [%+v]", got, want)
	}
	if len(messages.updated) != 1 || len(hub.updated) != 1 || len(hub.entries) != 1 {
		t.Fatalf("updated=%v message_updated=%d entry_update=%v", messages.updated, len(hub.updated), hub.entries)
	}

	uc.foldInboundReactions(reactionPayload(t, "5511999990000", "wamid.out", ""))
	if got := target.Reactions(); len(got) != 0 {
		t.Fatalf("an empty emoji removes the reaction: %+v", got)
	}
}

func TestAReactionFromSomeoneOutsideTheConversationIsIgnored(t *testing.T) {
	uc, messages, hub, target := reactionFixture()

	uc.foldInboundReactions(reactionPayload(t, "5521888887777", "wamid.out", "👍"))
	uc.foldInboundReactions(reactionPayload(t, "5511999990000", "wamid.unknown", "👍"))

	if len(target.Reactions()) != 0 || len(messages.updated) != 0 || len(hub.updated) != 0 {
		t.Fatalf("reactions=%+v updated=%v", target.Reactions(), messages.updated)
	}
}

func TestAQuotedInboundMessageCarriesTheQuotedProviderID(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	record := uc.inboundTextRecord(sairTap(), inboundTarget{entryID: "entry-1", entryType: shared.EntryTypeWhatsApp}, nil)
	if record.ReplyToWAMessageID != "wamid.tmpl" {
		t.Fatalf("reply_to = %q, want the quoted wamid for the history manager to resolve", record.ReplyToWAMessageID)
	}
	if got := quotedProviderMessageID(&conversation.WhatsAppMessage{}); got != "" {
		t.Fatalf("a message without context quotes nothing, got %q", got)
	}
}

func TestATranscriptIsStoredBesideTheAudioAndReachesTheOpenThread(t *testing.T) {
	audio := &conversation.Message{ID: "row-audio", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, MessageType: conversation.MessageTypeAudio, Text: conversation.AudioPlaceholder}
	messages := &wamidMessages{byID: map[string]*conversation.Message{"row-audio": audio}}
	hub := &messageUpdates{}
	uc := &handleWhatsAppMessageUseCase{messageRepo: messages, hub: hub}

	uc.storeTranscript("row-audio", " quero o plano ")

	if audio.Text != conversation.AudioPlaceholder || audio.Transcript != "quero o plano" {
		t.Fatalf("text = %q transcript = %q, the transcript must not overwrite the text", audio.Text, audio.Transcript)
	}
	if audio.ReadableText() != conversation.AudioPlaceholder+" quero o plano" {
		t.Fatalf("readers must still understand the audio: %q", audio.ReadableText())
	}
	if len(messages.updated) != 1 || len(hub.updated) != 1 || hub.updated[0].Transcript != "quero o plano" {
		t.Fatalf("updated=%v message_updated=%+v", messages.updated, hub.updated)
	}
}
