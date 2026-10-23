package conversationnote_repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversationnote"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func notesRepo(t *testing.T, prefix string) *Repository {
	t.Helper()
	return New(repotest.IsolatedDB(t, prefix, &schema.ConversationNote{}))
}

func storedNote(t *testing.T, repo *Repository, workspaceID string, entry shared.EntryRef, at time.Time, mentions ...string) conversationnote.Note {
	t.Helper()
	note, err := conversationnote.New(conversationnote.Draft{
		WorkspaceID: workspaceID,
		Entry:       entry,
		AuthorID:    uuid.NewString(),
		Body:        "nota " + at.Format(time.RFC3339Nano),
		Mentions:    mentions,
	}, uuid.NewString(), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), note); err != nil {
		t.Fatalf("create: %v", err)
	}
	return note
}

func TestANoteRoundTripsWithItsMentionsAgainstPostgres(t *testing.T) {
	repo := notesRepo(t, "conversation_notes_roundtrip")
	ws := uuid.NewString()
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeTelegram}
	ana, bia := uuid.NewString(), uuid.NewString()
	at := time.Date(2026, 10, 18, 12, 30, 15, 123456000, time.UTC)

	want := storedNote(t, repo, ws, chat, at, ana, bia)
	got, err := repo.Find(context.Background(), ws, want.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ID != want.ID || got.WorkspaceID != ws || got.Entry() != chat || got.AuthorID != want.AuthorID || got.Body != want.Body {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if strings.Join(got.Mentions, ",") != ana+","+bia {
		t.Fatalf("mentions = %v, want the stored order", got.Mentions)
	}
	if !got.CreatedAt.Equal(at) || got.DeletedAt != nil {
		t.Fatalf("created_at = %v deleted_at = %v", got.CreatedAt, got.DeletedAt)
	}
}

func TestANoteWithoutMentionsReadsBackAsAnEmptyListAgainstPostgres(t *testing.T) {
	repo := notesRepo(t, "conversation_notes_empty_mentions")
	ws := uuid.NewString()
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeWhatsApp}
	note := storedNote(t, repo, ws, chat, time.Now().UTC())
	got, err := repo.Find(context.Background(), ws, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mentions == nil || len(got.Mentions) != 0 {
		t.Fatalf("mentions = %#v, want an empty list", got.Mentions)
	}
}

func TestFindNeverCrossesWorkspacesOrReturnsDeletedNotesAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := notesRepo(t, "conversation_notes_find")
	ws := uuid.NewString()
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeWhatsApp}
	note := storedNote(t, repo, ws, chat, time.Now().UTC())

	if _, err := repo.Find(ctx, uuid.NewString(), note.ID); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("other workspace: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Find(ctx, ws, "not-a-uuid"); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("unreadable id: err = %v, want ErrNotFound", err)
	}
	if err := repo.MarkDeleted(ctx, uuid.NewString(), note.ID, time.Now()); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("deleting from another workspace: err = %v, want ErrNotFound", err)
	}
	if err := repo.MarkDeleted(ctx, ws, note.ID, time.Now()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Find(ctx, ws, note.ID); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("deleted note: err = %v, want ErrNotFound", err)
	}
	if err := repo.MarkDeleted(ctx, ws, note.ID, time.Now()); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("deleting twice: err = %v, want ErrNotFound", err)
	}
}

func TestListByEntryIsNewestFirstScopedAndPagedByBeforeAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := notesRepo(t, "conversation_notes_list")
	ws := uuid.NewString()
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeWhatsApp}
	sameIDOtherChannel := shared.EntryRef{EntryID: chat.EntryID, EntryType: shared.EntryTypeTelegram}
	base := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	oldest := storedNote(t, repo, ws, chat, base)
	middle := storedNote(t, repo, ws, chat, base.Add(time.Minute))
	deleted := storedNote(t, repo, ws, chat, base.Add(2*time.Minute))
	newest := storedNote(t, repo, ws, chat, base.Add(3*time.Minute))
	storedNote(t, repo, ws, sameIDOtherChannel, base.Add(4*time.Minute))
	storedNote(t, repo, uuid.NewString(), chat, base.Add(5*time.Minute))
	storedNote(t, repo, ws, shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeWhatsApp}, base.Add(6*time.Minute))
	if err := repo.MarkDeleted(ctx, ws, deleted.ID, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	page, err := repo.ListByEntry(ctx, conversationnote.PageQuery{WorkspaceID: ws, Entry: chat, Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if ids(page) != newest.ID+","+middle.ID {
		t.Fatalf("first page = %v, want newest then middle (the deleted note and other conversations left out)", ids(page))
	}

	before := middle.CreatedAt
	rest, err := repo.ListByEntry(ctx, conversationnote.PageQuery{WorkspaceID: ws, Entry: chat, Limit: 10, Before: &before})
	if err != nil {
		t.Fatal(err)
	}
	if ids(rest) != oldest.ID {
		t.Fatalf("after the cursor = %v, want only the oldest", ids(rest))
	}
}

func TestTheListIndexCoversOnlyLiveNotesAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "conversation_notes_index", &schema.ConversationNote{})
	var definition string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'idx_conversation_notes_entry'`).Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(definition, "(entry_id, entry_type, created_at DESC)") || !strings.Contains(definition, "WHERE (deleted_at IS NULL)") {
		t.Fatalf("index = %q, want (entry_id, entry_type, created_at DESC) over live notes", definition)
	}
}

func ids(notes []conversationnote.Note) string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.ID
	}
	return strings.Join(out, ",")
}

func TestANotesImagesRoundTripInTheirOrderAgainstPostgres(t *testing.T) {
	repo := notesRepo(t, "conversation_notes_images")
	ws := uuid.NewString()
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeWebchat}
	first, second := uuid.NewString(), uuid.NewString()
	note, err := conversationnote.New(conversationnote.Draft{
		WorkspaceID: ws, Entry: chat, AuthorID: uuid.NewString(), ImageIDs: []string{second, first},
	}, uuid.NewString(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), note); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.Find(context.Background(), ws, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.ImageIDs, ",") != second+","+first || got.Body != "" {
		t.Fatalf("image ids = %v body = %q, want the stored order and an empty body", got.ImageIDs, got.Body)
	}
	page, err := repo.ListByEntry(context.Background(), conversationnote.PageQuery{WorkspaceID: ws, Entry: chat, Limit: 5})
	if err != nil || len(page) != 1 || strings.Join(page[0].ImageIDs, ",") != second+","+first {
		t.Fatalf("listed = %+v err = %v", page, err)
	}
}

func TestANoteWrittenBeforeImagesExistedReadsBackWithoutImagesAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "conversation_notes_legacy_images", &schema.ConversationNote{})
	repo := New(db)
	ws, id := uuid.NewString(), uuid.NewString()
	if err := db.Exec(
		`INSERT INTO conversation_notes (id, workspace_id, entry_id, entry_type, author_id, body, created_at) VALUES (?, ?, ?, 'whatsapp', ?, 'antiga', now())`,
		id, ws, uuid.NewString(), uuid.NewString(),
	).Error; err != nil {
		t.Fatalf("insert without image_ids: %v", err)
	}
	got, err := repo.Find(context.Background(), ws, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageIDs == nil || len(got.ImageIDs) != 0 || got.Mentions == nil {
		t.Fatalf("image ids = %#v mentions = %#v, want empty lists from the column defaults", got.ImageIDs, got.Mentions)
	}
}
