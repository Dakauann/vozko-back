package conversation

import (
	"testing"

	"vozko/domain/shared"
)

func TestAStoredEmailBodyIsValidMediaButNeverASendableUpload(t *testing.T) {
	body := &ConversationMedia{ID: "m1", EntryID: "conv-1", EntryType: shared.EntryTypeEmail, Type: MediaTypeDocument, URL: "https://cdn/x.html", Purpose: MediaPurposeEmailBody}
	if err := body.Validate(); err != nil {
		t.Fatalf("validate = %v, want an email body to be a valid purpose", err)
	}
	if body.SendableIn("conv-1", shared.EntryTypeEmail) {
		t.Fatal("the stored html of an email must never be sent as an upload")
	}
	if _, err := SendableUpload(entryMedia{body}, "conv-1", shared.EntryTypeEmail, "m1"); err == nil {
		t.Fatal("the stored html of an email was resolved as a sendable upload")
	}
}

type entryMedia struct{ row *ConversationMedia }

func (e entryMedia) GetInEntry(string, shared.EntryType, string) (*ConversationMedia, error) {
	return e.row, nil
}
