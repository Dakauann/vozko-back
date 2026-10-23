package database

import (
	"testing"

	"github.com/google/uuid"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTheEmailBodyPurposeRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "cm_mark_email_bodies" {
			return
		}
	}
	t.Fatal("email html stored before the email body purpose must be marked at boot, or it stays sendable")
}

func TestEmailHTMLStoredAsMessageMediaIsMarkedAsAnEmailBody(t *testing.T) {
	db := repotest.IsolatedDB(t, "email_body_purpose", &schema.ConversationMedia{}, &schema.ConversationMessage{})
	conv, other := uuid.NewString(), uuid.NewString()
	body, attachment, foreignBody := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seed := []schema.ConversationMedia{
		{ID: body, EntryID: conv, EntryType: "email", Type: "document", MimeType: "text/html", URL: "https://cdn/" + body},
		{ID: attachment, EntryID: conv, EntryType: "email", Type: "document", MimeType: "text/html", URL: "https://cdn/" + attachment},
		{ID: foreignBody, EntryID: other, EntryType: "whatsapp", Type: "document", MimeType: "text/html", URL: "https://cdn/" + foreignBody},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}
	messages := []schema.ConversationMessage{
		{ID: uuid.NewString(), EntryID: conv, EntryType: "email", Text: "Oi", Metadata: []byte(`{"email_html_media_id":"` + body + `"}`)},
		{ID: uuid.NewString(), EntryID: conv, EntryType: "email", Text: "anexo", MediaID: &attachment, Metadata: []byte(`{}`)},
		{ID: uuid.NewString(), EntryID: other, EntryType: "whatsapp", Text: "x", Metadata: []byte(`{"email_html_media_id":"` + foreignBody + `"}`)},
	}
	for i := range messages {
		if err := db.Create(&messages[i]).Error; err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}

	for range 2 {
		if err := markEmailBodies(db); err != nil {
			t.Fatalf("repair: %v", err)
		}
	}

	want := map[string]string{body: "email_body", attachment: "message", foreignBody: "message"}
	for id, purpose := range want {
		var got string
		if err := db.Raw(`SELECT purpose FROM conversation_media WHERE id = ?`, id).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != purpose {
			t.Errorf("media %s has purpose %q, want %q", id, got, purpose)
		}
	}
}

func TestTheEmailBodyRepairSkipsADatabaseWithoutMessages(t *testing.T) {
	db := repotest.IsolatedDB(t, "email_body_purpose_empty", &schema.ConversationMedia{})
	if err := markEmailBodies(db); err != nil {
		t.Fatalf("repair: %v", err)
	}
}
