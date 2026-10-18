package lead

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/lead"
	"vozko/domain/leadcompany"
)

const (
	imageUUID   = "5a1b2c3d-0000-4e5f-8a9b-0c1d2e3f4a5b"
	otherLeadID = "6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a02"
)

var companyAt = time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)

var companyRowColumns = []string{"id", "workspace_id", "name", "accepts_leads", "image_media_id", "created_by", "created_at", "updated_at", "image_url", "image_preview_url", "members_count"}

func acmeCompany() leadcompany.Company {
	return leadcompany.Company{ID: companyUUID, WorkspaceID: wsUUID, Name: "Acme", AcceptsLeads: true, ImageMediaID: imageUUID, CreatedBy: actorUUID, CreatedAt: companyAt, UpdatedAt: companyAt}
}

func TestCreateCompanyInsertsTheRow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(exact(insertCompanySQL)).
		WithArgs(companyUUID, wsUUID, "Acme", true, imageUUID, actorUUID, companyAt, companyAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := (&repository{db: db}).CreateCompany(context.Background(), acmeCompany()); err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateCompanyTouchesOnlyALiveCompanyOfTheWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	c := acmeCompany()
	c.ImageMediaID, c.AcceptsLeads = "", false
	mock.ExpectExec(exact(updateCompanySQL)).
		WithArgs("Acme", false, nil, companyAt, wsUUID, companyUUID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := (&repository{db: db}).UpdateCompany(context.Background(), c); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetCompanyReadsTheImageAndCountsTheLiveMembers(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(companySelectSQL+" AND c.id = ?")).WithArgs(wsUUID, companyUUID).
		WillReturnRows(sqlmock.NewRows(companyRowColumns).
			AddRow(companyUUID, wsUUID, "Acme", true, imageUUID, actorUUID, companyAt, companyAt, "https://cdn/logo.png", "https://cdn/logo-preview.jpg", 12))
	got, err := (&repository{db: db}).GetCompany(context.Background(), wsUUID, companyUUID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	want := leadcompany.Image{MediaID: imageUUID, URL: "https://cdn/logo.png", PreviewURL: "https://cdn/logo-preview.jpg"}
	if got.Name != "Acme" || got.MembersCount != 12 || got.Image == nil || *got.Image != want || got.CreatedBy != actorUUID {
		t.Fatalf("company = %+v", got)
	}
	if !strings.Contains(companySelectSQL, "l.deleted_at IS NULL") || !strings.Contains(companySelectSQL, "m.deleted_at IS NULL") {
		t.Fatal("members and images must be live rows")
	}
}

func TestACompanyWhoseImageIsGoneHasNoImageButKeepsTheReference(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(companySelectSQL + " AND c.id = ?")).
		WillReturnRows(sqlmock.NewRows(companyRowColumns).
			AddRow(companyUUID, wsUUID, "Acme", true, imageUUID, nil, companyAt, companyAt, nil, nil, 0))
	got, err := (&repository{db: db}).GetCompany(context.Background(), wsUUID, companyUUID)
	if err != nil || got.Image != nil || got.ImageMediaID != imageUUID {
		t.Fatalf("company = %+v, err = %v", got, err)
	}
}

func TestGetCompanyOfAnIDThatIsNotAUUIDIsNotFoundWithoutAQuery(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if _, err := (&repository{db: db}).GetCompany(context.Background(), wsUUID, "acme"); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListCompaniesSearchesTheFoldedNameAndPages(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	q := leadcompany.ListQuery{Search: "Ação_1", Page: 2, PageSize: 10}
	mock.ExpectQuery(exact(companyCountSQL+companySearchSQL)).WithArgs(wsUUID, `%Ação\_1%`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(11))
	mock.ExpectQuery(exact(companySelectSQL+companySearchSQL+companyPageSQL)).WithArgs(wsUUID, `%Ação\_1%`, 10, 10).
		WillReturnRows(sqlmock.NewRows(companyRowColumns).
			AddRow(companyUUID, wsUUID, "Ação 1", true, nil, nil, companyAt, companyAt, nil, nil, 3))
	page, err := (&repository{db: db}).ListCompanies(context.Background(), wsUUID, q)
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	if page.TotalItems != 11 || page.TotalPages != 2 || page.Page != 2 || len(page.Items) != 1 || page.Items[0].MembersCount != 3 {
		t.Fatalf("page = %+v", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteCompanyReleasesEveryMemberWithItsEventInTheSameTransaction(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(exact(lockCompanyForDeleteSQL)).WithArgs(wsUUID, companyUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(companyUUID))
	mock.ExpectExec(exact(deleteCompanySQL)).WithArgs(companyAt, companyAt, wsUUID, companyUUID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(exact(releaseMembersSQL)).WithArgs(sqlmock.AnyArg(), wsUUID, companyUUID).
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "version"}).AddRow(leadUUID, 5).AddRow(otherLeadID, 2))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	released, err := (&repository{db: db}).DeleteCompany(context.Background(), wsUUID, companyUUID, lead.CompanyReleaseEvent(actorUUID, companyUUID), companyAt)
	if err != nil {
		t.Fatalf("DeleteCompany: %v", err)
	}
	want := []leadcompany.ReleasedLead{{LeadID: leadUUID, Version: 5}, {LeadID: otherLeadID, Version: 2}}
	if len(released) != 2 || released[0] != want[0] || released[1] != want[1] {
		t.Fatalf("released = %+v, want %+v", released, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteCompanyOfAMissingCompanyWritesNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(exact(lockCompanyForDeleteSQL)).WithArgs(wsUUID, companyUUID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	if _, err := (&repository{db: db}).DeleteCompany(context.Background(), wsUUID, companyUUID, lead.CompanyReleaseEvent(actorUUID, companyUUID), companyAt); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryCompanyStatementBindsAsManyArgumentsAsPlaceholders(t *testing.T) {
	cases := map[string]int{
		insertCompanySQL:                                     8,
		updateCompanySQL:                                     6,
		companySelectSQL + " AND c.id = ?":                   2,
		companyCountSQL + companySearchSQL:                   2,
		companySelectSQL + companySearchSQL + companyPageSQL: 4,
		lockCompanyForDeleteSQL:                              2,
		deleteCompanySQL:                                     4,
		releaseMembersSQL:                                    3,
	}
	for sql, want := range cases {
		if got := strings.Count(sql, "?"); got != want {
			t.Errorf("%d placeholders, want %d in %s", got, want, sql)
		}
	}
}
