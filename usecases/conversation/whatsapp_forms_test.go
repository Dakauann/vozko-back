package conversation_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func formSender() (*MessageSenderService, *stubAudioMessageRepo) {
	messages := &stubAudioMessageRepo{}
	return &MessageSenderService{
		whatsappRepo:      stubAudioEntryRepo{},
		leadRepo:          stubAudioLeadRepo{},
		messageWindowRepo: stubAudioWindowRepo{},
		messageRepo:       messages,
	}, messages
}

func TestOfficialTargetResolvesTheEntryNumberAndWindow(t *testing.T) {
	svc, _ := formSender()
	target, err := svc.OfficialTarget("entry-1")
	if err != nil {
		t.Fatal(err)
	}
	if target.LeadID != "lead-1" || target.LeadNumber != "5511999999999" || target.BusinessPhoneID != "phone-1" || target.WorkspaceID != "ws-1" || !target.WindowOpen {
		t.Fatalf("target = %+v", target)
	}
}

func TestRecordFormMessageStoresAnOperatorOrAutomationMessageWithTheFormCard(t *testing.T) {
	svc, messages := formSender()
	card := map[string]any{"kind": "message", "formId": "form-1"}
	if _, err := svc.RecordFormMessage(FormMessageRecord{EntryID: "entry-1", To: "5511999999999", MessageID: "wamid.f", Body: "Preencha", SentBy: "user-1", Metadata: map[string]any{"form": card}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordFormMessage(FormMessageRecord{EntryID: "entry-1", To: "5511999999999", MessageID: "wamid.g", Body: "Preencha", Metadata: map[string]any{"form": card}}); err != nil {
		t.Fatal(err)
	}
	person, automated := messages.created[0], messages.created[1]
	if person.MessageType != conversation.MessageTypeOperator || person.SenderUserID() != "user-1" || person.WhatsAppMessageID == nil || *person.WhatsAppMessageID != "wamid.f" || person.Text != "Preencha" {
		t.Fatalf("person message = %+v", person)
	}
	if automated.MessageType != conversation.MessageTypeAIResponse || automated.EntryType != shared.EntryTypeWhatsApp {
		t.Fatalf("automation message = %+v", automated)
	}
	if len(person.Metadata) == 0 || len(automated.Metadata) == 0 {
		t.Fatal("both carry the form card")
	}
}

type mediaClient struct {
	conversation.WhatsAppClient
	data []byte
	mime string
	err  error
}

func (c mediaClient) DownloadMedia(context.Context, string) ([]byte, string, error) {
	return c.data, c.mime, c.err
}

type capturingStore struct {
	stored []conversation.StoreMediaInput
}

func (s *capturingStore) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	s.stored = append(s.stored, in)
	return &conversation.ConversationMedia{ID: in.ID}, nil
}

func TestFormFilesAreDownloadedCheckedAndStored(t *testing.T) {
	data := []byte("%PDF-1.7 bytes")
	sum := sha256.Sum256(data)
	store := &capturingStore{}
	files := NewFormFileStore(stubAudioClientFactory{client: mediaClient{data: data, mime: "application/pdf"}}, store, func() string { return "media-1" })
	id, err := files.Store(context.Background(), FormFile{BusinessPhoneID: "phone-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, MediaID: "meta-1", Name: "rg.pdf", MimeType: "application/pdf", SHA256: hex.EncodeToString(sum[:])})
	if err != nil || id != "media-1" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	in := store.stored[0]
	if in.Key != "conversations/whatsapp/entry-1/media-1.pdf" || in.Type != conversation.MediaTypeDocument || in.WhatsAppMediaID != "meta-1" || in.OriginalFilename != "rg.pdf" {
		t.Fatalf("stored = %+v", in)
	}
}

func TestAFormFileWhoseHashDoesNotMatchIsRefused(t *testing.T) {
	store := &capturingStore{}
	files := NewFormFileStore(stubAudioClientFactory{client: mediaClient{data: []byte("x"), mime: "image/jpeg"}}, store, func() string { return "media-1" })
	_, err := files.Store(context.Background(), FormFile{BusinessPhoneID: "phone-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, MediaID: "meta-1", SHA256: "deadbeef"})
	if !errors.Is(err, ErrFormFileIntegrity) || len(store.stored) != 0 {
		t.Fatalf("err = %v stored = %d", err, len(store.stored))
	}
}

func TestAPhotoIsStoredAsAnImage(t *testing.T) {
	store := &capturingStore{}
	files := NewFormFileStore(stubAudioClientFactory{client: mediaClient{data: []byte("jpg"), mime: "image/jpeg"}}, store, func() string { return "media-2" })
	if _, err := files.Store(context.Background(), FormFile{BusinessPhoneID: "phone-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, MediaID: "meta-2"}); err != nil {
		t.Fatal(err)
	}
	if store.stored[0].Type != conversation.MediaTypeImage {
		t.Fatalf("stored = %+v", store.stored[0])
	}
}
