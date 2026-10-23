package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
)

type capturingMediaStore struct {
	received []conversation.StoreMediaInput
}

func (s *capturingMediaStore) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	s.received = append(s.received, in)
	return &conversation.ConversationMedia{ID: in.ID, Purpose: in.Purpose}, nil
}

func TestTheUploadHandsItsPurposeToTheMediaStore(t *testing.T) {
	store := &capturingMediaStore{}
	upload := NewUploadConversationMediaUseCase(store)

	for _, purpose := range []conversation.MediaPurpose{conversation.MediaPurposeNote, ""} {
		if _, err := upload.Execute(conversation.UploadMediaInput{
			EntryID: "entry-1", EntryType: "whatsapp", MediaType: conversation.MediaTypeImage,
			Filename: "a.png", Data: []byte("png"), MimeType: "image/png", Purpose: purpose,
		}); err != nil {
			t.Fatalf("upload: %v", err)
		}
	}
	if store.received[0].Purpose != conversation.MediaPurposeNote {
		t.Fatalf("note purpose was lost: %+v", store.received[0])
	}
	if store.received[1].Purpose != "" {
		t.Fatalf("a plain upload must leave the purpose to the store default, got %q", store.received[1].Purpose)
	}
}
