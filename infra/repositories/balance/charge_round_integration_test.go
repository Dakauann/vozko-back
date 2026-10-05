package balance_repository

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/balance"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace/workspace_pricing"
	"vozko/infra/database/schema"
	wce_repository "vozko/infra/repositories/whatsapp_campaign_entry"
	balance_usecase "vozko/usecases/balance"
)

type roundFixture struct {
	db         *gorm.DB
	consume    balance.ConsumeWhatsappTemplateUseCase
	entries    wce.Repository
	ledger     balance.Repository
	price      int64
	workspace  string
	department string
	campaign   string
	entry      string
}

func newRoundFixture(t *testing.T) *roundFixture {
	t.Helper()
	db := sendCapIntegrationDB(t)
	if err := db.AutoMigrate(&schema.Lead{}, &schema.WhatsAppCampaign{}, &schema.WhatsAppCampaignEntry{}, &schema.WhatsAppTemplateSend{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ledger := NewRepository(db)
	consume := balance_usecase.NewConsumeWhatsappTemplateUseCase(ledger, workspace_pricing.NewPricer(flatPricing{}), activeSubscription{}, NewMonthlySendCapRepository(db))
	price, err := consume.GetTemplateCostMicros(uuid.New().String(), "MARKETING")
	if err != nil {
		t.Fatalf("price: %v", err)
	}

	f := &roundFixture{db: db, consume: consume, entries: wce_repository.NewRepository(db), ledger: ledger, price: price,
		department: uuid.New().String(), campaign: uuid.New().String(), entry: uuid.New().String()}
	f.workspace = seedWorkspace(t, db, "Round", price*100)

	leadID := uuid.New().String()
	if err := db.Create(&schema.Lead{ID: leadID, WorkspaceID: f.workspace, Number: "5511999990001"}).Error; err != nil {
		t.Fatalf("seed lead: %v", err)
	}
	if err := db.Create(&schema.WhatsAppCampaign{ID: f.campaign, WorkspaceID: f.workspace, DepartmentID: &f.department, Name: "round"}).Error; err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	if err := f.entries.Create(&wce.WhatsAppCampaignEntry{ID: f.entry, CampaignID: f.campaign, LeadID: leadID, Status: wce.SendStatusPending}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	return f
}

func (f *roundFixture) current(t *testing.T) *wce.WhatsAppCampaignEntry {
	t.Helper()
	e, err := f.entries.FindByID(f.entry)
	if err != nil {
		t.Fatalf("find entry: %v", err)
	}
	return e
}

func (f *roundFixture) send(t *testing.T) string {
	t.Helper()
	ref := f.current(t).ChargeReference()
	if _, err := f.consume.Execute(f.workspace, ref, "MARKETING"); err != nil {
		t.Fatalf("charge %s: %v", ref, err)
	}
	return ref
}

func (f *roundFixture) fail(t *testing.T, ref string) {
	t.Helper()
	if err := f.consume.Refund(f.workspace, ref, "MARKETING"); err != nil {
		t.Fatalf("refund %s: %v", ref, err)
	}
}

func (f *roundFixture) reset(t *testing.T) {
	t.Helper()
	if _, err := f.entries.ResetAllStatuses(f.campaign); err != nil {
		t.Fatalf("reset: %v", err)
	}
}

func (f *roundFixture) ledgerCounts(t *testing.T) (debits, refunds int64) {
	t.Helper()
	f.db.Model(&schema.BalanceTransaction{}).Where("workspace_id = ? AND type = 'debit' AND is_refund = false", f.workspace).Count(&debits)
	f.db.Model(&schema.BalanceTransaction{}).Where("workspace_id = ? AND is_refund = true", f.workspace).Count(&refunds)
	return debits, refunds
}

func (f *roundFixture) balance(t *testing.T) int64 {
	t.Helper()
	var b schema.Balance
	if err := f.db.Where("workspace_id = ?", f.workspace).First(&b).Error; err != nil {
		t.Fatalf("balance: %v", err)
	}
	return b.Amount
}

func TestChargeRound_EverySendIsChargedOnceAndEveryFailureRefundedOnce(t *testing.T) {
	f := newRoundFixture(t)
	start := f.balance(t)

	first := f.send(t)
	if first != f.entry {
		t.Fatalf("round 0 must keep the legacy reference, got %q", first)
	}
	f.send(t)
	if d, _ := f.ledgerCounts(t); d != 1 {
		t.Fatalf("a queue redelivery of the same send charged again: %d debits", d)
	}
	f.fail(t, first)
	f.fail(t, first)
	if _, r := f.ledgerCounts(t); r != 1 {
		t.Fatalf("a redelivered failure webhook refunded twice: %d refunds", r)
	}

	f.reset(t)
	resent := f.current(t)
	if resent.SendRound != 1 || resent.MessageID != "" || resent.Status != wce.SendStatusPending {
		t.Fatalf("reset must open round 1 with a clean slate, got round %d message %q status %s", resent.SendRound, resent.MessageID, resent.Status)
	}
	if second := f.send(t); second != f.entry+":1" {
		t.Fatalf("round 1 reference = %q", second)
	}

	f.reset(t)
	third := f.send(t)
	if third != f.entry+":2" {
		t.Fatalf("round 2 reference = %q", third)
	}
	f.fail(t, third)

	debits, refunds := f.ledgerCounts(t)
	if debits != 3 || refunds != 2 {
		t.Fatalf("ledger = %d debits, %d refunds; want 3 sends charged and the 2 failures refunded", debits, refunds)
	}
	if spent := start - f.balance(t); spent != f.price {
		t.Fatalf("the workspace paid %d, want exactly one delivered send (%d)", spent, f.price)
	}
}

func TestChargeRound_ReportCountsEveryRoundUnderItsCampaign(t *testing.T) {
	f := newRoundFixture(t)
	first := f.send(t)
	f.fail(t, first)
	f.reset(t)
	f.send(t)
	f.reset(t)
	third := f.send(t)
	f.fail(t, third)

	repo := NewRepository(f.db).(*BalanceRepositoryImpl)
	for name, filter := range map[string]balance.WhatsAppChargeFilter{
		"workspace":  {WorkspaceID: f.workspace},
		"department": {WorkspaceID: f.workspace, DepartmentIDs: []string{f.department}},
	} {
		stats, err := repo.AggregateWhatsAppTemplateCharges(filter)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if stats.NetDispatches != 1 {
			t.Fatalf("%s report: net dispatches = %d, want the single delivered send", name, stats.NetDispatches)
		}
	}
}

func TestChargeRound_AStaticReferenceWouldSilenceTheResend(t *testing.T) {
	f := newRoundFixture(t)
	if _, err := f.consume.Execute(f.workspace, f.entry, "MARKETING"); err != nil {
		t.Fatal(err)
	}
	f.reset(t)
	if _, err := f.consume.Execute(f.workspace, f.entry, "MARKETING"); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.ledgerCounts(t); d != 1 {
		t.Fatalf("debits = %d; this test documents why the entry id alone cannot be the reference", d)
	}
}

func TestChargeRound_OnlyAResetMovesTheRound(t *testing.T) {
	f := newRoundFixture(t)
	for i := 0; i < 3; i++ {
		f.reset(t)
	}
	if err := f.entries.UpdateStatus(f.entry, wce.SendStatusSent, "wamid.1", 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.entries.UpdateMetadata(f.entry, map[string]interface{}{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if got := f.current(t).SendRound; got != 3 {
		t.Fatalf("round = %d, status and metadata writes must never move it", got)
	}
}

func TestChargeRound_AResetHidesTheOldMessageFromItsLateWebhook(t *testing.T) {
	f := newRoundFixture(t)
	if err := f.entries.UpdateStatus(f.entry, wce.SendStatusSent, "wamid.old", 0, ""); err != nil {
		t.Fatal(err)
	}
	f.reset(t)
	if _, err := f.entries.FindByMessageID("wamid.old"); err == nil {
		t.Fatal("a late webhook for the previous round must not find the entry, or it would refund the new round")
	}
}

func TestChargeRound_ConcurrentDeliveriesOfTheSameSend(t *testing.T) {
	f := newRoundFixture(t)
	for attempt := 0; attempt < 20; attempt++ {
		ref := uuid.New().String()
		charged := race(16, func() bool {
			tx, err := f.consume.Execute(f.workspace, ref, "MARKETING")
			return err == nil && tx != nil
		})
		var debits int64
		f.db.Model(&schema.BalanceTransaction{}).Where("reference_id = ?", ref).Count(&debits)
		if debits != 1 || charged != 1 {
			t.Fatalf("16 simultaneous deliveries of one send: %d debits, %d told they charged; want exactly one of each", debits, charged)
		}
	}
}

func TestChargeRound_ConcurrentFailureWebhooksRefundOnce(t *testing.T) {
	f := newRoundFixture(t)
	start := f.balance(t)
	for attempt := 0; attempt < 20; attempt++ {
		ref := uuid.New().String()
		if _, err := f.consume.Execute(f.workspace, ref, "MARKETING"); err != nil {
			t.Fatal(err)
		}
		race(16, func() bool { return f.consume.Refund(f.workspace, ref, "MARKETING") == nil })
		var refunds int64
		f.db.Model(&schema.BalanceTransaction{}).Where("reference_id = ?", "refund:"+ref).Count(&refunds)
		if refunds != 1 {
			t.Fatalf("16 simultaneous failure webhooks refunded %d times", refunds)
		}
	}
	if f.balance(t) != start {
		t.Fatalf("balance moved from %d to %d after charging and refunding every send", start, f.balance(t))
	}
}

func race(n int, do func() bool) int {
	gate := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			if do() {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	close(gate)
	wg.Wait()
	return won
}

func TestChargeReport_FiltersEveryReferenceShapeByItsCampaign(t *testing.T) {
	f := newRoundFixture(t)
	otherDepartment := uuid.New().String()
	otherCampaign := uuid.New().String()
	if err := f.db.Create(&schema.WhatsAppCampaign{ID: otherCampaign, WorkspaceID: f.workspace, DepartmentID: &otherDepartment, Name: "other"}).Error; err != nil {
		t.Fatal(err)
	}
	attempt := uuid.New().String()
	if err := f.db.Create(&schema.WhatsAppTemplateSend{ID: attempt, WorkspaceID: f.workspace, UserID: uuid.New().String(), IdempotencyKey: attempt,
		BusinessPhoneID: uuid.New().String(), TemplateID: uuid.New().String(), CampaignID: &f.campaign}).Error; err != nil {
		t.Fatal(err)
	}

	f.send(t)
	f.reset(t)
	f.fail(t, f.send(t))
	for _, ref := range []string{f.campaign, "waba:" + attempt, otherCampaign} {
		if _, err := f.consume.Execute(f.workspace, ref, "MARKETING"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.consume.Refund(f.workspace, f.campaign, "MARKETING"); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(f.db).(*BalanceRepositoryImpl)
	stats, err := repo.AggregateWhatsAppTemplateCharges(balance.WhatsAppChargeFilter{WorkspaceID: f.workspace, DepartmentIDs: []string{f.department}})
	if err != nil {
		t.Fatal(err)
	}
	if stats.NetDispatches != 2 {
		t.Fatalf("net dispatches = %d; want round 0, the attempt and the legacy campaign charge (refunded) of this department, nothing from the other", stats.NetDispatches)
	}
	everything, err := repo.AggregateWhatsAppTemplateCharges(balance.WhatsAppChargeFilter{WorkspaceID: f.workspace})
	if err != nil {
		t.Fatal(err)
	}
	if everything.NetDispatches != 3 {
		t.Fatalf("unfiltered net dispatches = %d, want 3", everything.NetDispatches)
	}
}
