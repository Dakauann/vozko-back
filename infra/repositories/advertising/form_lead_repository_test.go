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

const saveFormLeadPattern = `INSERT INTO ad_form_leads \(meta_id, workspace_id, form_meta_id, ad_meta_id, page_id, answers, lead_id, created_time\) ` +
	`VALUES .* ON CONFLICT \(meta_id\) DO NOTHING`

func formLead() *advertising.FormLead {
	return &advertising.FormLead{
		MetaID:      "l-1",
		FormMetaID:  "f-1",
		AdMetaID:    "ad-1",
		PageID:      "p-1",
		WorkspaceID: "ws",
		Answers:     map[string]string{"email": "a@b.com"},
		CreatedTime: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC),
	}
}

func TestFormLeadSaveReportsWhetherItInserted(t *testing.T) {
	db, mock := newMockDB(t)
	l := formLead()
	mock.ExpectExec(saveFormLeadPattern).
		WithArgs("l-1", "ws", "f-1", "ad-1", "p-1", sqlmock.AnyArg(), nil, l.CreatedTime).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(saveFormLeadPattern).WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewFormLeadRepository(db)
	if inserted, err := repo.Save(context.Background(), l); err != nil || !inserted {
		t.Fatalf("first: %v, %v", inserted, err)
	}
	if inserted, err := repo.Save(context.Background(), l); err != nil || inserted {
		t.Fatalf("duplicate: %v, %v", inserted, err)
	}
	expectationsMet(t, mock)
}

func TestFormLeadSaveRequiresWorkspaceAndMetaID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewFormLeadRepository(db)
	l := formLead()
	l.WorkspaceID = ""
	if _, err := repo.Save(context.Background(), l); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	l = formLead()
	l.MetaID = ""
	if _, err := repo.Save(context.Background(), l); !errors.Is(err, errMetaIDRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestFormLeadLinkLeadOfUnknownLeadIsAnError(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_form_leads" SET "lead_id"=$1 WHERE meta_id = $2`)).
		WithArgs("lead-1", "l-x").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewFormLeadRepository(db).LinkLead(context.Background(), "l-x", "lead-1"); !errors.Is(err, errFormLeadNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestFormLeadListFiltersByFormNewestFirstWithTotal(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ad_form_leads" WHERE workspace_id = $1 AND form_meta_id = $2`)).
		WithArgs("ws", "f-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(7)))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_form_leads" WHERE workspace_id = $1 AND form_meta_id = $2 ORDER BY created_time DESC, meta_id LIMIT $3 OFFSET $4`)).
		WithArgs("ws", "f-1", 2, 4).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id", "workspace_id", "form_meta_id", "answers", "lead_id"}).
			AddRow("l-2", "ws", "f-1", []byte(`{"email":"a@b.com"}`), "lead-1").
			AddRow("l-1", "ws", "f-1", []byte(`{}`), nil))
	leads, total, err := NewFormLeadRepository(db).List(context.Background(),
		advertising.FormLeadQuery{WorkspaceID: "ws", FormMetaID: "f-1", Limit: 2, Offset: 4})
	if err != nil || total != 7 || len(leads) != 2 {
		t.Fatalf("got %v, %d, %v", leads, total, err)
	}
	if leads[0].Answers["email"] != "a@b.com" || leads[0].LeadID != "lead-1" || leads[1].LeadID != "" {
		t.Fatalf("got %+v / %+v", leads[0], leads[1])
	}
	expectationsMet(t, mock)
}

func TestFormLeadListWithoutFormSpansTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ad_form_leads" WHERE workspace_id = $1`)).
		WithArgs("ws").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_form_leads" WHERE workspace_id = $1 ORDER BY created_time DESC, meta_id LIMIT $2`)).
		WithArgs("ws", 20).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}))
	if _, _, err := NewFormLeadRepository(db).List(context.Background(), advertising.FormLeadQuery{WorkspaceID: "ws", Limit: 20}); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestFormLeadListRequiresWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	_, _, err := NewFormLeadRepository(db).List(context.Background(), advertising.FormLeadQuery{FormMetaID: "f-1"})
	if !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}
