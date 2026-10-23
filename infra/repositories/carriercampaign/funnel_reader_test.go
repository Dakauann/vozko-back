package carriercampaign_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriercampaign"
	"vozko/domain/rcs"
)

var funnelColumns = []string{"campaign_id", "variant", "targeted", "skipped", "sent", "accepted", "rejected", "delivered", "read", "failed", "expired",
	"taps", "clicks", "replies", "opt_outs", "fell_back_whatsapp", "fell_back_sms", "spend_micros"}

func TestVariantFunnelsMapTheAggregateAndAttributeConversionsToTheLastTouch(t *testing.T) {
	db, mock := newMockDB(t)
	won := testNow
	mock.ExpectQuery(`WITH scope AS .* WHERE r.workspace_id = \$1 AND r.campaign_id IN \(\$2\) AND r.run_id = \$3`).
		WithArgs("ws1", campaignID, runID, "ws1", "rcs", "sms").
		WillReturnRows(sqlmock.NewRows(funnelColumns).
			AddRow(campaignID, 0, 10, 2, 8, 8, 0, 6, 3, 1, 1, 2, 1, 1, 0, 1, 1, 4000).
			AddRow(campaignID, 1, 5, 0, 5, 5, 1, 4, 2, 0, 0, 0, 0, 0, 1, 0, 0, 2000))
	mock.ExpectQuery(`FROM carrier_settings WHERE workspace_id = \$1`).WithArgs("ws1").
		WillReturnRows(sqlmock.NewRows([]string{"attribution_days"}).AddRow(7))
	mock.ExpectQuery(`FROM opportunities o`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "won_at", "value_cents"}).
			AddRow("o1", "l1", won, 15000).
			AddRow("o2", "l2", won, 9900))
	mock.ExpectQuery(`AS delivered_at`).
		WillReturnRows(sqlmock.NewRows(touchColumns).
			AddRow("m-old", campaignID, runID, 1, "l1", "rcs", won.Add(-72*time.Hour)).
			AddRow("m-new", campaignID, runID, 0, "l1", "rcs", won.Add(-time.Hour)).
			AddRow("m-other", "another-campaign", "another-run", 0, "l2", "rcs", won.Add(-time.Hour)).
			AddRow("m-mine", campaignID, runID, 1, "l2", "rcs", won.Add(-2*time.Hour)))
	got, err := NewFunnelReader(db).VariantFunnels(context.Background(), carriercampaign.FunnelQuery{WorkspaceID: "ws1", CampaignID: campaignID, RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	a, b := got[0], got[1]
	if a.Targeted != 10 || a.Skipped != 2 || a.Sent != 8 || a.Delivered != 6 || a.Read != 3 || a.Taps != 2 || a.FellBackSMS != 1 || a.FellBackWhatsApp != 1 || a.Expired != 1 || a.Failed != 1 || a.Replies != 1 || a.Clicks != 1 || a.Accepted != 8 || a.SpendMicros != 4000 {
		t.Fatalf("variant a %+v", a)
	}
	if a.Conversions != 1 || a.ConversionValueMicros != 15000*10000 {
		t.Fatalf("the newest touch of l1 is variant a: %+v", a)
	}
	if b.Conversions != 0 || b.Rejected != 1 || b.OptOuts != 1 {
		t.Fatalf("l2 converted on another campaign, so variant b gets nothing: %+v", b)
	}
	expectationsMet(t, mock)
}

var touchColumns = []string{"message_id", "campaign_id", "run_id", "variant", "lead_id", "channel", "delivered_at"}

func TestFunnelsOfFoldsEveryCampaignOfAListPageInOneQuery(t *testing.T) {
	db, mock := newMockDB(t)
	other := "1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	mock.ExpectQuery(`WITH scope AS .* WHERE r.workspace_id = \$1 AND r.campaign_id IN \(\$2,\$3\)`).
		WithArgs("ws1", campaignID, other, "ws1", "rcs", "sms").
		WillReturnRows(sqlmock.NewRows(funnelColumns).
			AddRow(campaignID, 0, 3, 0, 3, 3, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 10).
			AddRow(campaignID, 1, 2, 0, 2, 2, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 5).
			AddRow(other, 0, 7, 1, 6, 6, 0, 6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 30))
	mock.ExpectQuery(`FROM carrier_settings`).WillReturnRows(sqlmock.NewRows([]string{"attribution_days"}))
	mock.ExpectQuery(`FROM opportunities o`).WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "won_at", "value_cents"}))
	got, err := NewFunnelReader(db).FunnelsOf(context.Background(), "ws1", []string{campaignID, other})
	if err != nil || got[campaignID].Targeted != 5 || got[campaignID].SpendMicros != 15 || got[other].Delivered != 6 || len(got) != 2 {
		t.Fatalf("got %+v %v", got, err)
	}
	empty, err := NewFunnelReader(db).FunnelsOf(context.Background(), "ws1", nil)
	if err != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("empty page %+v %v", empty, err)
	}
	expectationsMet(t, mock)
}

func TestFunnelSumsTheVariants(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`WITH scope AS .* WHERE r.workspace_id = \$1 AND r.campaign_id IN \(\$2\) AND c.channel = \$3`).
		WillReturnRows(sqlmock.NewRows(funnelColumns).
			AddRow(campaignID, 0, 10, 0, 10, 10, 0, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 100).
			AddRow(campaignID, 1, 4, 0, 4, 4, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 50))
	mock.ExpectQuery(`FROM carrier_settings`).WillReturnRows(sqlmock.NewRows([]string{"attribution_days"}))
	mock.ExpectQuery(`FROM opportunities o`).WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "won_at", "value_cents"}))
	got, err := NewFunnelReader(db).Funnel(context.Background(), carriercampaign.FunnelQuery{WorkspaceID: "ws1", CampaignID: campaignID, Channel: "rcs"})
	if err != nil || got.Targeted != 14 || got.Delivered != 12 || got.SpendMicros != 150 {
		t.Fatalf("got %+v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestFunnelQueriesFailClosed(t *testing.T) {
	cases := []struct {
		name string
		q    carriercampaign.FunnelQuery
		want error
	}{
		{"no workspace", carriercampaign.FunnelQuery{CampaignID: campaignID}, errFunnelScope},
		{"malformed campaign", carriercampaign.FunnelQuery{WorkspaceID: "ws1", CampaignID: "x"}, carriercampaign.ErrCampaignNotFound},
		{"malformed run", carriercampaign.FunnelQuery{WorkspaceID: "ws1", RunID: "x"}, carriercampaign.ErrRunNotFound},
		{"unknown channel", carriercampaign.FunnelQuery{WorkspaceID: "ws1", Channel: "fax"}, errUnknownStoredValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			reader := NewFunnelReader(db)
			if _, err := reader.Funnel(context.Background(), tc.q); !errors.Is(err, tc.want) {
				t.Fatalf("funnel: %v", err)
			}
			if _, _, err := reader.ButtonCounts(context.Background(), tc.q, 0); !errors.Is(err, tc.want) {
				t.Fatalf("buttons: %v", err)
			}
			if _, err := reader.Carriers(context.Background(), tc.q); !errors.Is(err, tc.want) {
				t.Fatalf("carriers: %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestButtonCountsSplitTapsAndClicks(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`WITH scope AS .* t.variant = \$\d+ AND e.kind IN \(\$\d+,\$\d+\) AND e.button_index IS NOT NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"kind", "button_card", "button_index", "total"}).
			AddRow("tap", -1, 0, 5).AddRow("click", -1, 0, 3).AddRow("tap", 1, 2, 1))
	taps, clicks, err := NewFunnelReader(db).ButtonCounts(context.Background(), carriercampaign.FunnelQuery{WorkspaceID: "ws1", CampaignID: campaignID}, 1)
	if err != nil || taps[rcs.ButtonKey{Card: -1, Index: 0}] != 5 || clicks[rcs.ButtonKey{Card: -1, Index: 0}] != 3 || taps[rcs.ButtonKey{Card: 1, Index: 2}] != 1 {
		t.Fatalf("got %v %v %v", taps, clicks, err)
	}
	mock.ExpectQuery(`WITH scope AS`).WillReturnRows(sqlmock.NewRows([]string{"kind", "button_card", "button_index", "total"}).AddRow("like", 0, 0, 1))
	if _, _, err := NewFunnelReader(db).ButtonCounts(context.Background(), carriercampaign.FunnelQuery{WorkspaceID: "ws1"}, 0); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("unknown kind: %v", err)
	}
	expectationsMet(t, mock)
}

func TestCarriersGroupByOperator(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`WITH scope AS .* GROUP BY 1 ORDER BY sent DESC, operator ASC`).
		WillReturnRows(sqlmock.NewRows([]string{"operator", "sent", "delivered", "fell_back"}).AddRow("Vivo", 10, 9, 1).AddRow("", 2, 0, 2))
	got, err := NewFunnelReader(db).Carriers(context.Background(), carriercampaign.FunnelQuery{WorkspaceID: "ws1", CampaignID: campaignID})
	if err != nil || len(got) != 2 || got[0].Operator != "Vivo" || got[0].Delivered != 9 || got[1].FellBack != 2 {
		t.Fatalf("got %+v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestTouchesOfALead(t *testing.T) {
	db, mock := newMockDB(t)
	since := testNow.Add(-7 * 24 * time.Hour)
	mock.ExpectQuery(`AS delivered_at .* m.workspace_id = \$1 .* m.run_kind IN \(\$2,\$3\) .* IN \(\$4\) .* wc.workspace_id = \$6 .* e.lead_id IN \(\$7\)`).
		WithArgs("ws1", "campaign", "automation", "l1", since, "ws1", "l1", since).
		WillReturnRows(sqlmock.NewRows(touchColumns).
			AddRow("m1", campaignID, runID, 1, "l1", "rcs", testNow).
			AddRow("e1", "wa-campaign", "", 0, "l1", "whatsapp", testNow))
	got, err := NewFunnelReader(db).Touches(context.Background(), "ws1", "l1", since)
	if err != nil || len(got) != 1 || got[0].MessageID != "m1" || got[0].Variant != 1 || !got[0].DeliveredAt.Equal(testNow) {
		t.Fatalf("got %+v %v", got, err)
	}
	expectationsMet(t, mock)
}
