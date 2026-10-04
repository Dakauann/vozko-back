package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
	attendance_repository "vozko/infra/repositories/attendance"
)

var attributionWindow = struct{ from, to time.Time }{
	from: time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC),
	to:   time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC),
}

func twoCampaigns() []advertising.AdGroup {
	return []advertising.AdGroup{{AdMetaID: "ad-1", Key: "c-1"}, {AdMetaID: "ad-2", Key: "c-2"}}
}

func TestByGroupScopesOriginsToTheWorkspaceThroughEveryChannel(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM conversation_ad_origins cao\s+JOIN \(VALUES \(CAST\(\$1 AS text\), CAST\(\$2 AS text\)\), \(CAST\(\$3 AS text\), CAST\(\$4 AS text\)\)\) AS g\(ad_id, group_key\) ON g\.ad_id = cao\.ad_id\s+`+
		`JOIN \(`+regexp.QuoteMeta(attendance_repository.EntryWorkspaceUnion())+
		`\) e ON e\.entry_id = cao\.entry_id AND e\.entry_type = cao\.entry_type AND e\.workspace_id = \$5\s+`+
		`WHERE cao\.arrived_at >= \$6 AND cao\.arrived_at < \$7\s+`+
		`AND EXISTS \(SELECT 1 FROM ad_objects ao WHERE ao\.meta_id = cao\.ad_id AND ao\.workspace_id = \$8\)`+
		`.*JOIN opportunities o ON o\.id = oc\.opportunity_id AND o\.deleted_at IS NULL AND o\.workspace_id = \$9`+
		`.*COUNT\(DISTINCT currency\) > 1 THEN 'MIXED'`+
		`.*WHERE status = 'won'`).
		WithArgs("ad-1", "c-1", "ad-2", "c-2", "ws", attributionWindow.from, attributionWindow.to, "ws", "ws").
		WillReturnRows(sqlmock.NewRows([]string{"group_key", "conversations", "leads", "won_deals", "revenue", "revenue_currency"}).
			AddRow("c-1", int64(10), int64(4), int64(2), int64(150000), "BRL").
			AddRow("c-2", int64(3), int64(1), int64(2), int64(0), "MIXED"))

	rows, err := NewAttributionRepository(db).ByGroup(context.Background(), "ws", twoCampaigns(), attributionWindow.from, attributionWindow.to)
	if err != nil {
		t.Fatal(err)
	}
	want := []advertising.Attribution{
		{Key: "c-1", Conversations: 10, Leads: 4, WonDeals: 2, Revenue: 150000, RevenueCurrency: "BRL"},
		{Key: "c-2", Conversations: 3, Leads: 1, WonDeals: 2, Revenue: 0, RevenueCurrency: "MIXED"},
	}
	if len(rows) != len(want) || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("got %+v", rows)
	}
	expectationsMet(t, mock)
}

func TestByGroupNeverSumsRevenueAcrossCurrencies(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`CASE WHEN COUNT\(DISTINCT currency\) > 1 THEN 0 ELSE SUM\(value_cents\) END`).
		WillReturnRows(sqlmock.NewRows([]string{"group_key"}))
	if _, err := NewAttributionRepository(db).ByGroup(context.Background(), "ws", twoCampaigns(), attributionWindow.from, attributionWindow.to); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestByGroupCountsAWonDealOncePerGroupEvenWhenReachedThroughManyAds(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT DISTINCT group_key, opportunity_id, value_cents, currency FROM linked WHERE status = 'won'`).
		WillReturnRows(sqlmock.NewRows([]string{"group_key"}))
	if _, err := NewAttributionRepository(db).ByGroup(context.Background(), "ws", twoCampaigns(), attributionWindow.from, attributionWindow.to); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestByGroupWithoutAdsRunsNoQuery(t *testing.T) {
	db, mock := newMockDB(t)
	rows, err := NewAttributionRepository(db).ByGroup(context.Background(), "ws", nil, attributionWindow.from, attributionWindow.to)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("got %v, %v", rows, err)
	}
	expectationsMet(t, mock)
}

func TestAttributionWithoutWorkspaceRunsNoQuery(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAttributionRepository(db)
	if _, err := repo.ByGroup(context.Background(), " ", twoCampaigns(), attributionWindow.from, attributionWindow.to); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	if _, err := repo.Conversations(context.Background(), "", "ad-1", attributionWindow.from, attributionWindow.to); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestConversationsCountsScopedOriginsOfOneAd(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\)::bigint FROM conversation_ad_origins cao\s+JOIN \(`+regexp.QuoteMeta(attendance_repository.EntryWorkspaceUnion())+
		`\) e ON .* e\.workspace_id = \$1\s+WHERE cao\.ad_id IN \(\$2\) AND cao\.arrived_at >= \$3 AND cao\.arrived_at < \$4\s+`+
		`AND EXISTS \(SELECT 1 FROM ad_objects ao WHERE ao\.meta_id = cao\.ad_id AND ao\.workspace_id = \$5\)`).
		WithArgs("ws", "ad-1", attributionWindow.from, attributionWindow.to, "ws").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(7)))
	n, err := NewAttributionRepository(db).Conversations(context.Background(), "ws", "ad-1", attributionWindow.from, attributionWindow.to)
	if err != nil || n != 7 {
		t.Fatalf("got %d, %v", n, err)
	}
	expectationsMet(t, mock)
}
