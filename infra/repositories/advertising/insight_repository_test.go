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

func day(raw string) time.Time {
	d, err := advertising.ParseDay(raw)
	if err != nil {
		panic(err)
	}
	return d
}

func insightRow(ad, raw string) advertising.DailyInsight {
	return advertising.DailyInsight{
		AdMetaID: ad, CampaignMetaID: "c-1", AdSetMetaID: "s-1", Day: day(raw), Currency: "BRL",
		SpendMicros: 1_000_000, Impressions: 100, Clicks: 5, LinkClicks: 3,
		Actions: map[string]int64{advertising.ActionConversationStarted: 2},
	}
}

func TestReplaceDaysDeletesTheRangeAndInsertsInOneTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	r := advertising.DateRange{Since: day("2026-09-01"), Until: day("2026-09-30")}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ad_insights_daily" WHERE ad_account_id = $1 AND day >= $2 AND day <= $3`)).
		WithArgs("a-1", r.Since, r.Until).
		WillReturnResult(sqlmock.NewResult(0, 9))
	mock.ExpectExec(`INSERT INTO "ad_insights_daily" \("ad_meta_id","day","ad_account_id",.*\) VALUES \(.*\),\(.*\)`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	rows := []advertising.DailyInsight{insightRow("ad-1", "2026-09-01"), insightRow("ad-1", "2026-09-30")}
	if err := NewInsightRepository(db).ReplaceDays(context.Background(), "a-1", r, rows); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestReplaceDaysWithNoRowsOnlyClearsTheRange(t *testing.T) {
	db, mock := newMockDB(t)
	r := advertising.DateRange{Since: day("2026-09-01"), Until: day("2026-09-02")}
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "ad_insights_daily"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	if err := NewInsightRepository(db).ReplaceDays(context.Background(), "a-1", r, nil); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestReplaceDaysRejectsRowsOutsideTheRangeOrAccount(t *testing.T) {
	db, mock := newMockDB(t)
	r := advertising.DateRange{Since: day("2026-09-01"), Until: day("2026-09-02")}
	outside := insightRow("ad-1", "2026-09-03")
	foreign := insightRow("ad-1", "2026-09-01")
	foreign.AdAccountID = "a-2"
	repo := NewInsightRepository(db)
	for _, row := range []advertising.DailyInsight{outside, foreign} {
		if err := repo.ReplaceDays(context.Background(), "a-1", r, []advertising.DailyInsight{row}); err == nil {
			t.Fatalf("accepted %+v", row)
		}
	}
	if err := repo.ReplaceDays(context.Background(), "a-1", advertising.DateRange{}, nil); !errors.Is(err, advertising.ErrInvalidRange) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestInsightRowsAreOrderedByDayThenAd(t *testing.T) {
	db, mock := newMockDB(t)
	r := advertising.DateRange{Since: day("2026-09-01"), Until: day("2026-09-30")}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_insights_daily" WHERE ad_account_id = $1 AND day >= $2 AND day <= $3 ORDER BY day, ad_meta_id`)).
		WithArgs("a-1", r.Since, r.Until).
		WillReturnRows(sqlmock.NewRows([]string{"ad_meta_id", "day", "ad_account_id", "currency", "spend_micros", "actions"}).
			AddRow("ad-1", day("2026-09-01"), "a-1", "BRL", int64(42), []byte(`{"lead":3}`)))
	rows, err := NewInsightRepository(db).Rows(context.Background(), "a-1", r)
	if err != nil || len(rows) != 1 {
		t.Fatalf("got %v, %v", rows, err)
	}
	if rows[0].SpendMicros != 42 || rows[0].Actions[advertising.ActionLead] != 3 || rows[0].AdAccountID != "a-1" {
		t.Fatalf("got %+v", rows[0])
	}
}

func TestAdDayWithoutRowIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_insights_daily" WHERE ad_meta_id = $1 AND day = $2`)).
		WithArgs("ad-1", day("2026-09-01"), 1).
		WillReturnRows(sqlmock.NewRows([]string{"ad_meta_id"}))
	if _, err := NewInsightRepository(db).AdDay(context.Background(), "ad-1", day("2026-09-01")); !errors.Is(err, advertising.ErrObjectNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}
