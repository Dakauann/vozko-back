package carriercampaign_repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/infra/database/schema"
)

type funnelWorld struct {
	t   *testing.T
	db  *gorm.DB
	ws  string
	now time.Time
}

func (w funnelWorld) message(channel, step, status, parent, operator, charge string, accepted, delivered, read *time.Time) string {
	w.t.Helper()
	id := uuid.NewString()
	row := schema.CarrierMessage{
		ID: id, WorkspaceID: w.ws, Channel: channel, Reference: strings.ReplaceAll(id, "-", ""), SenderID: uuid.NewString(),
		Phone: "5511999990030", RunKind: "campaign", Marketing: true, Content: datatypes.JSON(`{}`), Fallback: datatypes.JSON(`{}`),
		Step: step, Status: status, ChargeReference: charge, AcceptedAt: accepted, DeliveredAt: delivered, ReadAt: read,
		CreatedAt: w.now, UpdatedAt: w.now,
	}
	if step == "rcs" {
		row.Class = "basic"
	}
	if parent != "" {
		row.ParentMessageID = &parent
	}
	if operator != "" {
		row.Operator = &operator
	}
	if err := w.db.Create(&row).Error; err != nil {
		w.t.Fatal(err)
	}
	if parent != "" {
		if err := w.db.Exec("UPDATE carrier_messages SET fallback_message_id = ? WHERE id = ?", id, parent).Error; err != nil {
			w.t.Fatal(err)
		}
	}
	return id
}

func (w funnelWorld) event(messageID, kind, dedup string, card, index *int) {
	w.t.Helper()
	row := schema.CarrierMessageEvent{ID: uuid.NewString(), WorkspaceID: w.ws, MessageID: &messageID, Phone: "5511999990030", Kind: kind,
		ButtonCard: card, ButtonIndex: index, DedupKey: dedup, OccurredAt: w.now, CreatedAt: w.now, UpdatedAt: w.now}
	if err := w.db.Create(&row).Error; err != nil {
		w.t.Fatal(err)
	}
}

func (w funnelWorld) ledger(kind string, amount int64, refund bool, reference string) {
	w.t.Helper()
	if err := w.db.Exec(`INSERT INTO balance_transactions (id, workspace_id, type, amount, service_type, reference_id, is_refund, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), w.ws, kind, amount, "rcs", reference, refund, w.now).Error; err != nil {
		w.t.Fatal(err)
	}
}

func (w funnelWorld) won(leadID string, at time.Time, cents int64, status string) {
	w.t.Helper()
	if err := w.db.Exec(`INSERT INTO opportunities (id, workspace_id, lead_id, status, value_cents, close_date) VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), w.ws, leadID, status, cents, at).Error; err != nil {
		w.t.Fatal(err)
	}
}

func TestIntegrationFunnelCountsEveryStageFromTheTables(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * 24 * time.Hour)
	w := funnelWorld{t: t, db: db, ws: uuid.NewString(), now: now}
	campaigns := NewCampaignRepository(db)
	c := seedCampaign(t, campaigns, w.ws, nil)
	other := seedCampaign(t, campaigns, w.ws, nil)
	run, otherRun := uuid.NewString(), uuid.NewString()
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }

	m1 := w.message("rcs", "rcs", "read", "", "Vivo", "carrier:rcs_basic:m1", at(time.Minute), at(2*time.Minute), at(3*time.Minute))
	m2 := w.message("rcs", "rcs", "failed", "", "Claro", "carrier:rcs_basic:m2", at(time.Minute), nil, nil)
	wa := w.message("rcs", "whatsapp", "failed", m2, "", "", nil, nil, nil)
	w.message("sms", "sms", "delivered", wa, "", "carrier:sms_segment:s", at(time.Hour), at(time.Hour), nil)
	m3 := w.message("rcs", "rcs", "rejected", "", "", "", nil, nil, nil)
	m4 := w.message("rcs", "rcs", "delivered", "", "", "", at(5*time.Hour), at(5*time.Hour), nil)

	minus1, zero := -1, 0
	w.event(m1, "tap", "tap-1", &minus1, &zero)
	w.event(m1, "tap", "tap-2", &minus1, &zero)
	w.event(m1, "click", "click-1", &minus1, &zero)
	w.event(m3, "opt_out", "opt-1", nil, nil)
	w.event(m3, "inbound", "in-1", nil, nil)

	w.ledger("debit", 100, false, "carrier:rcs_basic:m1")
	w.ledger("debit", 100, false, "carrier:rcs_basic:m2")
	w.ledger("credit", 100, true, "refund:carrier:rcs_basic:m2")
	w.ledger("debit", 50, false, "carrier:sms_segment:s")
	w.ledger("debit", 999, false, "carrier:rcs_basic:unrelated")

	l1, l2, l3, l4, l5 := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	recipients := []carriercampaign.Recipient{
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: l1, Variant: 0, Status: carriercampaign.RecipientSent, MessageID: m1},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: l2, Variant: 0, Status: carriercampaign.RecipientSent, MessageID: m2},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: l3, Variant: 1, Status: carriercampaign.RecipientSent, MessageID: m3},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: l4, Variant: 1, Status: carriercampaign.RecipientSkipped, SkipReason: carriercampaign.SkipSuppressed},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: l5, Variant: 1, Status: carriercampaign.RecipientPending},
		{WorkspaceID: w.ws, CampaignID: other.ID, RunID: otherRun, LeadID: l2, Variant: 0, Status: carriercampaign.RecipientSent, MessageID: m4},
	}
	if err := NewRecipientRepository(db).CreateMany(ctx, recipients); err != nil {
		t.Fatal(err)
	}
	w.won(l1, now.Add(24*time.Hour), 200, "won")
	w.won(l1, now.Add(24*time.Hour), 900, "lost")
	w.won(l2, now.Add(48*time.Hour), 300, "won")
	w.won(l1, now.Add(30*24*time.Hour), 700, "won")

	reader := NewFunnelReader(db)
	q := carriercampaign.FunnelQuery{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run}
	variants, err := reader.VariantFunnels(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	want0 := carriercampaign.Funnel{Targeted: 2, Sent: 2, Accepted: 2, Delivered: 1, Read: 1, Failed: 1, Taps: 1, Clicks: 1,
		FellBackWhatsApp: 1, FellBackSMS: 1, Conversions: 1, ConversionValueMicros: 200 * centsToMicros, SpendMicros: 150}
	want1 := carriercampaign.Funnel{Targeted: 3, Skipped: 1, Sent: 1, Rejected: 1, Replies: 1, OptOuts: 1}
	if variants[0] != want0 {
		t.Fatalf("variant 0\n got %+v\nwant %+v", variants[0], want0)
	}
	if variants[1] != want1 {
		t.Fatalf("variant 1\n got %+v\nwant %+v", variants[1], want1)
	}
	total, err := reader.Funnel(ctx, carriercampaign.FunnelQuery{WorkspaceID: w.ws, CampaignID: c.ID})
	if err != nil || total != want0.Add(want1) {
		t.Fatalf("campaign total %+v %v", total, err)
	}
	if none, err := reader.Funnel(ctx, carriercampaign.FunnelQuery{WorkspaceID: w.ws, CampaignID: c.ID, Channel: carriermessaging.ChannelSMS}); err != nil || none != (carriercampaign.Funnel{}) {
		t.Fatalf("an rcs campaign has no sms funnel: %+v %v", none, err)
	}
	otherFunnel, err := reader.Funnel(ctx, carriercampaign.FunnelQuery{WorkspaceID: w.ws, CampaignID: other.ID})
	if err != nil || otherFunnel.Conversions != 1 || otherFunnel.ConversionValueMicros != 300*centsToMicros {
		t.Fatalf("the last touch of l2 is the other campaign: %+v %v", otherFunnel, err)
	}

	taps, clicks, err := reader.ButtonCounts(ctx, q, 0)
	key := rcs.ButtonKey{Card: -1, Index: 0}
	if err != nil || taps[key] != 1 || clicks[key] != 1 || len(taps) != 1 {
		t.Fatalf("buttons %v %v %v", taps, clicks, err)
	}
	carriers, err := reader.Carriers(ctx, q)
	if err != nil || len(carriers) != 3 {
		t.Fatalf("carriers %+v %v", carriers, err)
	}
	byOperator := map[string]carriercampaign.CarrierStat{}
	for _, c := range carriers {
		byOperator[c.Operator] = c
	}
	if byOperator["Vivo"].Delivered != 1 || byOperator["Claro"].FellBack != 1 || byOperator[""].Sent != 1 {
		t.Fatalf("carriers %+v", byOperator)
	}
	page, err := reader.FunnelsOf(ctx, w.ws, []string{c.ID, other.ID})
	if err != nil || page[c.ID] != total || page[other.ID] != otherFunnel || len(page) != 2 {
		t.Fatalf("list page funnels %+v %v", page, err)
	}
	touches, err := reader.Touches(ctx, w.ws, l2, now.Add(-time.Hour))
	if err != nil || len(touches) != 2 || touches[0].MessageID != m2 || !touches[0].DeliveredAt.Equal(now.Add(time.Hour)) || touches[1].CampaignID != other.ID {
		t.Fatalf("touches %+v %v", touches, err)
	}
}
