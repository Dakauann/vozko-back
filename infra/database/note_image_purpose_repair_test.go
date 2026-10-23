package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTheNoteImagePurposeRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "cm_mark_note_images" {
			return
		}
	}
	t.Fatal("images already attached to notes must be marked as note media at boot, or they stay sendable")
}

func TestNoteImagesAttachedBeforeThePurposeColumnAreMarkedAsNotes(t *testing.T) {
	db := repotest.IsolatedDB(t, "note_purpose", &schema.ConversationMedia{}, &schema.ConversationNote{})
	entry := uuid.NewString()
	noteImage, deletedNoteImage, upload := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{noteImage, deletedNoteImage, upload} {
		if err := db.Create(&schema.ConversationMedia{ID: id, EntryID: entry, EntryType: "whatsapp", Type: "image", URL: "https://cdn/" + id}).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}
	deletedAt := time.Now().UTC()
	notes := []schema.ConversationNote{
		{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), EntryID: entry, EntryType: "whatsapp", AuthorID: uuid.NewString(), Body: "veja", ImageIDs: pq.StringArray{noteImage}, CreatedAt: time.Now().UTC()},
		{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), EntryID: entry, EntryType: "whatsapp", AuthorID: uuid.NewString(), Body: "apagada", ImageIDs: pq.StringArray{deletedNoteImage}, CreatedAt: time.Now().UTC(), DeletedAt: &deletedAt},
		{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), EntryID: entry, EntryType: "whatsapp", AuthorID: uuid.NewString(), Body: "sem imagem", CreatedAt: time.Now().UTC()},
	}
	for i := range notes {
		if err := db.Create(&notes[i]).Error; err != nil {
			t.Fatalf("seed note: %v", err)
		}
	}

	for range 2 {
		if err := markNoteImages(db); err != nil {
			t.Fatalf("repair: %v", err)
		}
	}

	want := map[string]string{noteImage: "note", deletedNoteImage: "note", upload: "message"}
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

func TestTheNoteImageRepairSkipsADatabaseWithoutNotes(t *testing.T) {
	db := repotest.IsolatedDB(t, "note_purpose_empty", &schema.ConversationMedia{})
	if err := markNoteImages(db); err != nil {
		t.Fatalf("repair: %v", err)
	}
}
