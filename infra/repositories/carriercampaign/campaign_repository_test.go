package carriercampaign_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

func sampleCampaign() *carriercampaign.Campaign {
	at := testNow.Add(time.Hour)
	return &carriercampaign.Campaign{
		WorkspaceID: "ws1", Name: "Black Friday", SenderID: "s1", Channel: carriermessaging.ChannelRCS,
		Status: carriercampaign.StatusDraft, Kind: carriercampaign.KindMarketing,
		Variants:    []carriercampaign.Variant{{Name: "A", Content: rcs.Content{Text: "oi"}}},
		Fallback:    carriercampaign.Fallback{SMS: &carriercampaign.SMSFallback{RouteID: "r1", Text: "oi"}},
		Audience:    selection.Selection{Mode: selection.ModeFirstN, Limit: 50, ExpectedCount: 40, Fingerprint: "fp"},
		ScheduledAt: &at, CreatedBy: "u1",
	}
}

func TestCampaignCreateStoresTheJSONDocuments(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "carrier_campaigns"`).
		WithArgs(sqlmock.AnyArg(), "ws1", "Black Friday", "s1", "rcs", "draft", "marketing",
			`[{"name":"A","content":{"text":"oi"}}]`, `{"sms":{"route_id":"r1","text":"oi"}}`,
			`{"mode":"first_n","limit":50,"expected_count":40,"fingerprint":"fp"}`,
			sqlmock.AnyArg(), "u1", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	c := sampleCampaign()
	if err := NewCampaignRepository(db).Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.CreatedAt.IsZero() {
		t.Fatalf("got %+v", c)
	}
	expectationsMet(t, mock)
}

func TestCampaignWritesRefuseUnknownValues(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*carriercampaign.Campaign)
	}{
		{"status", func(c *carriercampaign.Campaign) { c.Status = "live" }},
		{"kind", func(c *carriercampaign.Campaign) { c.Kind = "spam" }},
		{"channel", func(c *carriercampaign.Campaign) { c.Channel = "fax" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			c := sampleCampaign()
			tc.modify(c)
			if err := NewCampaignRepository(db).Create(context.Background(), c); !errors.Is(err, errUnknownStoredValue) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestCampaignFindMapsEveryField(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns" WHERE workspace_id = \$1 AND id = \$2`).
		WithArgs("ws1", campaignID, 1).WillReturnRows(campaignRows(defaultCampaignRow()))
	got, err := NewCampaignRepository(db).FindByID(context.Background(), "ws1", campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != carriermessaging.ChannelRCS || got.Status != carriercampaign.StatusScheduled || got.Kind != carriercampaign.KindMarketing ||
		len(got.Variants) != 1 || got.Variants[0].Content.Text != "oi" || got.Fallback.SMS == nil || got.Fallback.SMS.RouteID != "r1" ||
		got.Audience.Mode != selection.ModeFirstN || got.Audience.ExpectedCount != 40 || got.Audience.Fingerprint != "fp" ||
		got.Recurrence == nil || got.Recurrence.Frequency != carriercampaign.FrequencyDaily || got.CreatedBy != "u1" || got.ScheduledAt == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestCampaignReadsFailClosed(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*campaignRow)
		want   error
	}{
		{"status", func(r *campaignRow) { r.status = "live" }, errUnknownStoredValue},
		{"kind", func(r *campaignRow) { r.kind = "spam" }, errUnknownStoredValue},
		{"channel", func(r *campaignRow) { r.channel = "fax" }, errUnknownStoredValue},
		{"variants", func(r *campaignRow) { r.variants = `{` }, nil},
		{"audience", func(r *campaignRow) { r.audience = `[` }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			row := defaultCampaignRow()
			tc.modify(&row)
			mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns"`).WillReturnRows(campaignRows(row))
			got, err := NewCampaignRepository(db).FindByIDUnscoped(context.Background(), campaignID)
			if err == nil || got != nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestCampaignFindMissingOrMalformedIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewCampaignRepository(db)
	if _, err := repo.FindByIDUnscoped(context.Background(), "not-a-uuid"); !errors.Is(err, carriercampaign.ErrCampaignNotFound) {
		t.Fatalf("malformed id: %v", err)
	}
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns"`).WillReturnRows(sqlmock.NewRows(campaignColumns))
	if _, err := repo.FindByIDUnscoped(context.Background(), campaignID); !errors.Is(err, carriercampaign.ErrCampaignNotFound) {
		t.Fatalf("missing: %v", err)
	}
	expectationsMet(t, mock)
}

func TestCampaignUpdateOnlyWhileEditable(t *testing.T) {
	cases := []struct {
		name     string
		status   carriercampaign.Status
		from     carriercampaign.Status
		affected int64
		existing *sqlmock.Rows
		want     error
	}{
		{"updated", carriercampaign.StatusScheduled, carriercampaign.StatusDraft, 1, nil, nil},
		{"ready goes back to draft", carriercampaign.StatusDraft, carriercampaign.StatusReady, 1, nil, nil},
		{"locked", carriercampaign.StatusScheduled, carriercampaign.StatusDraft, 0, campaignRows(defaultCampaignRow()), carriercampaign.ErrNotEditable},
		{"missing", carriercampaign.StatusDraft, carriercampaign.StatusDraft, 0, sqlmock.NewRows(campaignColumns), carriercampaign.ErrCampaignNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "carrier_campaigns" SET .* WHERE workspace_id = \$\d+ AND id = \$\d+ AND status = \$\d+`).
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "ws1", campaignID, string(tc.from)).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if tc.existing != nil {
				mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns"`).WillReturnRows(tc.existing)
			}
			c := sampleCampaign()
			c.ID, c.Status = campaignID, tc.status
			if err := NewCampaignRepository(db).Update(context.Background(), c, tc.from); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestCampaignUpdateRefusesALaunchedTargetStatus(t *testing.T) {
	db, mock := newMockDB(t)
	c := sampleCampaign()
	c.ID, c.Status = campaignID, carriercampaign.StatusRunning
	if err := NewCampaignRepository(db).Update(context.Background(), c, carriercampaign.StatusDraft); !errors.Is(err, carriercampaign.ErrNotEditable) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestCampaignUpdateRefusesALaunchedReadStatus(t *testing.T) {
	db, mock := newMockDB(t)
	c := sampleCampaign()
	c.ID, c.Status = campaignID, carriercampaign.StatusDraft
	if err := NewCampaignRepository(db).Update(context.Background(), c, carriercampaign.StatusRunning); !errors.Is(err, carriercampaign.ErrNotEditable) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestCampaignDeleteOnlyDrafts(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		existing *sqlmock.Rows
		want     error
	}{
		{"deleted", 1, nil, nil},
		{"launched", 0, campaignRows(defaultCampaignRow()), carriercampaign.ErrNotEditable},
		{"missing", 0, sqlmock.NewRows(campaignColumns), carriercampaign.ErrCampaignNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`DELETE FROM "carrier_campaigns" WHERE workspace_id = \$1 AND id = \$2 AND status IN \(\$3,\$4\)`).
				WithArgs("ws1", campaignID, "draft", "ready").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if tc.existing != nil {
				mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns"`).WillReturnRows(tc.existing)
			}
			if err := NewCampaignRepository(db).Delete(context.Background(), "ws1", campaignID); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestCampaignListFiltersAndPages(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_campaigns" WHERE workspace_id = \$1 AND channel = \$2 AND status = \$3 AND kind = \$4 AND sender_id = \$5 AND name ILIKE \$6`).
		WithArgs("ws1", "sms", "draft", "marketing", "s1", "%50\\%%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(21))
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns" WHERE .* ORDER BY created_at DESC, id DESC LIMIT \$7 OFFSET \$8`).
		WillReturnRows(campaignRows(defaultCampaignRow()))
	got, err := NewCampaignRepository(db).List(context.Background(), carriercampaign.ListCampaignsInput{
		WorkspaceID: "ws1", Channel: carriermessaging.ChannelSMS, Status: carriercampaign.StatusDraft, Kind: carriercampaign.KindMarketing,
		SenderID: "s1", Search: " 50% ", Pagination: shared.Pagination{Page: 2, PageSize: 10},
	})
	if err != nil || got.TotalItems != 21 || len(got.Items) != 1 || got.Page != 2 || got.PageSize != 10 {
		t.Fatalf("got %+v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestCampaignStatusWrites(t *testing.T) {
	at := testNow.Add(time.Hour)
	cases := []struct {
		name  string
		query string
		args  []driver.Value
		run   func(carriercampaign.CampaignRepository) (bool, error)
	}{
		{"swap", `UPDATE "carrier_campaigns" SET "status"=\$1,"updated_at"=\$2 WHERE id = \$3 AND status = \$4`,
			[]driver.Value{"running", sqlmock.AnyArg(), campaignID, "scheduled"},
			func(r carriercampaign.CampaignRepository) (bool, error) {
				return r.SwapStatus(context.Background(), campaignID, carriercampaign.StatusScheduled, carriercampaign.StatusRunning)
			}},
		{"reschedule", `UPDATE "carrier_campaigns" SET "scheduled_at"=\$1,"status"=\$2,"updated_at"=\$3 WHERE id = \$4 AND status = \$5`,
			[]driver.Value{at, "scheduled", sqlmock.AnyArg(), campaignID, "running"},
			func(r carriercampaign.CampaignRepository) (bool, error) {
				return r.Reschedule(context.Background(), campaignID, carriercampaign.StatusRunning, carriercampaign.StatusScheduled, &at)
			}},
		{"claim", `UPDATE "carrier_campaigns" SET "scheduled_at"=\$1,"updated_at"=\$2 WHERE id = \$3 AND status = \$4 AND scheduled_at IS NOT NULL AND scheduled_at <= \$5`,
			[]driver.Value{nil, sqlmock.AnyArg(), campaignID, "scheduled", testNow},
			func(r carriercampaign.CampaignRepository) (bool, error) {
				return r.ClaimScheduledStart(campaignID, testNow)
			}},
	}
	for _, tc := range cases {
		for _, affected := range []int64{0, 1} {
			t.Run(tc.name, func(t *testing.T) {
				db, mock := newMockDB(t)
				mock.ExpectExec(tc.query).WithArgs(tc.args...).WillReturnResult(sqlmock.NewResult(0, affected))
				got, err := tc.run(NewCampaignRepository(db))
				if err != nil || got != (affected == 1) {
					t.Fatalf("affected %d: got %v %v", affected, got, err)
				}
				expectationsMet(t, mock)
			})
		}
	}
}

func TestCampaignStatusWritesRefuseUnknownStatuses(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewCampaignRepository(db)
	if _, err := repo.SwapStatus(context.Background(), campaignID, "live", carriercampaign.StatusRunning); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("swap: %v", err)
	}
	if _, err := repo.Reschedule(context.Background(), campaignID, carriercampaign.StatusRunning, "later", nil); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("reschedule: %v", err)
	}
	expectationsMet(t, mock)
}

func TestCampaignScans(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewCampaignRepository(db)
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns" WHERE status = \$1 AND scheduled_at <= \$2 ORDER BY scheduled_at ASC LIMIT \$3`).
		WithArgs("scheduled", testNow, 25).WillReturnRows(campaignRows(defaultCampaignRow()))
	if got, err := repo.ListScheduledDue(context.Background(), testNow, 25); err != nil || len(got) != 1 {
		t.Fatalf("due %v %v", got, err)
	}
	mock.ExpectQuery(`SELECT \* FROM "carrier_campaigns" WHERE status IN \(\$1,\$2\) ORDER BY created_at ASC`).
		WithArgs("running", "paused").WillReturnRows(campaignRows(defaultCampaignRow()))
	if got, err := repo.ListByStatus(context.Background(), carriercampaign.StatusRunning, carriercampaign.StatusPaused); err != nil || len(got) != 1 {
		t.Fatalf("by status %v %v", got, err)
	}
	mock.ExpectQuery(`SELECT status, count\(\*\) AS total FROM "carrier_campaigns" WHERE workspace_id = \$1 GROUP BY "status"`).
		WithArgs("ws1").WillReturnRows(sqlmock.NewRows([]string{"status", "total"}).AddRow("draft", 3).AddRow("running", 1))
	counts, err := repo.CountByStatus(context.Background(), "ws1")
	if err != nil || counts[carriercampaign.StatusDraft] != 3 || counts[carriercampaign.StatusRunning] != 1 {
		t.Fatalf("counts %v %v", counts, err)
	}
	mock.ExpectQuery(`SELECT status, count`).WillReturnRows(sqlmock.NewRows([]string{"status", "total"}).AddRow("live", 1))
	if _, err := repo.CountByStatus(context.Background(), "ws1"); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("unknown status count: %v", err)
	}
	expectationsMet(t, mock)
}
