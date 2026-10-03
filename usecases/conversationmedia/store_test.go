package conversationmedia

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type bucket struct {
	uploaded map[string][]byte
	fail     error
}

func (b *bucket) UploadFile(key string, data []byte, _ string) error {
	if b.fail != nil {
		return b.fail
	}
	if b.uploaded == nil {
		b.uploaded = map[string][]byte{}
	}
	b.uploaded[key] = data
	return nil
}

func (b *bucket) GetFileURL(key string) string { return "https://cdn/" + key }

type mediaRows struct {
	rows []*conversation.ConversationMedia
	fail error
}

func (m *mediaRows) Create(media *conversation.ConversationMedia) error {
	if m.fail != nil {
		return m.fail
	}
	m.rows = append(m.rows, media)
	return nil
}
func (m *mediaRows) GetByID(id string) (*conversation.ConversationMedia, error) {
	for _, r := range m.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, conversation.ErrMediaNotFound
}
func (m *mediaRows) ListByIDs(ids []string) ([]*conversation.ConversationMedia, error) {
	var out []*conversation.ConversationMedia
	for _, id := range ids {
		if r, err := m.GetByID(id); err == nil {
			out = append(out, r)
		}
	}
	return out, m.fail
}
func (m *mediaRows) GetByWhatsAppMediaID(string) (*conversation.ConversationMedia, error) {
	return nil, conversation.ErrMediaNotFound
}
func (m *mediaRows) ListByEntry(string, shared.EntryType) ([]*conversation.ConversationMedia, error) {
	return m.rows, nil
}
func (m *mediaRows) Delete(string) error                          { return nil }
func (m *mediaRows) DeleteByEntry(string, shared.EntryType) error { return nil }

type fixedInspector struct {
	layout conversation.MediaLayout
	calls  int
}

func (f *fixedInspector) Inspect([]byte, conversation.MediaType) conversation.MediaLayout {
	f.calls++
	return f.layout
}

func photoInput() conversation.StoreMediaInput {
	return conversation.StoreMediaInput{
		ID: "7b0f6d3e-1c2a-4b5d-9e8f-0a1b2c3d4e5f", Key: "conversations/whatsapp/e1/photo.jpg",
		EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeImage,
		MimeType: "image/jpeg", Data: []byte("jpeg-bytes"), OriginalFilename: " foto.jpg ",
	}
}

func TestStoringMediaUploadsMeasuresAndRecordsItOnce(t *testing.T) {
	files, rows := &bucket{}, &mediaRows{}
	inspector := &fixedInspector{layout: conversation.MediaLayout{Width: 1600, Height: 1200, Thumbhash: "abc"}}
	stored, err := NewStore(files, rows, inspector).Store(photoInput())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if string(files.uploaded["conversations/whatsapp/e1/photo.jpg"]) != "jpeg-bytes" {
		t.Fatal("the bytes must be uploaded under the caller's key")
	}
	if len(rows.rows) != 1 || stored.URL != "https://cdn/conversations/whatsapp/e1/photo.jpg" || stored.SizeBytes != 10 {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.Layout != inspector.layout || stored.OriginalFilename != "foto.jpg" || inspector.calls != 1 {
		t.Fatalf("stored = %+v calls=%d", stored, inspector.calls)
	}
}

func TestAFailedUploadOrInsertLeavesNoMediaBehind(t *testing.T) {
	down := errors.New("bucket down")
	if _, err := NewStore(&bucket{fail: down}, &mediaRows{}, &fixedInspector{}).Store(photoInput()); !errors.Is(err, down) {
		t.Fatalf("upload failure: %v", err)
	}
	dbDown := errors.New("db down")
	if media, err := NewStore(&bucket{}, &mediaRows{fail: dbDown}, &fixedInspector{}).Store(photoInput()); !errors.Is(err, dbDown) || media != nil {
		t.Fatalf("a row that was not saved must not hand back a media id: %v %v", media, err)
	}
}

func TestStoringRefusesIncompleteInput(t *testing.T) {
	store := NewStore(&bucket{}, &mediaRows{}, &fixedInspector{})
	for name, mutate := range map[string]func(*conversation.StoreMediaInput){
		"no bytes": func(in *conversation.StoreMediaInput) { in.Data = nil },
		"no key":   func(in *conversation.StoreMediaInput) { in.Key = " " },
		"no id":    func(in *conversation.StoreMediaInput) { in.ID = "" },
		"no entry": func(in *conversation.StoreMediaInput) { in.EntryID = "" },
		"bad kind": func(in *conversation.StoreMediaInput) { in.Type = "hologram" },
	} {
		in := photoInput()
		mutate(&in)
		if _, err := store.Store(in); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}
