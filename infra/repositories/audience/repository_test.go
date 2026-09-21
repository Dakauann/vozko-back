package audience_repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	ca "vozko/domain/audience"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

var now = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func ref() ca.ContainerRef {
	return ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1"}
}

func TestInsertIsOnConflictDoNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "audience_analyses"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	a, _ := ca.NewPending(ca.NewInput{
		WorkspaceID: "ws-1", Container: ref(), SubjectID: "c-1", AuthorExternalID: "u-1", Text: "oi", Now: now,
	})
	a.ID = "row-1"
	inserted, err := NewRepository(db).Insert(context.Background(), a)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !inserted {
		t.Fatal("a fresh row must report inserted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInsertDuplicateIsNotAnError(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta(`ON CONFLICT ("source","subject_kind","subject_id","revision") DO NOTHING`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	a, _ := ca.NewPending(ca.NewInput{
		WorkspaceID: "ws-1", Container: ref(), SubjectID: "c-1", AuthorExternalID: "u-1", Text: "oi", Now: now,
	})
	a.ID = "row-1"
	inserted, err := NewRepository(db).Insert(context.Background(), a)
	if err != nil {
		t.Fatalf("a duplicate must not be an error, got %v", err)
	}
	if inserted {
		t.Fatal("a duplicate must report not inserted")
	}
}

func TestClaimByIDsIsOneGuardedUpdate(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`UPDATE audience_analyses\s+SET status = \$1, attempts = attempts \+ 1, updated_at = \$2\s+WHERE status = \$3 AND deleted_at IS NULL AND id IN \(\$4,\$5,\$6\)\s+RETURNING id`).
		WithArgs(string(ca.StatusInFlight), now, string(ca.StatusPending), "row-1", "row-2", "row-3").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("row-1").AddRow("row-3"))

	got, err := NewRepository(db).ClaimByIDs(context.Background(), []string{"row-1", "row-2", "row-3"}, now)
	if err != nil {
		t.Fatalf("ClaimByIDs: %v", err)
	}
	if len(got) != 2 || got[0] != "row-1" || got[1] != "row-3" {
		t.Fatalf("claimed = %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimByIDsEmptyIsNotAnError(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`UPDATE audience_analyses`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := NewRepository(db).ClaimByIDs(context.Background(), []string{"row-1"}, now)
	if err != nil {
		t.Fatalf("empty claim must not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected nothing, got %v", got)
	}
	if got, err := NewRepository(db).ClaimByIDs(context.Background(), nil, now); err != nil || len(got) != 0 {
		t.Fatalf("nil ids: %v %v", got, err)
	}
}

func TestGetTrendUsesLiveFiltersAndGroupsByUTCDay(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	day := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT date_trunc\('day'.*FROM "audience_analyses".*subject_kind IN.*GROUP BY date_trunc`).
		WillReturnRows(sqlmock.NewRows([]string{"bucket_date", "total", "analyzed", "conversation_count", "messages_total"}).
			AddRow(day, 1, 1, 1, 114))

	rows, err := NewRepository(db).GetTrend(context.Background(), ca.ListInput{
		WorkspaceID: "ws-1",
		Source:      ca.SourceUnofficialWhatsApp,
		SubjectKinds: []ca.SubjectKind{
			ca.SubjectKindConversation,
		},
	})
	if err != nil {
		t.Fatalf("GetTrend: %v", err)
	}
	if len(rows) != 1 || !rows[0].BucketDate.Equal(day) || rows[0].ConversationCount != 1 || rows[0].MessagesTotal != 114 {
		t.Fatalf("rows = %+v", rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveWritesZeroValues(t *testing.T) {
	cols := saveColumns(&ca.Analysis{ID: "row-1", Status: ca.StatusPending})
	for _, key := range []string{"severity", "requires_action", "failure_reason", "attempts", "batch_id", "analyzed_at"} {
		if _, ok := cols[key]; !ok {
			t.Errorf("saveColumns must always carry %q", key)
		}
	}
	if cols["severity"] != 0 || cols["requires_action"] != false || cols["failure_reason"] != "" {
		t.Errorf("zero values must be present, not dropped: %+v", cols)
	}
}

func TestListPendingContainersGroupsByContainer(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT subject_kind, source, account_id, container_id, MIN\(workspace_id::text\) AS workspace_id,\s+COUNT\(\*\) AS pending, MIN\(created_at\) AS oldest_at FROM "audience_analyses" WHERE status = \$1 AND deleted_at IS NULL AND created_at < \$2 GROUP BY subject_kind, source, account_id, container_id ORDER BY oldest_at ASC LIMIT \$3`).
		WithArgs(string(ca.StatusPending), now, 50).
		WillReturnRows(sqlmock.NewRows([]string{"subject_kind", "source", "account_id", "container_id", "workspace_id", "pending", "oldest_at"}).
			AddRow("comment", "instagram", "acc-1", "media-1", "ws-1", 12, now.Add(-time.Hour)).
			AddRow("conversation", "whatsapp", "acc-1", "camp-1", "ws-1", 3, now.Add(-time.Hour)))

	got, err := NewRepository(db).ListPendingContainers(context.Background(), now, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d containers, want 2: %+v", len(got), got)
	}
	if !got[0].Ref.Equal(ref()) || got[0].Pending != 12 || got[0].WorkspaceID != "ws-1" {
		t.Fatalf("comment container: %+v", got[0])
	}
	wantConversation := ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceWhatsApp,
		AccountID: "acc-1", ContainerID: "camp-1",
	}
	if !got[1].Ref.Equal(wantConversation) {
		t.Fatalf("conversation container: %+v", got[1].Ref)
	}
}
