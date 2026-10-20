package campaignstamp

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/campaign"
)

func timelineDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func TestTheSpanRunsFromTheFirstSendOrFailureToTheLastMilestone(t *testing.T) {
	db, mock, sqlDB := timelineDB(t)
	defer sqlDB.Close()
	first := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	last := time.Date(2026, 10, 20, 11, 30, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT MIN\(LEAST\(sent_at, failed_at\)\) AS first, MAX\(GREATEST\(sent_at, delivered_at, read_at, failed_at\)\) AS last FROM whatsapp_campaign_entries WHERE campaign_id = \$1 AND deleted_at IS NULL`).
		WithArgs("camp-1").
		WillReturnRows(sqlmock.NewRows([]string{"first", "last"}).AddRow(first, last))

	span, err := NewTimelineReader(db, OfficialEntries).Span("camp-1")
	if err != nil {
		t.Fatalf("Span = %v", err)
	}
	if !span.First.Equal(first) || !span.Last.Equal(last) {
		t.Fatalf("span = %v to %v", span.First, span.Last)
	}
}

func TestACampaignWithNoStampsHasNoSpan(t *testing.T) {
	db, mock, sqlDB := timelineDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`FROM unofficial_whatsapp_campaign_entries`).WillReturnRows(sqlmock.NewRows([]string{"first", "last"}).AddRow(nil, nil))

	span, err := NewTimelineReader(db, UnofficialEntries).Span("camp-1")
	if err != nil || span.First != nil || span.Last != nil {
		t.Fatalf("span = %+v, %v", span, err)
	}
}

func TestBucketsAreCountedInOnePassOverTheCampaignEntries(t *testing.T) {
	db, mock, sqlDB := timelineDB(t)
	defer sqlDB.Close()
	from := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 20, 10, 2, 0, 0, time.UTC)
	plan := campaign.TimelinePlan{Bucket: campaign.TimelineMinute, From: from, To: to, Location: time.UTC}
	mock.ExpectQuery(`SELECT date_trunc\(\$1, m\.at AT TIME ZONE \$2\) AT TIME ZONE \$3 AS bucket, m\.kind, COUNT\(\*\) AS total `+
		`FROM whatsapp_campaign_entries e CROSS JOIN LATERAL \(VALUES \('sent', e\.sent_at\), \('delivered', e\.delivered_at\), \('read', e\.read_at\), \('failed', e\.failed_at\)\) AS m\(kind, at\) `+
		`WHERE e\.campaign_id = \$4 AND e\.deleted_at IS NULL AND m\.at >= \$5 AND m\.at <= \$6 GROUP BY 1, 2 ORDER BY 1`).
		WithArgs("minute", "UTC", "UTC", "camp-1", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"bucket", "kind", "total"}).
			AddRow(from, "sent", 4).
			AddRow(from, "delivered", 3).
			AddRow(from.Add(time.Minute), "sent", 2))

	points, err := NewTimelineReader(db, OfficialEntries).Count("camp-1", plan)
	if err != nil {
		t.Fatalf("Count = %v", err)
	}
	want := []campaign.TimelinePoint{{At: from, Sent: 4, Delivered: 3}, {At: from.Add(time.Minute), Sent: 2}}
	if !reflect.DeepEqual(points, want) {
		t.Fatalf("points = %+v, want %+v", points, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
