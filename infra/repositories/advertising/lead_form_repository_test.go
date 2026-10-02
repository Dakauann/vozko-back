package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

const trackFormPattern = `INSERT INTO ad_lead_forms \(meta_id, workspace_id, ad_account_id, page_id, name\) VALUES .* ` +
	`ON CONFLICT \(meta_id\) DO UPDATE SET .*WHERE ad_lead_forms\.workspace_id = EXCLUDED\.workspace_id`

func trackedForm() *advertising.TrackedForm {
	return &advertising.TrackedForm{MetaID: "f-1", WorkspaceID: "ws", AdAccountID: "a-1", PageID: "p-1", Name: "Cadastro"}
}

func TestLeadFormTrackUpsertsWithinTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(trackFormPattern).
		WithArgs("f-1", "ws", "a-1", "p-1", "Cadastro").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewLeadFormRepository(db).Track(context.Background(), trackedForm()); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestLeadFormTrackRefusesAFormOfAnotherWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(trackFormPattern).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewLeadFormRepository(db).Track(context.Background(), trackedForm()); !errors.Is(err, errFormTrackedElsewhere) {
		t.Fatalf("got %v", err)
	}
}

func TestLeadFormTrackRequiresWorkspaceAndMetaID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLeadFormRepository(db)
	f := trackedForm()
	f.WorkspaceID = ""
	if err := repo.Track(context.Background(), f); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	f = trackedForm()
	f.MetaID = " "
	if err := repo.Track(context.Background(), f); !errors.Is(err, errMetaIDRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestLeadFormFindByMetaIDMissingIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_lead_forms" WHERE meta_id = $1`)).
		WithArgs("f-x", 1).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}))
	if _, err := NewLeadFormRepository(db).FindByMetaID(context.Background(), "f-x"); !errors.Is(err, advertising.ErrLeadFormNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestLeadFormFindByMetaIDMapsTheRow(t *testing.T) {
	db, mock := newMockDB(t)
	polled := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM "ad_lead_forms"`).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id", "workspace_id", "ad_account_id", "page_id", "name", "last_polled_at"}).
			AddRow("f-1", "ws", "a-1", "p-1", "Cadastro", polled))
	f, err := NewLeadFormRepository(db).FindByMetaID(context.Background(), "f-1")
	if err != nil {
		t.Fatal(err)
	}
	if f.WorkspaceID != "ws" || f.AdAccountID != "a-1" || f.PageID != "p-1" || f.Name != "Cadastro" ||
		f.LastPolledAt == nil || !f.LastPolledAt.Equal(polled) {
		t.Fatalf("got %+v", f)
	}
}

func TestLeadFormListByWorkspaceIsScoped(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_lead_forms" WHERE workspace_id = $1 ORDER BY name, meta_id`)).
		WithArgs("ws").
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}).AddRow("f-1"))
	repo := NewLeadFormRepository(db)
	forms, err := repo.ListByWorkspace(context.Background(), "ws")
	if err != nil || len(forms) != 1 {
		t.Fatalf("got %v, %v", forms, err)
	}
	if _, err := repo.ListByWorkspace(context.Background(), ""); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestLeadFormListAllPutsNeverPolledFormsFirst(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_lead_forms" ORDER BY last_polled_at ASC NULLS FIRST, meta_id LIMIT $1 OFFSET $2`)).
		WithArgs(50, 100).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}).AddRow("f-1").AddRow("f-2"))
	forms, err := NewLeadFormRepository(db).ListAll(context.Background(), 50, 100)
	if err != nil || len(forms) != 2 {
		t.Fatalf("got %v, %v", forms, err)
	}
}

func TestLeadFormMarkPolledOfUnknownFormIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_lead_forms" SET "last_polled_at"=$1 WHERE meta_id = $2`)).
		WithArgs(at, "f-x").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewLeadFormRepository(db).MarkPolled(context.Background(), "f-x", at); !errors.Is(err, advertising.ErrLeadFormNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}
