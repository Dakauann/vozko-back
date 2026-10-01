package conversation_repository

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestMediaLayoutIsStoredAndReadInOneBatch(t *testing.T) {
	repo := NewMediaRepository(repotest.IsolatedDB(t, "cmedia_test", &schema.ConversationMedia{}))
	entry := uuid.NewString()
	photo := &conversation.ConversationMedia{
		ID: uuid.NewString(), EntryID: entry, EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeImage,
		URL: "https://cdn/p.jpg", Layout: conversation.MediaLayout{Width: 1600, Height: 1200, Thumbhash: "1QcSHQRnh493V4dIh4eXh1h4kJUI"},
	}
	voice := &conversation.ConversationMedia{
		ID: uuid.NewString(), EntryID: entry, EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeAudio, URL: "https://cdn/a.ogg",
	}
	for _, m := range []*conversation.ConversationMedia{photo, voice} {
		if err := repo.Create(m); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	rows, err := repo.ListByIDs([]string{photo.ID, voice.ID, uuid.NewString()})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	byID := map[string]*conversation.ConversationMedia{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if byID[photo.ID].Layout != photo.Layout {
		t.Fatalf("layout = %+v, want %+v", byID[photo.ID].Layout, photo.Layout)
	}
	if byID[voice.ID].Layout.Known() {
		t.Fatalf("media without a layout must read as unknown, got %+v", byID[voice.ID].Layout)
	}
	if none, err := repo.ListByIDs(nil); err != nil || none != nil {
		t.Fatalf("an empty page must not query: %v %v", none, err)
	}
}

func BenchmarkAHistoryPageOfMediaFromALargeTable(b *testing.B) {
	db := repotest.IsolatedDB(b, "cmedia_bench", &schema.ConversationMedia{})
	repo := NewMediaRepository(db)
	rows := make([]schema.ConversationMedia, 20_000)
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = uuid.NewString()
		rows[i] = schema.ConversationMedia{ID: ids[i], EntryID: uuid.NewString(), EntryType: "whatsapp", Type: "image", URL: "https://cdn/x.jpg", Width: 1600, Height: 1200, Thumbhash: "1QcSHQRnh493V4dIh4eXh1h4kJUI"}
	}
	if err := db.CreateInBatches(rows, 1000).Error; err != nil {
		b.Fatal(err)
	}
	if err := db.Exec("ANALYZE conversation_media").Error; err != nil {
		b.Fatal(err)
	}
	page := ids[10_000:10_050]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		found, err := repo.ListByIDs(page)
		if err != nil || len(found) != len(page) {
			b.Fatalf("found=%d err=%v", len(found), err)
		}
	}
}
