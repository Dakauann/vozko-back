package emailcampaign

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domain "vozko/domain/emailcampaign"
)

func TestSummariesAndDailyQueriesBindEveryPlaceholder(t *testing.T) {
	cases := map[string]struct {
		sql  string
		want int
	}{
		"summaries": {summariesSQL, 2},
		"timeline":  {timelineSQL, 5},
		"daily":     {overviewDailySQL, 4},
	}
	for name, tc := range cases {
		if got := len(regexp.MustCompile(`\?`).FindAllStringIndex(tc.sql, -1)); got != tc.want {
			t.Errorf("%s binds %d placeholders, want %d", name, got, tc.want)
		}
	}
	if !regexp.MustCompile(regexp.QuoteMeta(`m.workspace_id = ? AND m.run_kind = 'campaign' AND m.run_id IN ?`)).MatchString(summariesSQL) {
		t.Error("summaries must be scoped to campaign runs of the workspace")
	}
}

func TestSetSettlementOnlySettlesOnce(t *testing.T) {
	db, mock := newMockDB(t)
	at := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "email_campaigns" SET "refunded_micros"=$1,"refunded_units"=$2,"settled_at"=$3,"updated_at"=$4 WHERE id = $5 AND settled_at IS NULL`)).
		WithArgs(int64(2400), int64(4), at, sqlmock.AnyArg(), "c1").WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err := NewCampaignRepository(db).SetSettlement(context.Background(), "c1", domain.RunSettlement{RefundedUnits: 4, RefundedMicros: 2400, SettledAt: at})
	if err != nil || ok {
		t.Fatalf("SetSettlement() = %v, %v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
