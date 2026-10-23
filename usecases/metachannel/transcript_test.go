package metachannel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	mm "vozko/domain/metamessaging"
	"vozko/domain/shared"
	"vozko/usecases/conversationmedia"
)

type recordingHistory struct {
	records []conversation.MessageHistoryRecord
}

func (r *recordingHistory) Record(_ context.Context, rec conversation.MessageHistoryRecord) error {
	r.records = append(r.records, rec)
	return nil
}

type memoryStore struct {
	byMID   map[string]*conversation.Message
	updated []string
	deleted []string
}

func (m *memoryStore) GetByExternalMessageID(_ shared.EntryType, mid string) (*conversation.Message, error) {
	if msg, ok := m.byMID[mid]; ok {
		return msg, nil
	}
	return nil, conversation.ErrMessageNotFound
}
func (m *memoryStore) GetByEntryAndExternalMessageID(_ shared.EntryType, entryID, mid string) (*conversation.Message, error) {
	if msg, ok := m.byMID[mid]; ok && msg.EntryID == entryID {
		return msg, nil
	}
	return nil, conversation.ErrMessageNotFound
}
func (m *memoryStore) Update(id string, _ *conversation.Message) error {
	m.updated = append(m.updated, id)
	return nil
}
func (m *memoryStore) Delete(id string) error {
	m.deleted = append(m.deleted, id)
	return nil
}

type memoryRows struct {
	rows []*conversation.ConversationMedia
}

func (m *memoryRows) Create(media *conversation.ConversationMedia) error {
	m.rows = append(m.rows, media)
	return nil
}
func (m *memoryRows) GetByID(string) (*conversation.ConversationMedia, error) {
	return nil, conversation.ErrMediaNotFound
}
func (m *memoryRows) ListByIDs([]string) ([]*conversation.ConversationMedia, error) { return nil, nil }
func (m *memoryRows) GetInEntry(string, shared.EntryType, string) (*conversation.ConversationMedia, error) {
	return nil, conversation.ErrMediaNotFound
}

func (m *memoryRows) GetByWhatsAppMediaID(string) (*conversation.ConversationMedia, error) {
	return nil, conversation.ErrMediaNotFound
}
func (m *memoryRows) ListByEntry(string, shared.EntryType) ([]*conversation.ConversationMedia, error) {
	return m.rows, nil
}
func (m *memoryRows) Delete(string) error                          { return nil }
func (m *memoryRows) DeleteByEntry(string, shared.EntryType) error { return nil }

type blindInspector struct{}

func (blindInspector) Inspect([]byte, conversation.MediaType) conversation.MediaLayout {
	return conversation.MediaLayout{}
}

type memoryFiles struct{ uploads []string }

func (f *memoryFiles) UploadFile(key string, _ []byte, _ string) error {
	f.uploads = append(f.uploads, key)
	return nil
}
func (f *memoryFiles) GetFileURL(key string) string { return "https://cdn/" + key }

type recordingBroadcast struct {
	entries []string
	updated []string
}

func (r *recordingBroadcast) BroadcastMessageUpdated(_, _ string, message *conversation.Message) {
	r.updated = append(r.updated, message.ID)
}

func (r *recordingBroadcast) BroadcastEntryUpdate(entryID, _ string, _ *conversation.Message) {
	r.entries = append(r.entries, entryID)
}

func newTranscript() (*Transcript, *recordingHistory, *memoryStore, *memoryFiles, *recordingBroadcast) {
	h, s, f, b := &recordingHistory{}, &memoryStore{byMID: map[string]*conversation.Message{}}, &memoryFiles{}, &recordingBroadcast{}
	return &Transcript{
		EntryType: shared.EntryTypeFacebook, Channel: conversation.MessageChannelFacebook, Prefix: "facebook",
		History: h, Messages: s, Media: conversationmedia.NewStore(f, &memoryRows{}, blindInspector{}), Broadcaster: b,
		Fetch: func(context.Context, string) ([]byte, string, error) { return []byte("x"), "image/jpeg", nil },
	}, h, s, f, b
}

func TestTextWithTwoAttachmentsBecomesThreeRowsWithDistinctIDs(t *testing.T) {
	tr, h, _, files, _ := newTranscript()
	msg := &mm.Message{MID: "m1", Text: " hi ", Attachments: []*mm.Attachment{
		{Type: "image", Payload: &mm.AttachmentPayload{URL: "https://a"}},
		{Type: "file", Payload: &mm.AttachmentPayload{URL: "https://b"}},
	}}
	text, err := tr.RecordMessage(context.Background(), "e1", conversation.SentByContact("psid"), msg, time.Now(), Party{From: "psid", To: "page"})
	if err != nil || text != "hi" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if len(h.records) != 3 || len(files.uploads) != 2 {
		t.Fatalf("records=%d uploads=%d", len(h.records), len(files.uploads))
	}
	ids := map[string]bool{}
	for _, r := range h.records {
		ids[r.ProviderMessageID] = true
		if r.EntryType != shared.EntryTypeFacebook || r.Channel != conversation.MessageChannelFacebook {
			t.Fatalf("record = %+v", r)
		}
	}
	if len(ids) != 3 || !ids["m1"] || !ids["m1:att0"] || !ids["m1:att1"] {
		t.Fatalf("provider ids = %v", ids)
	}
}

func TestStickerIsStoredAsAnImageRow(t *testing.T) {
	tr, h, _, files, _ := newTranscript()
	msg := &mm.Message{MID: "m2", Attachments: []*mm.Attachment{{Type: "sticker", Payload: &mm.AttachmentPayload{URL: "https://st", StickerID: "369239263222822"}}}}
	if _, err := tr.RecordMessage(context.Background(), "e1", conversation.SentByContact("psid"), msg, time.Now(), Party{}); err != nil {
		t.Fatal(err)
	}
	if len(h.records) != 1 || h.records[0].MessageType != conversation.MessageTypeSticker || h.records[0].ProviderMessageID != "m2" || len(files.uploads) != 1 {
		t.Fatalf("records = %+v", h.records)
	}
}

func TestLinkShareWithoutMediaKeepsNoPlaceholder(t *testing.T) {
	tr, h, _, _, _ := newTranscript()
	msg := &mm.Message{MID: "m3", Attachments: []*mm.Attachment{{Type: "fallback"}}}
	if _, err := tr.RecordMessage(context.Background(), "e1", conversation.SentByContact("psid"), msg, time.Now(), Party{}); err != nil {
		t.Fatal(err)
	}
	if len(h.records) != 1 || h.records[0].MessageType != conversation.MessageTypeLinkShare || h.records[0].Text != "" {
		t.Fatalf("record = %+v", h.records[0])
	}
}

func TestEmptyMessageBecomesUnsupported(t *testing.T) {
	tr, h, _, _, _ := newTranscript()
	if _, err := tr.RecordMessage(context.Background(), "e1", conversation.SentByContact("psid"), &mm.Message{MID: "m4"}, time.Now(), Party{}); err != nil {
		t.Fatal(err)
	}
	if h.records[0].MessageType != conversation.MessageTypeUnsupported || h.records[0].Text == "" {
		t.Fatalf("record = %+v", h.records[0])
	}
}

func TestFailedDownloadStillRecordsTheText(t *testing.T) {
	tr, h, _, _, _ := newTranscript()
	tr.Fetch = func(context.Context, string) ([]byte, string, error) { return nil, "", errors.New("expired") }
	msg := &mm.Message{MID: "m5", Text: "look", Attachments: []*mm.Attachment{{Type: "image", Payload: &mm.AttachmentPayload{URL: "https://a"}}}}
	if _, err := tr.RecordMessage(context.Background(), "e1", conversation.SentByContact("psid"), msg, time.Now(), Party{}); err != nil {
		t.Fatal(err)
	}
	if len(h.records) != 1 || h.records[0].Text != "look" {
		t.Fatalf("records = %+v", h.records)
	}
}

func TestAnEditReplacesTheTextAndReachesTheOpenThread(t *testing.T) {
	tr, _, store, _, broadcast := newTranscript()
	store.byMID["m1"] = &conversation.Message{ID: "row-1", EntryID: "e1", EntryType: shared.EntryTypeFacebook}
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	if err := tr.ApplyEdit("e1", &mm.MessageEdit{MID: "m1", Text: " new ", NumEdit: "2"}, at); err != nil {
		t.Fatal(err)
	}

	edited := store.byMID["m1"]
	if edited.Text != "new" || edited.EditedAt == nil || !edited.EditedAt.Equal(at) {
		t.Fatalf("text = %q edited_at = %v", edited.Text, edited.EditedAt)
	}
	if len(store.updated) != 1 || strings.Join(broadcast.updated, ",") != "row-1" || strings.Join(broadcast.entries, ",") != "e1" {
		t.Fatalf("updated=%v message_updated=%v entry_update=%v", store.updated, broadcast.updated, broadcast.entries)
	}
}

func TestAReactionIsFoldedOntoTheMessageInTheOneShape(t *testing.T) {
	tr, _, store, _, broadcast := newTranscript()
	store.byMID["m1"] = &conversation.Message{ID: "row-1", EntryID: "e1", EntryType: shared.EntryTypeFacebook}
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	if err := tr.ApplyReaction("e1", &mm.Reaction{MID: "m1", Action: "react", Reaction: "love", Emoji: "❤️"}, "psid-9", at); err != nil {
		t.Fatal(err)
	}
	want := conversation.Reaction{Emoji: "❤️", Direction: conversation.ReactionInbound, ActorID: "psid-9", At: at}
	if got := store.byMID["m1"].Reactions(); len(got) != 1 || got[0] != want {
		t.Fatalf("reactions = %+v, want [%+v]", got, want)
	}

	if err := tr.ApplyReaction("e1", &mm.Reaction{MID: "m1", Action: "react", Reaction: "like"}, "psid-9", at); err != nil {
		t.Fatal(err)
	}
	if got := store.byMID["m1"].Reactions(); len(got) != 1 || got[0].Emoji != "like" {
		t.Fatalf("a reaction without emoji falls back to its name and replaces the previous one: %+v", got)
	}

	if err := tr.ApplyReaction("e1", &mm.Reaction{MID: "m1", Action: "unreact", Reaction: "like", Emoji: "👍"}, "psid-9", at); err != nil {
		t.Fatal(err)
	}
	if got := store.byMID["m1"].Reactions(); len(got) != 0 {
		t.Fatalf("an unreact must remove the reaction: %+v", got)
	}
	if len(broadcast.updated) != 3 {
		t.Fatalf("each reaction change must reach the open thread: %v", broadcast.updated)
	}
}

func TestADeletedMessageIsRetractedNotRemoved(t *testing.T) {
	tr, _, store, _, broadcast := newTranscript()
	store.byMID["m1"] = &conversation.Message{ID: "row-1", EntryID: "e1", EntryType: shared.EntryTypeFacebook, Text: "oi"}
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	if err := tr.Retract("e1", "m1", at); err != nil {
		t.Fatal(err)
	}

	if retracted := store.byMID["m1"]; !retracted.IsRetracted() || !retracted.RetractedAt.Equal(at) {
		t.Fatalf("retracted_at = %v", retracted.RetractedAt)
	}
	if len(store.deleted) != 0 || len(store.updated) != 1 || strings.Join(broadcast.updated, ",") != "row-1" {
		t.Fatalf("deleted=%v updated=%v message_updated=%v", store.deleted, store.updated, broadcast.updated)
	}
}

func TestAReadReceiptRefreshesOnlyTheInboxRow(t *testing.T) {
	tr, _, store, _, broadcast := newTranscript()
	store.byMID["m1"] = &conversation.Message{ID: "row-1", EntryID: "e1"}

	if err := tr.MarkRead("e1", "m1"); err != nil {
		t.Fatal(err)
	}

	if store.byMID["m1"].DeliveryStatus != conversation.DeliveryStatusRead {
		t.Fatal("read not applied")
	}
	if len(broadcast.entries) != 1 || len(broadcast.updated) != 0 {
		t.Fatalf("entry_update=%v message_updated=%v", broadcast.entries, broadcast.updated)
	}
}

func TestMutationsOnUnknownMessagesAreNoOps(t *testing.T) {
	tr, _, store, _, broadcast := newTranscript()
	if err := tr.ApplyEdit("e1", &mm.MessageEdit{MID: "ghost"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tr.Retract("e1", "ghost", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(store.updated) != 0 || len(broadcast.entries) != 0 || len(broadcast.updated) != 0 {
		t.Fatal("unknown message touched")
	}
}

func TestFindIsScopedToTheEntryWhenKnown(t *testing.T) {
	tr, _, store, _, _ := newTranscript()
	store.byMID["m1"] = &conversation.Message{ID: "row-1", EntryID: "e1"}
	if _, err := tr.Find("e2", "m1"); !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("cross-entry lookup matched: %v", err)
	}
	if m, err := tr.Find("", "m1"); err != nil || m.ID != "row-1" {
		t.Fatalf("channel-wide lookup = %v, %v", m, err)
	}
}

func TestRecordOptionsOverrideThePlainTypeAndAddMetadata(t *testing.T) {
	tr, h, _, _, _ := newTranscript()
	msg := &mm.Message{MID: "e1", Text: "sent from inbox"}
	if _, err := tr.RecordMessage(context.Background(), "e1", conversation.SentExternally(), msg, time.Now(), Party{},
		AsType(conversation.MessageTypeOperator), WithMetadata(map[string]any{"facebook_sent_via": "meta_business_suite"})); err != nil {
		t.Fatal(err)
	}
	rec := h.records[0]
	if rec.MessageType != conversation.MessageTypeOperator || !strings.Contains(string(rec.Metadata), `"facebook_sent_via":"meta_business_suite"`) {
		t.Fatalf("record = %+v metadata=%s", rec, rec.Metadata)
	}
}

func TestStoryEventsWithoutTextKeepNoPlaceholder(t *testing.T) {
	cases := map[string]*mm.Message{
		"mention": {MID: "s1", Attachments: []*mm.Attachment{{Type: "story_mention", Payload: &mm.AttachmentPayload{URL: "https://cdn/story"}}}},
		"reply":   {MID: "s2", ReplyTo: &mm.ReplyTo{Story: &mm.Story{ID: "st", URL: "https://cdn/story"}}},
	}
	for name, msg := range cases {
		tr, h, _, files, _ := newTranscript()
		if _, err := tr.RecordMessage(context.Background(), "e1", conversation.SentByContact("igsid"), msg, time.Now(), Party{}); err != nil {
			t.Fatal(err)
		}
		if len(h.records) != 1 || h.records[0].Text != "" || h.records[0].MessageType == conversation.MessageTypeUnsupported {
			t.Errorf("%s: record = %+v", name, h.records[0])
		}
		if len(files.uploads) != 0 {
			t.Errorf("%s: story media must never be stored, uploads = %v", name, files.uploads)
		}
	}
}

func TestTheAdTravelsOnTheFirstRowOfAMessageOnly(t *testing.T) {
	tr, h, _, _, _ := newTranscript()
	tr.EntryType = shared.EntryTypeInstagram
	msg := &mm.Message{MID: "m3", Text: "quero saber", Referral: &mm.Referral{Source: "ADS", AdID: "9", AdsContextData: &mm.AdsContextData{AdTitle: "Coleção"}},
		Attachments: []*mm.Attachment{{Type: "image", Payload: &mm.AttachmentPayload{URL: "https://a"}}}}
	if _, err := tr.RecordMessage(context.Background(), "e1", conversation.SentByContact("igsid"), msg, time.Now(), Party{}); err != nil {
		t.Fatal(err)
	}
	if len(h.records) != 2 {
		t.Fatalf("records = %d", len(h.records))
	}
	if ad := h.records[0].AdReferral; ad == nil || ad.AdID != "9" || ad.Platform != conversation.AdPlatformInstagram {
		t.Fatalf("first row ad = %+v", ad)
	}
	if h.records[1].AdReferral != nil {
		t.Fatal("the attachment row must not repeat the ad")
	}
}
