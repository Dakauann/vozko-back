package personalinbox_repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/personalinbox"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func marksRepo(t *testing.T) (*Repository, func() int64) {
	t.Helper()
	db := repotest.IsolatedDB(t, "personal_marks_test", &schema.ConversationPersonalMark{})
	rows := func() int64 {
		var n int64
		if err := db.Model(&schema.ConversationPersonalMark{}).Count(&n).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	return New(db), rows
}

func TestChangeStoresAMarkAndDeletesItWhenItBecomesEmptyAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo, rows := marksRepo(t)
	user := uuid.NewString()
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeWebchat}
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	before, after, err := repo.Change(ctx, user, chat, func(m personalinbox.Mark) (personalinbox.Mark, error) {
		return m.SetPinned(true, now), nil
	})
	if err != nil || !before.Empty() || !after.Pinned() {
		t.Fatalf("pin = %+v -> %+v, %v", before, after, err)
	}
	stored, err := repo.Find(ctx, user, chat)
	if err != nil || !stored.Same(after) {
		t.Fatalf("Find = %+v, %v; want %+v", stored, err, after)
	}

	_, after, err = repo.Change(ctx, user, chat, func(m personalinbox.Mark) (personalinbox.Mark, error) {
		return m.SetPinned(false, now), nil
	})
	if err != nil || !after.Empty() {
		t.Fatalf("unpin = %+v, %v", after, err)
	}
	if rows() != 0 {
		t.Fatal("an empty mark must be deleted, not stored")
	}
	if found, err := repo.Find(ctx, user, chat); err != nil || !found.Empty() || found.UserID != user || found.Entry() != chat {
		t.Fatalf("Find without a row = %+v, %v; want an empty mark of that user and conversation", found, err)
	}
}

func TestARefusedChangeLeavesNothingBehindAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo, rows := marksRepo(t)
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeTelegram}
	refused := errors.New("refused")

	if _, _, err := repo.Change(ctx, uuid.NewString(), chat, func(m personalinbox.Mark) (personalinbox.Mark, error) {
		return m, refused
	}); !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the change's own error", err)
	}
	if rows() != 0 {
		t.Fatal("a refused change on a new mark left a row")
	}
}

func TestMarksAreKeyedByUserChannelAndConversationAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo, _ := marksRepo(t)
	ana, bia := uuid.NewString(), uuid.NewString()
	id := uuid.NewString()
	telegram := shared.EntryRef{EntryID: id, EntryType: shared.EntryTypeTelegram}
	webchat := shared.EntryRef{EntryID: id, EntryType: shared.EntryTypeWebchat}
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	pin := func(m personalinbox.Mark) (personalinbox.Mark, error) { return m.SetPinned(true, now), nil }

	for _, step := range []struct {
		user  string
		entry shared.EntryRef
	}{{ana, telegram}, {bia, webchat}} {
		if _, _, err := repo.Change(ctx, step.user, step.entry, pin); err != nil {
			t.Fatal(err)
		}
	}

	marks, err := repo.ForEntries(ctx, ana, []shared.EntryRef{telegram, webchat})
	if err != nil || len(marks) != 1 || marks[0].Entry() != telegram || marks[0].UserID != ana {
		t.Fatalf("ForEntries = %+v, %v; want only ana's telegram mark (same id on another channel is another conversation)", marks, err)
	}
	if empty, err := repo.ForEntries(ctx, ana, nil); err != nil || len(empty) != 0 {
		t.Fatalf("ForEntries(nil) = %+v, %v", empty, err)
	}
}

func TestTheMarksTableHoldsOnlyThePinAndTheUnreadMarkAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "personal_marks_columns_test", &schema.ConversationPersonalMark{})
	var columns []string
	if err := db.Raw(`SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'conversation_personal_marks' ORDER BY column_name`).Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{"entry_id", "entry_type", "pinned_at", "unread_marked_at", "updated_at", "user_id"}
	if strings.Join(columns, ",") != strings.Join(want, ",") {
		t.Fatalf("columns = %v, want %v", columns, want)
	}
	var indexes []string
	if err := db.Raw(`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'conversation_personal_marks' ORDER BY indexname`).Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Join(indexes, ",") != "conversation_personal_marks_pkey,idx_personal_marks_entry" {
		t.Fatalf("indexes = %v, want only the key and the per-conversation index", indexes)
	}
}
