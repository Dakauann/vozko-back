package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// fakeConvMedia stands in for the conversation_media store.
//
// Create deliberately does NOT invent an id for a row that arrived without one,
// because the real repository does not either: it maps the domain value onto a
// schema struct and the database hook mints the id onto THAT copy. A fake that
// helpfully filled the field in would hide the exact bug these tests cover.
type fakeConvMedia struct {
	mu   sync.Mutex
	rows []*conversation.ConversationMedia
}

func (f *fakeConvMedia) Create(media *conversation.ConversationMedia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := *media
	f.rows = append(f.rows, &clone)
	return nil
}

func (f *fakeConvMedia) GetByID(id string) (*conversation.ConversationMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return nil, conversation.ErrMediaNotFound
}

func (f *fakeConvMedia) GetByWhatsAppMediaID(string) (*conversation.ConversationMedia, error) {
	return nil, conversation.ErrMediaNotFound
}

func (f *fakeConvMedia) ListByEntry(string, shared.EntryType) ([]*conversation.ConversationMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*conversation.ConversationMedia(nil), f.rows...), nil
}

func (f *fakeConvMedia) Delete(string) error                          { return nil }
func (f *fakeConvMedia) DeleteByEntry(string, shared.EntryType) error { return nil }

func (f *fakeConvMedia) all() []*conversation.ConversationMedia {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*conversation.ConversationMedia(nil), f.rows...)
}

// mediaHarness wires the ports an inbound attachment actually touches.
type mediaHarness struct {
	uc      *HandleWebhookUseCase
	storage *fakeStorage
	media   *fakeConvMedia
	history *recordingHistory
}

func newMediaHarness(t *testing.T) *mediaHarness {
	t.Helper()

	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-1",
		Status: uw.StatusConnected, PhoneNumber: "5599999999999",
	}
	h := &mediaHarness{
		storage: newFakeStorage(),
		media:   &fakeConvMedia{},
		history: &recordingHistory{},
	}
	h.uc = NewHandleWebhookUseCase(HandleWebhookDeps{
		Instances:     newFakeInstanceRepo(instance),
		Servers:       newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		Contacts:      newFakeContactRepo(),
		Conversations: newFakeConversationRepo(),
		Groups:        newFakeGroupRepo(),
		Messaging:     &fakeMessaging{},
		GroupAPI:      &fakeGroupAPI{},
		Assets:        &fakeAssets{},
		FileStorage:   h.storage,
		ConvMedia:     h.media,
		History:       h.history,
	})
	return h
}

func (h *mediaHarness) deliver(t *testing.T, msg map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1", "data": msg,
	})
	if err != nil {
		t.Fatalf("encoding the provider message: %v", err)
	}
	if err := h.uc.Execute(context.Background(), &QueuedEvent{
		InstanceID: "inst-1", Body: body,
	}); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
}

func mediaMessage(messageType, mimeType, fileName string) map[string]any {
	return map[string]any{
		"messageid":        "msg-" + messageType,
		"chatid":           "5511111111111@s.whatsapp.net",
		"sender":           "5511111111111@s.whatsapp.net",
		"sender_pn":        "5511111111111@s.whatsapp.net",
		"senderName":       "Ana",
		"messageType":      messageType,
		"mimetype":         mimeType,
		"fileName":         fileName,
		"messageTimestamp": time.Now().UnixMilli(),
	}
}

// An inbound attachment must reach the message row that renders it.
//
// This is the bug that made every photo, voice note and document invisible in
// the CRM on this channel. The bytes were fetched and uploaded correctly and the
// conversation_media row was written — but the id was read back off a struct the
// repository never wrote to, so MediaID arrived empty, the history manager
// dropped MediaType with it, and the operator got a bare "[imagem]" placeholder
// next to an orphaned object in storage.
func TestInboundAttachmentLinksMediaToMessage(t *testing.T) {
	h := newMediaHarness(t)

	h.deliver(t, mediaMessage("image", "image/jpeg", "foto.jpg"))

	records := h.history.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 recorded message, got %d", len(records))
	}
	rec := records[0]

	if rec.MediaID == "" {
		t.Error("MediaID is empty: the message cannot resolve its own attachment, so the CRM renders no media at all")
	}
	if rec.MediaType != conversation.MediaTypeImage {
		t.Errorf("MediaType = %q, want %q", rec.MediaType, conversation.MediaTypeImage)
	}
	if rec.MediaURL == "" {
		t.Error("MediaURL is empty")
	}

	rows := h.media.all()
	if len(rows) != 1 {
		t.Fatalf("expected 1 conversation_media row, got %d", len(rows))
	}
	if rows[0].ID == "" {
		t.Fatal("the stored row carries no id, so nothing can ever reference it")
	}
	// The id on the message and the id on the row must be the SAME id. A row
	// that exists under a different id is exactly as invisible as no row.
	if rows[0].ID != rec.MediaID {
		t.Errorf("message points at media %q but the row was stored as %q", rec.MediaID, rows[0].ID)
	}
	if rows[0].EntryType != shared.EntryTypeUnofficialWhatsApp {
		t.Errorf("EntryType = %q, want %q", rows[0].EntryType, shared.EntryTypeUnofficialWhatsApp)
	}
	// GetMedia refuses any row whose entry does not match the URL it was asked
	// under, so a mismatch here is a 403 on every attachment in the thread.
	if rows[0].EntryID != rec.EntryID {
		t.Errorf("media EntryID = %q, message EntryID = %q; the media endpoint would reject this", rows[0].EntryID, rec.EntryID)
	}

	// The avatar fetch uploads too, so this looks for the attachment rather than
	// counting objects.
	var attachmentKey string
	for _, key := range h.storage.keys() {
		if strings.HasPrefix(key, "conversations/unofficial_whatsapp/") {
			attachmentKey = key
		}
	}
	if attachmentKey == "" {
		t.Fatalf("no attachment was uploaded under the channel's prefix, got %v", h.storage.keys())
	}
	if !strings.HasSuffix(attachmentKey, ".jpg") {
		t.Errorf("object key %q lost its extension, so the CDN serves it with the wrong type", attachmentKey)
	}
	if rows[0].URL != rec.MediaURL {
		t.Errorf("row URL %q and message URL %q disagree", rows[0].URL, rec.MediaURL)
	}
}

// Every attachment kind the channel normalizes has to survive the same trip.
//
// Audio is called out separately in the symptom report and takes a different
// message type (MessageTypeAudio, the speech-to-text route) than the rest, so a
// regression could easily land on one kind and not the others.
func TestEveryAttachmentKindCarriesItsMedia(t *testing.T) {
	cases := []struct {
		messageType string
		mimeType    string
		fileName    string
		want        conversation.MediaType
	}{
		{"image", "image/jpeg", "foto.jpg", conversation.MediaTypeImage},
		{"video", "video/mp4", "clipe.mp4", conversation.MediaTypeVideo},
		{"audio", "audio/mpeg", "musica.mp3", conversation.MediaTypeAudio},
		{"ptt", "audio/ogg", "", conversation.MediaTypeAudio},
		{"document", "application/pdf", "contrato.pdf", conversation.MediaTypeDocument},
		{"sticker", "image/webp", "", conversation.MediaTypeSticker},
	}

	for _, tc := range cases {
		t.Run(tc.messageType, func(t *testing.T) {
			h := newMediaHarness(t)
			h.deliver(t, mediaMessage(tc.messageType, tc.mimeType, tc.fileName))

			records := h.history.all()
			if len(records) != 1 {
				t.Fatalf("expected 1 recorded message, got %d", len(records))
			}
			if got := records[0].MediaType; got != tc.want {
				t.Errorf("MediaType = %q, want %q", got, tc.want)
			}
			if records[0].MediaID == "" {
				t.Errorf("%s arrived with no MediaID, so it renders as a placeholder", tc.messageType)
			}
		})
	}
}

// A failed download must still produce a visible message.
//
// storeAttachment degrades on purpose, and the placeholder is the only thing
// standing between a fetch failure and a turn that vanishes from the transcript.
func TestFailedAttachmentDownloadStillRecordsAPlaceholder(t *testing.T) {
	h := newMediaHarness(t)
	// No ConvMedia and no storage: the closest stand-in for a download that
	// yields nothing usable.
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-1",
		Status: uw.StatusConnected, PhoneNumber: "5599999999999",
	}
	h.uc = NewHandleWebhookUseCase(HandleWebhookDeps{
		Instances:     newFakeInstanceRepo(instance),
		Servers:       newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		Contacts:      newFakeContactRepo(),
		Conversations: newFakeConversationRepo(),
		Groups:        newFakeGroupRepo(),
		Messaging:     &fakeMessaging{},
		GroupAPI:      &fakeGroupAPI{},
		Assets:        &fakeAssets{},
		History:       h.history,
	})

	h.deliver(t, mediaMessage("image", "image/jpeg", "foto.jpg"))

	records := h.history.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 recorded message, got %d", len(records))
	}
	if records[0].Text == "" {
		t.Error("a message with neither media nor text would be rejected and then lost")
	}
}
