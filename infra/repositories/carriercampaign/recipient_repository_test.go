package carriercampaign_repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriercampaign"
	"vozko/domain/shared"
)

const recipientID = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"

var recipientColumns = []string{"id", "workspace_id", "campaign_id", "run_id", "lead_id", "phone", "variant", "status", "skip_reason", "message_id", "error_code", "sent_at", "created_at", "updated_at"}

func recipientRows(status, skip string) *sqlmock.Rows {
	return sqlmock.NewRows(recipientColumns).AddRow(recipientID, "ws1", campaignID, runID, "l1", "5511999990000", 1, status, skip, nil, "", nil, testNow, testNow)
}

func TestRecipientCreateManyIsBatchedAndIdempotent(t *testing.T) {
	db, mock := newMockDB(t)
	recipients := make([]carriercampaign.Recipient, recipientBatch+1)
	for i := range recipients {
		recipients[i] = carriercampaign.Recipient{WorkspaceID: "ws1", CampaignID: campaignID, RunID: runID, LeadID: fmt.Sprintf("lead-%d", i), Phone: "5511999990000", Status: carriercampaign.RecipientPending}
	}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "carrier_campaign_recipients" .* ON CONFLICT \("run_id","lead_id"\) DO NOTHING`).WillReturnResult(sqlmock.NewResult(0, recipientBatch))
	mock.ExpectExec(`INSERT INTO "carrier_campaign_recipients" .* ON CONFLICT \("run_id","lead_id"\) DO NOTHING`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := NewRecipientRepository(db).CreateMany(context.Background(), recipients); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestRecipientCreateManyRefusesUnknownValues(t *testing.T) {
	cases := []carriercampaign.Recipient{
		{RunID: runID, LeadID: "l1", Status: "queued"},
		{RunID: runID, LeadID: "l1", Status: carriercampaign.RecipientSkipped, SkipReason: "mood"},
	}
	for _, r := range cases {
		db, mock := newMockDB(t)
		if err := NewRecipientRepository(db).CreateMany(context.Background(), []carriercampaign.Recipient{r}); !errors.Is(err, errUnknownStoredValue) {
			t.Fatalf("%+v: got %v", r, err)
		}
		expectationsMet(t, mock)
	}
	db, mock := newMockDB(t)
	if err := NewRecipientRepository(db).CreateMany(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestRecipientMarksOnlyFromPending(t *testing.T) {
	cases := []struct {
		name  string
		query string
		run   func(carriercampaign.RecipientRepository) (bool, error)
	}{
		{"sent", `UPDATE "carrier_campaign_recipients" SET "message_id"=\$1,"sent_at"=\$2,"status"=\$3,"updated_at"=\$4 WHERE id = \$5 AND status = \$6`,
			func(r carriercampaign.RecipientRepository) (bool, error) {
				return r.MarkSent(context.Background(), recipientID, "m1", testNow)
			}},
		{"skipped", `UPDATE "carrier_campaign_recipients" SET "skip_reason"=\$1,"status"=\$2,"updated_at"=\$3 WHERE id = \$4 AND status = \$5`,
			func(r carriercampaign.RecipientRepository) (bool, error) {
				return r.MarkSkipped(context.Background(), recipientID, carriercampaign.SkipSuppressed)
			}},
		{"failed", `UPDATE "carrier_campaign_recipients" SET "error_code"=\$1,"status"=\$2,"updated_at"=\$3 WHERE id = \$4 AND status = \$5`,
			func(r carriercampaign.RecipientRepository) (bool, error) {
				return r.MarkFailed(context.Background(), recipientID, "charge_refused")
			}},
	}
	outcomes := []struct {
		name     string
		affected int64
		exists   *int
		want     bool
		wantErr  error
	}{
		{"moved", 1, nil, true, nil},
		{"already settled", 0, new(1), false, nil},
		{"missing", 0, new(0), false, carriercampaign.ErrRecipientNotFound},
	}
	for _, tc := range cases {
		for _, o := range outcomes {
			t.Run(tc.name+" "+o.name, func(t *testing.T) {
				db, mock := newMockDB(t)
				mock.ExpectExec(tc.query).WillReturnResult(sqlmock.NewResult(0, o.affected))
				if o.exists != nil {
					mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_campaign_recipients" WHERE id = \$1`).
						WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(*o.exists))
				}
				got, err := tc.run(NewRecipientRepository(db))
				if got != o.want || !errors.Is(err, o.wantErr) {
					t.Fatalf("got %v %v", got, err)
				}
				expectationsMet(t, mock)
			})
		}
	}
}

func TestRecipientMarksValidateTheirInput(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecipientRepository(db)
	if _, err := repo.MarkSent(context.Background(), recipientID, " ", testNow); !errors.Is(err, carriercampaign.ErrMessageIDRequired) {
		t.Fatalf("sent: %v", err)
	}
	if _, err := repo.MarkSkipped(context.Background(), recipientID, "mood"); !errors.Is(err, carriercampaign.ErrSkipReasonInvalid) {
		t.Fatalf("skipped: %v", err)
	}
	if _, err := repo.MarkFailed(context.Background(), recipientID, ""); !errors.Is(err, carriercampaign.ErrErrorCodeRequired) {
		t.Fatalf("failed: %v", err)
	}
	expectationsMet(t, mock)
}

func TestRecipientPendingIDs(t *testing.T) {
	cases := []struct {
		name  string
		after string
		query string
	}{
		{"first page", "", `SELECT "id" FROM "carrier_campaign_recipients" WHERE run_id = \$1 AND status = \$2 ORDER BY id ASC LIMIT \$3`},
		{"next page", recipientID, `SELECT "id" FROM "carrier_campaign_recipients" WHERE \(run_id = \$1 AND status = \$2\) AND id > \$3 ORDER BY id ASC LIMIT \$4`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(tc.query).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("x"))
			got, err := NewRecipientRepository(db).PendingIDs(context.Background(), runID, tc.after, 100)
			if err != nil || len(got) != 1 {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestRecipientFindersAndList(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecipientRepository(db)
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaign_recipients" WHERE id = \$1`).WillReturnRows(recipientRows("skipped", "suppressed"))
	got, err := repo.FindByID(context.Background(), recipientID)
	if err != nil || got.SkipReason != carriercampaign.SkipSuppressed || got.Variant != 1 || got.LeadID != "l1" {
		t.Fatalf("by id %+v %v", got, err)
	}
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaign_recipients" WHERE message_id = \$1`).WillReturnRows(sqlmock.NewRows(recipientColumns))
	if _, err := repo.FindByMessageID(context.Background(), runID); !errors.Is(err, carriercampaign.ErrRecipientNotFound) {
		t.Fatalf("by message: %v", err)
	}
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaign_recipients"`).WillReturnRows(recipientRows("queued", ""))
	if _, err := repo.FindByID(context.Background(), recipientID); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("unknown status: %v", err)
	}
	variant := 1
	mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_campaign_recipients" WHERE \(workspace_id = \$1 AND run_id = \$2\) AND status = \$3 AND skip_reason = \$4 AND variant = \$5 AND phone LIKE \$6`).
		WithArgs("ws1", runID, "skipped", "suppressed", 1, "%9999%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaign_recipients" WHERE .* ORDER BY id ASC LIMIT \$7`).WillReturnRows(recipientRows("skipped", "suppressed"))
	page, err := repo.List(context.Background(), carriercampaign.ListRecipientsInput{
		WorkspaceID: "ws1", RunID: runID, Status: carriercampaign.RecipientSkipped, SkipReason: carriercampaign.SkipSuppressed,
		Variant: &variant, Search: " 9999 ", Pagination: shared.Pagination{Page: 1, PageSize: 20},
	})
	if err != nil || page.TotalItems != 1 || len(page.Items) != 1 {
		t.Fatalf("list %+v %v", page, err)
	}
	expectationsMet(t, mock)
}

func TestRecipientCounts(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecipientRepository(db)
	mock.ExpectQuery(`SELECT status, skip_reason, count\(\*\) AS total FROM "carrier_campaign_recipients" WHERE run_id = \$1 GROUP BY status, skip_reason`).
		WillReturnRows(sqlmock.NewRows([]string{"status", "skip_reason", "total"}).
			AddRow("pending", "", 2).AddRow("sent", "", 5).AddRow("failed", "", 1).AddRow("skipped", "suppressed", 3).AddRow("skipped", "balance", 1))
	counts, err := repo.Counts(context.Background(), runID)
	if err != nil || counts.Total != 12 || counts.Pending != 2 || counts.Sent != 5 || counts.Failed != 1 ||
		counts.Skipped[carriercampaign.SkipSuppressed] != 3 || counts.SkippedTotal() != 4 {
		t.Fatalf("counts %+v %v", counts, err)
	}
	mock.ExpectQuery(`SELECT variant, status, skip_reason, count\(\*\) AS total FROM "carrier_campaign_recipients" WHERE run_id = \$1 GROUP BY variant, status, skip_reason`).
		WillReturnRows(sqlmock.NewRows([]string{"variant", "status", "skip_reason", "total"}).AddRow(0, "sent", "", 4).AddRow(1, "sent", "", 6).AddRow(1, "skipped", "duplicate", 1))
	byVariant, err := repo.CountsByVariant(context.Background(), runID)
	if err != nil || byVariant[0].Sent != 4 || byVariant[1].Total != 7 || byVariant[1].Skipped[carriercampaign.SkipDuplicate] != 1 || byVariant[0].Skipped == nil {
		t.Fatalf("by variant %+v %v", byVariant, err)
	}
	mock.ExpectQuery(`SELECT status, skip_reason`).WillReturnRows(sqlmock.NewRows([]string{"status", "skip_reason", "total"}).AddRow("skipped", "mood", 1))
	if _, err := repo.Counts(context.Background(), runID); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("unknown skip reason: %v", err)
	}
	expectationsMet(t, mock)
}
