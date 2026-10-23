package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type entryScopedMedia struct {
	conversation.ConversationMediaRepository
	rows    map[string]*conversation.ConversationMedia
	lookups int
}

func (m *entryScopedMedia) GetInEntry(entryID string, entryType shared.EntryType, id string) (*conversation.ConversationMedia, error) {
	m.lookups++
	row, ok := m.rows[id]
	if !ok || !row.BelongsTo(entryID, entryType) {
		return nil, conversation.ErrMediaNotFound
	}
	return row, nil
}

func (m *entryScopedMedia) GetByID(string) (*conversation.ConversationMedia, error) {
	panic("a send must resolve its media inside the conversation, never by id alone")
}

func conversationUploads(rows ...*conversation.ConversationMedia) *entryScopedMedia {
	byID := make(map[string]*conversation.ConversationMedia, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	return &entryScopedMedia{rows: byID}
}

func uploadIn(id, entryID string, entryType shared.EntryType, purpose conversation.MediaPurpose) *conversation.ConversationMedia {
	return &conversation.ConversationMedia{
		ID: id, EntryID: entryID, EntryType: entryType, Type: conversation.MediaTypeImage,
		MimeType: "image/jpeg", URL: "https://cdn/" + id + ".jpg", OriginalFilename: id + ".jpg", Purpose: purpose,
	}
}

func newAdapterMediaSender(uploads *entryScopedMedia) (*MessageSenderService, *campaignSendAdapter) {
	adapter := &campaignSendAdapter{entryType: shared.EntryTypeUnofficialWhatsApp}
	return &MessageSenderService{
		messageRepo:     &campaignMessageRepo{},
		mediaRepo:       uploads,
		channelAdapters: conversation.NewAdapterRegistry(adapter),
	}, adapter
}

func assertMediaNotSendable(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, conversation.ErrMediaNotSendable) {
		t.Fatalf("err = %v, want ErrMediaNotSendable", err)
	}
	if code := conversation.SendFailureCode(err, "send_failed"); code != conversation.MediaNotSendableCode {
		t.Fatalf("code = %q, want %q", code, conversation.MediaNotSendableCode)
	}
}

func TestOperatorMediaUploadedToThisConversationIsSent(t *testing.T) {
	uw := shared.EntryTypeUnofficialWhatsApp
	svc, adapter := newAdapterMediaSender(conversationUploads(uploadIn("m-1", "conv-1", uw, conversation.MediaPurposeMessage)))

	msg, err := svc.SendMediaMessage("conv-1", string(uw), "m-1", "image", "veja", conversation.SendOrigin{SenderUserID: "user-1"})
	if err != nil {
		t.Fatalf("SendMediaMessage: %v", err)
	}
	if msg == nil || len(adapter.requests) != 1 || adapter.requests[0].URL != "https://cdn/m-1.jpg" {
		t.Fatalf("msg=%v requests=%+v, want the upload delivered once", msg, adapter.requests)
	}
}

func TestOperatorMediaOfAnotherWorkspaceIsRefused(t *testing.T) {
	uw := shared.EntryTypeUnofficialWhatsApp
	svc, adapter := newAdapterMediaSender(conversationUploads(uploadIn("foreign", "conv-of-another-workspace", uw, conversation.MediaPurposeMessage)))

	_, err := svc.SendMediaMessage("conv-1", string(uw), "foreign", "image", "", conversation.SendOrigin{SenderUserID: "user-1"})
	assertMediaNotSendable(t, err)
	if len(adapter.requests) != 0 {
		t.Fatal("another workspace's file reached the contact")
	}
}

func TestOperatorCannotSendANoteImageToTheCustomer(t *testing.T) {
	uw := shared.EntryTypeUnofficialWhatsApp
	svc, adapter := newAdapterMediaSender(conversationUploads(uploadIn("note-1", "conv-1", uw, conversation.MediaPurposeNote)))

	_, err := svc.SendMediaMessage("conv-1", string(uw), "note-1", "image", "", conversation.SendOrigin{SenderUserID: "user-1"})
	assertMediaNotSendable(t, err)
	if len(adapter.requests) != 0 {
		t.Fatal("an internal note image reached the customer")
	}
}

func TestOperatorCannotSendALibraryIdAsIfItWereAnUpload(t *testing.T) {
	uw := shared.EntryTypeUnofficialWhatsApp
	svc, adapter := newAdapterMediaSender(conversationUploads())

	_, err := svc.SendMediaMessage("conv-1", string(uw), "lib-1", "image", "", conversation.SendOrigin{SenderUserID: "user-1"})
	assertMediaNotSendable(t, err)
	if len(adapter.requests) != 0 {
		t.Fatal("an id that is no upload of this conversation was sent")
	}
}

func TestOfficialWhatsAppRefusesMediaOutsideTheConversationBeforeTouchingMeta(t *testing.T) {
	cases := map[string]*conversation.ConversationMedia{
		"another conversation": uploadIn("m-1", "entry-of-another-workspace", shared.EntryTypeWhatsApp, conversation.MediaPurposeMessage),
		"note image":           uploadIn("m-1", "entry-1", shared.EntryTypeWhatsApp, conversation.MediaPurposeNote),
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			svc, msgRepo := newAudioSender(t, &conversation.SendTextMessageOutput{MessageID: "wamid.X"}, nil)
			svc.whatsappClientFactory = nil
			svc.mediaRepo = conversationUploads(row)

			_, err := svc.SendMediaMessage("entry-1", "whatsapp", "m-1", "image", "", conversation.SendOrigin{SenderUserID: "user-1"})
			assertMediaNotSendable(t, err)
			if len(msgRepo.created) != 0 {
				t.Fatal("a refused send recorded a message")
			}
		})
	}
}
