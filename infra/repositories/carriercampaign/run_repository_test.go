package carriercampaign_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/campaign"
	"vozko/domain/carriercampaign"
	"vozko/domain/shared"
)

var runColumns = []string{"id", "workspace_id", "campaign_id", "occurrence", "status", "started_at", "completed_at", "created_at", "updated_at"}

func runRows(status string) *sqlmock.Rows {
	return sqlmock.NewRows(runColumns).AddRow(runID, "ws1", campaignID, testNow, status, testNow, nil, testNow, testNow)
}

func TestRunCreateIsIdempotentPerOccurrence(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		db, mock := newMockDB(t)
		mock.ExpectExec(`INSERT INTO "carrier_campaign_runs" .* ON CONFLICT \("campaign_id","occurrence"\) DO NOTHING`).
			WithArgs(sqlmock.AnyArg(), "ws1", campaignID, testNow, "RUNNING", testNow, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, affected))
		r := &carriercampaign.Run{WorkspaceID: "ws1", CampaignID: campaignID, Occurrence: testNow, Status: campaign.StatusRunning, StartedAt: testNow}
		created, err := NewRunRepository(db).Create(context.Background(), r)
		if err != nil || created != (affected == 1) {
			t.Fatalf("affected %d: got %v %v", affected, created, err)
		}
		if created && r.ID == "" {
			t.Fatal("the new run id was not written back")
		}
		expectationsMet(t, mock)
	}
}

func TestRunCreateRefusesAnUnknownStatus(t *testing.T) {
	db, mock := newMockDB(t)
	if _, err := NewRunRepository(db).Create(context.Background(), &carriercampaign.Run{CampaignID: campaignID, Status: "GOING"}); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestRunFinders(t *testing.T) {
	cases := []struct {
		name  string
		query string
		args  []driver.Value
		run   func(carriercampaign.RunRepository) (*carriercampaign.Run, error)
	}{
		{"by id", `SELECT \* FROM "carrier_campaign_runs" WHERE id = \$1`, []driver.Value{runID, 1},
			func(r carriercampaign.RunRepository) (*carriercampaign.Run, error) {
				return r.FindByID(context.Background(), runID)
			}},
		{"by occurrence", `SELECT \* FROM "carrier_campaign_runs" WHERE campaign_id = \$1 AND occurrence = \$2`, []driver.Value{campaignID, testNow, 1},
			func(r carriercampaign.RunRepository) (*carriercampaign.Run, error) {
				return r.FindByOccurrence(context.Background(), campaignID, testNow)
			}},
		{"active", `SELECT \* FROM "carrier_campaign_runs" WHERE campaign_id = \$1 AND status IN \(\$2,\$3\) ORDER BY occurrence DESC LIMIT \$4`, []driver.Value{campaignID, "RUNNING", "PAUSED", 1},
			func(r carriercampaign.RunRepository) (*carriercampaign.Run, error) {
				return r.FindActive(context.Background(), campaignID)
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(tc.query).WithArgs(tc.args...).WillReturnRows(runRows("RUNNING"))
			got, err := tc.run(NewRunRepository(db))
			if err != nil || got.ID != runID || got.Status != campaign.StatusRunning {
				t.Fatalf("got %+v %v", got, err)
			}
			expectationsMet(t, mock)

			db, mock = newMockDB(t)
			mock.ExpectQuery(tc.query).WillReturnRows(sqlmock.NewRows(runColumns))
			if _, err := tc.run(NewRunRepository(db)); !errors.Is(err, carriercampaign.ErrRunNotFound) {
				t.Fatalf("missing: %v", err)
			}
			db, mock = newMockDB(t)
			mock.ExpectQuery(tc.query).WillReturnRows(runRows("GOING"))
			if _, err := tc.run(NewRunRepository(db)); !errors.Is(err, errUnknownStoredValue) {
				t.Fatalf("unknown status: %v", err)
			}
		})
	}
}

func TestRunListsAndCounts(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRunRepository(db)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_campaign_runs" WHERE campaign_id = \$1`).WithArgs(campaignID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaign_runs" WHERE campaign_id = \$1 ORDER BY occurrence DESC, id DESC LIMIT \$2`).
		WillReturnRows(runRows("COMPLETED"))
	page, err := repo.ListByCampaign(context.Background(), campaignID, shared.Pagination{Page: 1, PageSize: 2})
	if err != nil || page.TotalItems != 3 || len(page.Items) != 1 || page.Items[0].Status != campaign.StatusCompleted {
		t.Fatalf("page %+v %v", page, err)
	}
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaign_runs" WHERE status IN \(\$1\) ORDER BY created_at ASC`).WithArgs("PAUSED").
		WillReturnRows(runRows("PAUSED"))
	if got, err := repo.ListByStatus(context.Background(), campaign.StatusPaused); err != nil || len(got) != 1 {
		t.Fatalf("by status %v %v", got, err)
	}
	mock.ExpectQuery(`SELECT status, count\(\*\) AS total FROM "carrier_campaign_runs" WHERE campaign_id = \$1 GROUP BY "status"`).
		WillReturnRows(sqlmock.NewRows([]string{"status", "total"}).AddRow("COMPLETED", 4))
	if counts, err := repo.CountByStatus(context.Background(), campaignID); err != nil || counts[campaign.StatusCompleted] != 4 {
		t.Fatalf("counts %v %v", counts, err)
	}
	expectationsMet(t, mock)
}

func TestRunStatusWrites(t *testing.T) {
	cases := []struct {
		name  string
		query string
		args  []driver.Value
		run   func(carriercampaign.RunRepository) (bool, error)
	}{
		{"swap", `UPDATE "carrier_campaign_runs" SET "status"=\$1,"updated_at"=\$2 WHERE id = \$3 AND status = \$4`,
			[]driver.Value{"PAUSED", sqlmock.AnyArg(), runID, "RUNNING"},
			func(r carriercampaign.RunRepository) (bool, error) {
				return r.SwapStatus(context.Background(), runID, campaign.StatusRunning, campaign.StatusPaused)
			}},
		{"complete", `UPDATE "carrier_campaign_runs" SET "completed_at"=\$1,"status"=\$2,"updated_at"=\$3 WHERE id = \$4 AND status = \$5`,
			[]driver.Value{testNow, "COMPLETED", sqlmock.AnyArg(), runID, "RUNNING"},
			func(r carriercampaign.RunRepository) (bool, error) {
				return r.Complete(context.Background(), runID, testNow)
			}},
	}
	for _, tc := range cases {
		for _, affected := range []int64{0, 1} {
			t.Run(tc.name, func(t *testing.T) {
				db, mock := newMockDB(t)
				mock.ExpectExec(tc.query).WithArgs(tc.args...).WillReturnResult(sqlmock.NewResult(0, affected))
				got, err := tc.run(NewRunRepository(db))
				if err != nil || got != (affected == 1) {
					t.Fatalf("got %v %v", got, err)
				}
				expectationsMet(t, mock)
			})
		}
	}
	db, mock := newMockDB(t)
	if _, err := NewRunRepository(db).SwapStatus(context.Background(), runID, "GOING", campaign.StatusPaused); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("unknown status: %v", err)
	}
	expectationsMet(t, mock)
}
