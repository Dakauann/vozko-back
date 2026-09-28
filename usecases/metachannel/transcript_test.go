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

type memoryFiles struct{ uploads []string }

func (f *memoryFiles) UploadFile(key string, _ []byte, _ string) error {
	f.uploads = append(f.uploads, key)
	return nil
}
func (f *memoryFiles) GetFileURL(key string) string { return "https://cdn/" + key }

type recordingBroadcast struct{ entries []string }

func (r *recordingBroadcast) BroadcastEntryUpdate(entryID, _ string, _ *conversation.Message) {
	r.entries = append(r.entries, entryID)
}

func newTranscript() (*Transcript, *recordingHistory, *memoryStore, *memoryFiles, *recordingBroadcast) {
	h, s, f, b := &recordingHistory{}, &memoryStore{byMID: map[string]*conversation.Message{}}, &memoryFiles{}, &recordingBroadcast{}
	return &Transcript{
		EntryType: shared.EntryTypeFacebook, Channel: conversation.MessageChannelFacebook, Prefix: "facebook",
		History: h, Messages: s, FileStorage: f, Broadcaster: b,
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

func TestEditReactionReadAndTombstone(t *testing.T) {
	tr, _, store, _, broadcast := newTranscript()
	store.byMID["m1"] = &conversation.Message{ID: "row-1", EntryID: "e1"}

	if err := tr.ApplyEdit("e1", &mm.MessageEdit{MID: "m1", Text: " new ", NumEdit: "2"}); err != nil {
		t.Fatal(err)
	}
	if store.byMID["m1"].Text != "new" {
		t.Fatalf("text = %q", store.byMID["m1"].Text)
	}
	if err := tr.ApplyReaction("e1", &mm.Reaction{MID: "m1", Action: "react", Reaction: "love", Emoji: "x"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tr.MarkRead("e1", "m1"); err != nil {
		t.Fatal(err)
	}
	if store.byMID["m1"].DeliveryStatus != conversation.DeliveryStatusRead {
		t.Fatal("read not applied")
	}
	if err := tr.Tombstone("e1", "m1"); err != nil {
		t.Fatal(err)
	}
	if len(store.updated) != 3 || len(store.deleted) != 1 || len(broadcast.entries) != 4 {
		t.Fatalf("updated=%v deleted=%v broadcasts=%v", store.updated, store.deleted, broadcast.entries)
	}
}

func TestMutationsOnUnknownMessagesAreNoOps(t *testing.T) {
	tr, _, store, _, broadcast := newTranscript()
	if err := tr.ApplyEdit("e1", &mm.MessageEdit{MID: "ghost"}); err != nil {
		t.Fatal(err)
	}
	if len(store.updated) != 0 || len(broadcast.entries) != 0 {
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
