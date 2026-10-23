package conversation_repository

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestMediaIsReadOnlyInsideItsOwnConversation(t *testing.T) {
	repo := NewMediaRepository(repotest.IsolatedDB(t, "cmedia_entry", &schema.ConversationMedia{}))
	entry, otherEntry := uuid.NewString(), uuid.NewString()
	photo := &conversation.ConversationMedia{
		ID: uuid.NewString(), EntryID: entry, EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeImage, URL: "https://cdn/p.jpg",
	}
	photo.Normalize()
	if err := repo.Create(photo); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetInEntry(entry, shared.EntryTypeWhatsApp, photo.ID)
	if err != nil {
		t.Fatalf("GetInEntry: %v", err)
	}
	if got.Purpose != conversation.MediaPurposeMessage {
		t.Fatalf("purpose = %q, want message", got.Purpose)
	}

	for name, c := range map[string]struct {
		entryID   string
		entryType shared.EntryType
		id        string
	}{
		"another conversation": {otherEntry, shared.EntryTypeWhatsApp, photo.ID},
		"another channel":      {entry, shared.EntryTypeTelegram, photo.ID},
		"unknown id":           {entry, shared.EntryTypeWhatsApp, uuid.NewString()},
		"malformed id":         {entry, shared.EntryTypeWhatsApp, "not-a-uuid"},
		"malformed entry":      {"not-a-uuid", shared.EntryTypeWhatsApp, photo.ID},
	} {
		if _, err := repo.GetInEntry(c.entryID, c.entryType, c.id); !errors.Is(err, conversation.ErrMediaNotFound) {
			t.Errorf("%s: err = %v, want ErrMediaNotFound", name, err)
		}
	}
}

func TestTheNotePurposeIsStoredAndReadBack(t *testing.T) {
	db := repotest.IsolatedDB(t, "cmedia_purpose", &schema.ConversationMedia{})
	repo := NewMediaRepository(db)
	entry := uuid.NewString()
	note := &conversation.ConversationMedia{
		ID: uuid.NewString(), EntryID: entry, EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeImage, URL: "https://cdn/n.jpg", Purpose: conversation.MediaPurposeNote,
	}
	if err := repo.Create(note); err != nil {
		t.Fatalf("create: %v", err)
	}
	unset := &conversation.ConversationMedia{
		ID: uuid.NewString(), EntryID: entry, EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeImage, URL: "https://cdn/u.jpg",
	}
	if err := repo.Create(unset); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetInEntry(entry, shared.EntryTypeWhatsApp, note.ID)
	if err != nil || got.Purpose != conversation.MediaPurposeNote {
		t.Fatalf("note read back as %+v, err %v", got, err)
	}
	if _, err := conversation.SendableUpload(repo, entry, shared.EntryTypeWhatsApp, note.ID); !errors.Is(err, conversation.ErrMediaNotSendable) {
		t.Fatalf("a stored note image was sendable: %v", err)
	}

	var column string
	if err := db.Raw(`SELECT purpose FROM conversation_media WHERE id = ?`, unset.ID).Scan(&column).Error; err != nil {
		t.Fatal(err)
	}
	if column != string(conversation.MediaPurposeMessage) {
		t.Fatalf("a row written without a purpose stored %q, want the message default", column)
	}
	if _, err := conversation.SendableUpload(repo, entry, shared.EntryTypeWhatsApp, unset.ID); err != nil {
		t.Fatalf("an ordinary upload must stay sendable: %v", err)
	}
}

func TestMediaIsFoundByAddressOnlyInsideItsOwnConversation(t *testing.T) {
	db := repotest.IsolatedDB(t, "cmedia_url", &schema.ConversationMedia{})
	repo, byURL := NewMediaRepository(db), NewMediaURLReader(db)
	entry, otherEntry := uuid.NewString(), uuid.NewString()
	photo := &conversation.ConversationMedia{
		ID: uuid.NewString(), EntryID: entry, EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeImage, URL: "https://cdn/by-url.jpg",
	}
	photo.Normalize()
	if err := repo.Create(photo); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := byURL.GetInEntryByURL(entry, shared.EntryTypeWhatsApp, photo.URL)
	if err != nil || got.ID != photo.ID || got.Purpose != conversation.MediaPurposeMessage {
		t.Fatalf("read back %+v, err %v", got, err)
	}
	for name, c := range map[string]struct {
		entryID   string
		entryType shared.EntryType
		url       string
	}{
		"another conversation": {otherEntry, shared.EntryTypeWhatsApp, photo.URL},
		"another channel":      {entry, shared.EntryTypeTelegram, photo.URL},
		"unknown address":      {entry, shared.EntryTypeWhatsApp, "https://cdn/other.jpg"},
		"malformed entry":      {"not-a-uuid", shared.EntryTypeWhatsApp, photo.URL},
	} {
		if _, err := byURL.GetInEntryByURL(c.entryID, c.entryType, c.url); !errors.Is(err, conversation.ErrMediaNotFound) {
			t.Errorf("%s: err = %v, want ErrMediaNotFound", name, err)
		}
	}
}
