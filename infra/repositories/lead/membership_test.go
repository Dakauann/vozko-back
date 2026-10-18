package lead

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const companyUUID = "3c2b1a00-7d1a-4c61-9a0e-2f8d4c1b2a10"

func memberLead(companyID string) *lead.Lead {
	return &lead.Lead{ID: leadUUID, WorkspaceID: wsUUID, Name: "Ana", CompanyID: companyID, Version: 4}
}

func joinEvents() []recordevent.Event {
	return []recordevent.Event{lead.CompanyReleaseEvent(actorUUID, companyUUID)}
}

func TestSaveMembershipLocksAnOpenCompanyAndBumpsTheVersionWithTheEvent(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(exact(lockCompanySQL)).WithArgs(wsUUID, companyUUID).
		WillReturnRows(sqlmock.NewRows([]string{"accepts_leads"}).AddRow(true))
	mock.ExpectQuery(exact(saveMembershipSQL)).WithArgs(companyUUID, sqlmock.AnyArg(), leadUUID, wsUUID, int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	l := memberLead(companyUUID)
	if err := (&repository{db: db}).SaveMembership(context.Background(), l, 4, joinEvents()); err != nil {
		t.Fatalf("SaveMembership: %v", err)
	}
	if l.Version != 5 || l.UpdatedAt.IsZero() {
		t.Fatalf("lead = %+v", l)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveMembershipRefusesAClosedOrMissingCompanyBeforeWriting(t *testing.T) {
	cases := map[string]struct {
		rows *sqlmock.Rows
		want error
	}{
		"closed":  {sqlmock.NewRows([]string{"accepts_leads"}).AddRow(false), leadcompany.ErrClosed},
		"missing": {sqlmock.NewRows([]string{"accepts_leads"}), leadcompany.ErrNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(exact(lockCompanySQL)).WithArgs(wsUUID, companyUUID).WillReturnRows(tc.rows)
			mock.ExpectRollback()
			if err := (&repository{db: db}).SaveMembership(context.Background(), memberLead(companyUUID), 4, joinEvents()); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSaveMembershipTreatsACompanyIDThatIsNotAUUIDAsMissing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if err := (&repository{db: db}).SaveMembership(context.Background(), memberLead("acme"), 4, nil); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLeavingACompanyLocksNoCompany(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(exact(saveMembershipSQL)).WithArgs(nil, sqlmock.AnyArg(), leadUUID, wsUUID, int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	mock.ExpectCommit()
	if err := (&repository{db: db}).SaveMembership(context.Background(), memberLead(""), 4, nil); err != nil {
		t.Fatalf("SaveMembership: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveMembershipTellsAStaleVersionFromAMissingLead(t *testing.T) {
	cases := map[string]struct {
		current *sqlmock.Rows
		want    error
	}{
		"stale":   {sqlmock.NewRows([]string{"version"}).AddRow(7), shared.ErrVersionConflict},
		"missing": {sqlmock.NewRows([]string{"version"}), lead.ErrLeadNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(exact(saveMembershipSQL)).WillReturnRows(sqlmock.NewRows([]string{"version"}))
			mock.ExpectQuery(exact(currentVersionSQL)).WithArgs(leadUUID, wsUUID).WillReturnRows(tc.current)
			mock.ExpectRollback()
			if err := (&repository{db: db}).SaveMembership(context.Background(), memberLead(""), 4, nil); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
