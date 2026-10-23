package carriercampaign_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/carriercampaign"
)

func TestWhatsAppCampaignSpendCountsOnlyDealsWhoseLastTouchIsWhatsApp(t *testing.T) {
	db, mock := newMockDB(t)
	from, to := testNow.Add(-7*24*time.Hour), testNow
	mock.ExpectQuery(`FROM balance_transactions bt WHERE bt.workspace_id = \$1 AND bt.service_type = \$2 AND bt.created_at >= \$3 AND bt.created_at < \$4 .* whatsapp_campaign_entries`).
		WithArgs("ws1", "whatsapp_campaign", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"spend"}).AddRow(int64(9000)))
	mock.ExpectQuery(`FROM opportunities o`).WithArgs("ws1", "won", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "won_at", "value_cents"}).
			AddRow("o1", "l1", testNow.Add(-time.Hour), 100).
			AddRow("o2", "l2", testNow.Add(-time.Hour), 100))
	mock.ExpectQuery(`AS delivered_at`).
		WillReturnRows(sqlmock.NewRows(touchColumns).
			AddRow("m1", campaignID, runID, 0, "l1", "rcs", testNow.Add(-3*time.Hour)).
			AddRow("e1", "wa1", "", 0, "l1", "whatsapp", testNow.Add(-2*time.Hour)).
			AddRow("e2", "wa1", "", 0, "l2", "whatsapp", testNow.Add(-3*time.Hour)).
			AddRow("m2", campaignID, runID, 0, "l2", "sms", testNow.Add(-2*time.Hour)))
	spend, conversions, err := NewWhatsAppSpendReader(db).WhatsAppCampaignSpend(context.Background(), "ws1", from, to, 7)
	if err != nil || spend != 9000 || conversions != 1 {
		t.Fatalf("got %d %d %v", spend, conversions, err)
	}
	expectationsMet(t, mock)
}

func TestWhatsAppCampaignSpendRefusesAnInvalidWindow(t *testing.T) {
	cases := []struct {
		name string
		from time.Time
		days int
	}{
		{"no attribution days", testNow.Add(-time.Hour), 0},
		{"too many attribution days", testNow.Add(-time.Hour), 91},
		{"inverted range", testNow.Add(time.Hour), 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			if _, _, err := NewWhatsAppSpendReader(db).WhatsAppCampaignSpend(context.Background(), "ws1", tc.from, testNow, tc.days); !errors.Is(err, errOverviewQuery) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestIntegrationOneLastTouchAcrossCarrierAndWhatsApp(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-24 * time.Hour)
	w := funnelWorld{t: t, db: db, ws: uuid.NewString(), now: now}
	c := seedCampaign(t, NewCampaignRepository(db), w.ws, nil)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	rcsLead, waLead := uuid.NewString(), uuid.NewString()
	m1 := w.message("rcs", "rcs", "delivered", "", "", "", at(0), at(time.Minute), nil)
	m2 := w.message("rcs", "rcs", "delivered", "", "", "", at(0), at(time.Minute), nil)
	run := uuid.NewString()
	if err := NewRecipientRepository(db).CreateMany(ctx, []carriercampaign.Recipient{
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: rcsLead, Status: carriercampaign.RecipientSent, MessageID: m1},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: waLead, Status: carriercampaign.RecipientSent, MessageID: m2},
	}); err != nil {
		t.Fatal(err)
	}
	waCampaign := uuid.NewString()
	entryLate, entryEarly, entryOtherWorkspace := uuid.NewString(), uuid.NewString(), uuid.NewString()
	otherCampaign := uuid.NewString()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO whatsapp_campaigns (id, workspace_id) VALUES (?, ?), (?, ?)`, []any{waCampaign, w.ws, otherCampaign, uuid.NewString()}},
		{`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, delivered_at) VALUES (?, ?, ?, ?), (?, ?, ?, ?), (?, ?, ?, ?)`, []any{
			entryLate, waCampaign, waLead, *at(2 * time.Hour),
			entryEarly, waCampaign, rcsLead, *at(-time.Hour),
			entryOtherWorkspace, otherCampaign, rcsLead, *at(3 * time.Hour),
		}},
	} {
		if err := db.Exec(stmt.sql, stmt.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	w.won(rcsLead, now.Add(5*time.Hour), 100, "won")
	w.won(waLead, now.Add(5*time.Hour), 300, "won")
	ledger := func(kind string, amount int64, refund bool, reference string, when time.Time) {
		if err := db.Exec(`INSERT INTO balance_transactions (id, workspace_id, type, amount, service_type, reference_id, is_refund, created_at) VALUES (?, ?, ?, ?, 'whatsapp_campaign', ?, ?, ?)`,
			uuid.NewString(), w.ws, kind, amount, reference, refund, when).Error; err != nil {
			t.Fatal(err)
		}
	}
	ledger("debit", 400, false, entryLate, now)
	ledger("debit", 400, false, entryEarly+":1", now)
	ledger("credit", 400, true, "refund:"+entryEarly+":1", now)
	ledger("debit", 250, false, entryEarly, now)
	ledger("debit", 999, false, "waba:attempt-1", now)
	ledger("debit", 777, false, entryLate, now.Add(-30*24*time.Hour))

	spend, conversions, err := NewWhatsAppSpendReader(db).WhatsAppCampaignSpend(ctx, w.ws, now.Add(-time.Hour), now.Add(24*time.Hour), 7)
	if err != nil || spend != 650 || conversions != 1 {
		t.Fatalf("whatsapp spend %d conversions %d %v", spend, conversions, err)
	}
	funnel, err := NewFunnelReader(db).Funnel(ctx, carriercampaign.FunnelQuery{WorkspaceID: w.ws, CampaignID: c.ID})
	if err != nil || funnel.Conversions != 1 || funnel.ConversionValueMicros != 100*centsToMicros {
		t.Fatalf("the carrier campaign keeps only the deal it touched last: %+v %v", funnel, err)
	}
	overview, err := NewOverviewReader(db).Overview(ctx, w.ws, now.Add(-time.Hour), now.Add(24*time.Hour), "UTC", "")
	if err != nil || overview.Totals.Conversions != 1 {
		t.Fatalf("overview conversions %+v %v", overview.Totals, err)
	}
}
