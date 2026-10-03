package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

func savedDraft() *advertising.SavedDraft {
	return &advertising.SavedDraft{
		WorkspaceID: "ws", AdAccountID: "a-1", CreatedBy: "u-1", UpdatedBy: "u-1",
		Content: advertising.AdDraft{AdAccountID: "a-1", Campaign: advertising.CampaignDraft{Name: "Promo"}},
	}
}

func TestDraftCreateStartsAtVersionOne(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ad_drafts"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	d := savedDraft()
	if err := NewDraftRepository(db).Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.Version != 1 {
		t.Fatalf("got %+v", d)
	}
	expectationsMet(t, mock)
}

func TestDraftCreateNeedsTheAccount(t *testing.T) {
	db, _ := newMockDB(t)
	d := savedDraft()
	d.AdAccountID = ""
	if err := NewDraftRepository(db).Create(context.Background(), d); !errors.Is(err, errAccountRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestDraftFindIsScopedAndMapsTheJob(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_drafts" WHERE workspace_id = $1 AND id = $2`)).
		WithArgs("ws", "d-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "ad_account_id", "content", "job_id", "version"}).
			AddRow("d-1", "ws", "a-1", []byte(`{"campaign":{"name":"Promo"}}`), "j-1", 3))
	d, err := NewDraftRepository(db).Find(context.Background(), "ws", "d-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Content.Campaign.Name != "Promo" || d.JobID != "j-1" || d.Version != 3 {
		t.Fatalf("got %+v", d)
	}
}

func TestDraftFindMissing(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewDraftRepository(db).Find(context.Background(), "ws", "d-1"); !errors.Is(err, advertising.ErrDraftNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestDraftSaveBumpsTheVersionAndClearsTheJob(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_drafts" SET`)).WillReturnResult(sqlmock.NewResult(0, 1))
	d := savedDraft()
	d.ID, d.Version, d.JobID = "d-1", 2, "j-1"
	if err := NewDraftRepository(db).Save(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if d.Version != 3 || d.JobID != "" {
		t.Fatalf("got %+v", d)
	}
	expectationsMet(t, mock)
}

func TestDraftSaveTellsAStaleVersionFromAMissingDraft(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_drafts" SET`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ad_drafts"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	d := savedDraft()
	d.ID, d.Version = "d-1", 2
	if err := NewDraftRepository(db).Save(context.Background(), d); !errors.Is(err, advertising.ErrDraftChanged) {
		t.Fatalf("got %v", err)
	}
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_drafts" SET`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ad_drafts"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	if err := NewDraftRepository(db).Save(context.Background(), d); !errors.Is(err, advertising.ErrDraftNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestDraftDeleteOnlyRemovesTheVersionThatWasRead(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ad_drafts" WHERE id = $1 AND workspace_id = $2 AND version = $3`)).
		WithArgs("d-1", "ws", 2).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ad_drafts"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	d := savedDraft()
	d.ID, d.Version = "d-1", 2
	if err := NewDraftRepository(db).Delete(context.Background(), d); !errors.Is(err, advertising.ErrDraftChanged) {
		t.Fatalf("a claim that landed first must keep the draft, got %v", err)
	}
	expectationsMet(t, mock)
}

func TestDraftClaimForPublishIsWonOnce(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_drafts" SET "job_id"=$1,"version"=version + 1,"updated_at"=$2 WHERE id = $3 AND workspace_id = $4 AND version = $5`)).
		WithArgs("j-1", sqlmock.AnyArg(), "d-1", "ws", 2).WillReturnResult(sqlmock.NewResult(0, 1))
	d := savedDraft()
	d.ID, d.Version = "d-1", 2
	if err := NewDraftRepository(db).ClaimForPublish(context.Background(), d, "j-1"); err != nil {
		t.Fatal(err)
	}
	if d.Version != 3 || d.JobID != "j-1" {
		t.Fatalf("got %+v", d)
	}
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_drafts" SET`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ad_drafts"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	d.Version = 2
	if err := NewDraftRepository(db).ClaimForPublish(context.Background(), d, "j-2"); !errors.Is(err, advertising.ErrDraftChanged) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}
