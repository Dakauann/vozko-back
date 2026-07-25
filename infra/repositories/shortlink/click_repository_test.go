package shortlink

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
)

func sampleClick() *shortlink.Click {
	return &shortlink.Click{
		ID:          "evt-1",
		ShortLinkID: "l-1",
		WorkspaceID: "ws-1",
		OccurredAt:  time.Now(),
		Country:     "BR",
	}
}

func TestClickRepo_RecordClick(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`INSERT INTO "short_link_clicks"`).WillReturnResult(sqlmock.NewResult(1, 1))
		isNew, err := NewClickRepository(db).RecordClick(context.Background(), sampleClick())
		if err != nil || !isNew {
			t.Fatalf("record = %v %v", isNew, err)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`INSERT INTO "short_link_clicks"`).WillReturnResult(sqlmock.NewResult(0, 0))
		isNew, err := NewClickRepository(db).RecordClick(context.Background(), sampleClick())
		if err != nil || isNew {
			t.Fatalf("dup = %v %v", isNew, err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`INSERT INTO "short_link_clicks"`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).RecordClick(context.Background(), sampleClick()); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestClickRepo_ApplyDailyStats(t *testing.T) {
	deltas := []shortlink.DailyStatDelta{
		{ShortLinkID: "l", WorkspaceID: "ws", Day: time.Now(), Dimension: "total", Clicks: 1, UniqueClicks: 1},
	}

	t.Run("empty", func(t *testing.T) {
		db, _, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		if err := NewClickRepository(db).ApplyDailyStats(context.Background(), nil); err != nil {
			t.Fatalf("empty: %v", err)
		}
	})
	t.Run("success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "short_link_daily_stats"`).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		if err := NewClickRepository(db).ApplyDailyStats(context.Background(), deltas); err != nil {
			t.Fatalf("apply: %v", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "short_link_daily_stats"`).WillReturnError(errors.New("db"))
		mock.ExpectRollback()
		if err := NewClickRepository(db).ApplyDailyStats(context.Background(), deltas); err == nil {
			t.Fatal("expected error")
		}
	})
}

func analyticsInput() shortlink.AnalyticsInput {
	return shortlink.AnalyticsInput{ShortLinkID: "l", WorkspaceID: "ws", From: time.Now().Add(-time.Hour), To: time.Now()}
}

func expectTotals(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`AS total`).WillReturnRows(sqlmock.NewRows([]string{"total", "unique"}).AddRow(10, 6))
}
func expectSeries(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`day AS day`).WillReturnRows(sqlmock.NewRows([]string{"day", "clicks"}).AddRow(time.Now(), 4))
}

func TestClickRepo_Analytics(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		expectTotals(mock)
		expectSeries(mock)
		for range 5 {
			mock.ExpectQuery(`dimension_value AS value`).WillReturnRows(sqlmock.NewRows([]string{"value", "clicks"}).AddRow("BR", 3))
		}
		res, err := NewClickRepository(db).Analytics(context.Background(), analyticsInput())
		if err != nil || res.TotalClicks != 10 || res.UniqueClicks != 6 || len(res.TimeSeries) != 1 {
			t.Fatalf("analytics = %v %+v", err, res)
		}
		if len(res.ByCountry) != 1 || res.ByCountry[0].Label != "BR" {
			t.Fatalf("breakdown = %+v", res.ByCountry)
		}
	})
	t.Run("totals error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`AS total`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).Analytics(context.Background(), analyticsInput()); err == nil {
			t.Fatal("expected totals error")
		}
	})
	t.Run("series error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		expectTotals(mock)
		mock.ExpectQuery(`day AS day`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).Analytics(context.Background(), analyticsInput()); err == nil {
			t.Fatal("expected series error")
		}
	})
	t.Run("breakdown error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		expectTotals(mock)
		expectSeries(mock)
		mock.ExpectQuery(`dimension_value AS value`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).Analytics(context.Background(), analyticsInput()); err == nil {
			t.Fatal("expected breakdown error")
		}
	})
}

func TestClickRepo_RecentClicks(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		now := time.Now()
		mock.ExpectQuery(`SELECT \* FROM "short_link_clicks"`).WillReturnRows(
			sqlmock.NewRows(clickColumns()).AddRow(
				"evt-1", "l-1", "ws-1", now, "hash", "BR", "SP", "City",
				"desktop", "Windows", "Chrome", "ref.com", "s", "m", "c", false, false, "pt", now,
			),
		)
		res, err := NewClickRepository(db).RecentClicks(context.Background(), "ws-1", "l-1", shared.Pagination{Page: 1, PageSize: 20})
		if err != nil || res.TotalItems != 1 || len(res.Items) != 1 {
			t.Fatalf("recent = %v %+v", err, res)
		}
	})
	t.Run("count error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).RecentClicks(context.Background(), "ws", "l", shared.Pagination{}); err == nil {
			t.Fatal("expected count error")
		}
	})
	t.Run("find error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(`SELECT \* FROM "short_link_clicks"`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).RecentClicks(context.Background(), "ws", "l", shared.Pagination{}); err == nil {
			t.Fatal("expected find error")
		}
	})
}

func TestClickRepo_Purge(t *testing.T) {
	t.Run("clicks success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`DELETE FROM "short_link_clicks"`).WillReturnResult(sqlmock.NewResult(0, 5))
		n, err := NewClickRepository(db).PurgeClicksBefore(context.Background(), time.Now())
		if err != nil || n != 5 {
			t.Fatalf("purge clicks = %v %d", err, n)
		}
	})
	t.Run("clicks error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`DELETE FROM "short_link_clicks"`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).PurgeClicksBefore(context.Background(), time.Now()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("daily success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`DELETE FROM "short_link_daily_stats"`).WillReturnResult(sqlmock.NewResult(0, 2))
		n, err := NewClickRepository(db).PurgeDailyStatsBefore(context.Background(), time.Now())
		if err != nil || n != 2 {
			t.Fatalf("purge daily = %v %d", err, n)
		}
	})
	t.Run("daily error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`DELETE FROM "short_link_daily_stats"`).WillReturnError(errors.New("db"))
		if _, err := NewClickRepository(db).PurgeDailyStatsBefore(context.Background(), time.Now()); err == nil {
			t.Fatal("expected error")
		}
	})
}
