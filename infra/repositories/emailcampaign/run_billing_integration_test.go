package emailcampaign

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailaddress"
	domain "vozko/domain/emailcampaign"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	"vozko/domain/workspace/workspace_pricing"
	"vozko/infra/database/schema"
	balance_repository "vozko/infra/repositories/balance"
	"vozko/infra/repositories/repotest"
	workspace_pricing_repository "vozko/infra/repositories/workspace_pricing"
	balance_usecase "vozko/usecases/balance"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

const scenarioWallet = int64(20_000_000)

type currentSubscription struct{}

func (currentSubscription) Execute(string) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, nil
}

type processLocks struct {
	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	broken bool
}

func (l *processLocks) Acquire(_ context.Context, key string) (func() (bool, error), error) {
	if l.broken {
		return func() (bool, error) { return false, nil }, nil
	}
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	lock, ok := l.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return func() (bool, error) { lock.Unlock(); return true, nil }, nil
}

type billingWorld struct {
	t          *testing.T
	db         *gorm.DB
	campaigns  *CampaignRepository
	recipients *RecipientRepository
	store      *BillingRepository
	locks      *processLocks
	billing    *emailcampaign_usecase.RunBilling
	ws         string
	start      int64
	unit       int64
	campaign   domain.Campaign
	sent       atomic.Int64
	violations atomic.Int64
}

func newBillingWorld(t *testing.T, recipients int, wallet int64) *billingWorld {
	t.Helper()
	db := migrated(t)
	if err := db.AutoMigrate(&schema.Balance{}, &schema.BalanceTransaction{}, &schema.PricingItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if pool, err := db.DB(); err == nil {
		pool.SetMaxOpenConns(20)
	}
	if err := workspace_pricing_repository.NewRepository(db).SeedDefaults(workspace_pricing.DefaultPricingCatalog); err != nil {
		t.Fatalf("seed prices: %v", err)
	}
	w := &billingWorld{
		t: t, db: db, campaigns: NewCampaignRepository(db), recipients: NewRecipientRepository(db), store: NewBillingRepository(db),
		locks: &processLocks{}, ws: uuid.NewString(), start: wallet,
	}
	if err := db.Create(&schema.Balance{ID: uuid.NewString(), WorkspaceID: w.ws, Amount: wallet, Currency: "USD"}).Error; err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	w.billing = w.billingAtCurrentPrices()
	w.launch(recipients)
	return w
}

func (w *billingWorld) billingAtCurrentPrices() *emailcampaign_usecase.RunBilling {
	w.t.Helper()
	pricer := workspace_pricing.NewPricer(workspace_pricing_repository.NewRepository(w.db))
	consume := balance_usecase.NewConsumeEmailUseCase(balance_repository.NewRepository(w.db), pricer, currentSubscription{})
	quote, err := consume.QuoteRun(context.Background(), w.ws, 1)
	if err != nil {
		w.t.Fatalf("quote: %v", err)
	}
	w.unit = quote.UnitMicros
	return emailcampaign_usecase.NewRunBilling(emailcampaign_usecase.RunBillingDeps{
		Campaigns: w.campaigns, Store: w.store, Recipients: w.recipients, Billing: consume, Locks: w.locks,
		Now: func() time.Time { return time.Now().UTC() }, Logf: w.t.Logf,
	})
}

func (w *billingWorld) setSendPrice(price int64) {
	w.t.Helper()
	err := w.db.Model(&schema.PricingItem{}).
		Where("workspace_id IS NULL AND category = ? AND service = ? AND metric = ?", string(workspace_pricing.CategoryEmail), workspace_pricing.EmailServiceSend, workspace_pricing.EmailMetricPerMessage).
		Update("price_micros", price).Error
	if err != nil {
		w.t.Fatalf("price: %v", err)
	}
	w.billing = w.billingAtCurrentPrices()
	if w.unit != price {
		w.t.Fatalf("unit = %d after the price change, want %d", w.unit, price)
	}
}

func (w *billingWorld) launch(n int) {
	w.t.Helper()
	ctx := context.Background()
	c := &domain.Campaign{WorkspaceID: w.ws, Name: "Lotes", Status: domain.StatusDraft}
	if err := w.campaigns.Create(ctx, c); err != nil {
		w.t.Fatal(err)
	}
	if ok, err := w.campaigns.MarkLaunched(ctx, c.ID, domain.StatusDraft, domain.Launch{StartedAt: time.Now().UTC()}); err != nil || !ok {
		w.t.Fatalf("launch = %v %v", ok, err)
	}
	w.campaign = *c
	people := make([]domain.Recipient, n)
	for i := range people {
		people[i] = domain.Recipient{ID: uuid.NewString(), Address: emailaddress.Address(fmt.Sprintf("p%05d@x.com", i)), Variant: domain.VariantA, Status: domain.RecipientPending}
	}
	if err := w.recipients.Replace(ctx, w.ws, c.ID, people); err != nil {
		w.t.Fatal(err)
	}
	w.campaign = w.current()
}

func (w *billingWorld) current() domain.Campaign {
	w.t.Helper()
	c, err := w.campaigns.GetByID(context.Background(), w.campaign.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	return *c
}

func (w *billingWorld) pendingIDs() []string {
	w.t.Helper()
	ids, err := w.recipients.PendingIDs(context.Background(), w.campaign.ID, nil, "", 1_000_000)
	if err != nil {
		w.t.Fatal(err)
	}
	return ids
}

func (w *billingWorld) state() domain.BillingState {
	w.t.Helper()
	s, err := w.store.BillingState(context.Background(), w.campaign.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	return s
}

func (w *billingWorld) paidUnits() int64 {
	var paid int64
	if err := w.db.Raw("SELECT paid_units FROM email_campaigns WHERE id = ?", w.campaign.ID).Scan(&paid).Error; err != nil {
		w.violations.Add(1)
	}
	return paid
}

func (w *billingWorld) send(id string) error {
	ctx := context.Background()
	if err := w.billing.Claim(ctx, w.campaign, id); err != nil {
		return err
	}
	if sentNow := w.sent.Add(1); sentNow > w.paidUnits() {
		w.violations.Add(1)
	}
	ok, err := w.recipients.MarkSent(ctx, id, "", time.Now().UTC())
	if err != nil || !ok {
		return fmt.Errorf("mark sent %s: %v %w", id, ok, err)
	}
	return nil
}

func (w *billingWorld) deliver(ids []string) {
	w.t.Helper()
	const workers = 8
	var wg sync.WaitGroup
	var failure atomic.Value
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := i; j < len(ids); j += workers {
				if err := w.send(ids[j]); err != nil {
					failure.CompareAndSwap(nil, fmt.Sprintf("send %s: %v", ids[j], err))
				}
			}
		}(i)
	}
	wg.Wait()
	if msg := failure.Load(); msg != nil {
		w.t.Fatal(msg)
	}
}

func (w *billingWorld) swap(from, to domain.Status) {
	w.t.Helper()
	if ok, err := w.campaigns.SwapStatus(context.Background(), w.campaign.ID, from, to, domain.HoldNone); err != nil || !ok {
		w.t.Fatalf("swap %s to %s = %v %v", from, to, ok, err)
	}
	w.campaign = w.current()
}

func (w *billingWorld) settle() {
	w.t.Helper()
	settled, err := w.billing.Settle(context.Background(), w.current())
	if err != nil || !settled {
		w.t.Fatalf("settle = %v %v", settled, err)
	}
}

func (w *billingWorld) wallet() int64 {
	w.t.Helper()
	var row schema.Balance
	if err := w.db.Where("workspace_id = ?", w.ws).First(&row).Error; err != nil {
		w.t.Fatal(err)
	}
	return row.Amount
}

func (w *billingWorld) ledgerRows(kind, pattern string) []schema.BalanceTransaction {
	w.t.Helper()
	var rows []schema.BalanceTransaction
	if err := w.db.Where("workspace_id = ? AND type = ? AND reference_id LIKE ?", w.ws, kind, pattern).Order("created_at").Find(&rows).Error; err != nil {
		w.t.Fatal(err)
	}
	return rows
}

func (w *billingWorld) batchDebits() []schema.BalanceTransaction {
	return w.ledgerRows("debit", "email_campaign:"+w.campaign.ID+":batch:%")
}

func (w *billingWorld) batchRefunds() []schema.BalanceTransaction {
	return w.ledgerRows("credit", "refund:email_campaign:"+w.campaign.ID+":batch:%")
}

func (w *billingWorld) assertNoOversend() {
	w.t.Helper()
	if w.violations.Load() != 0 {
		w.t.Fatalf("%d sends went out beyond the paid units", w.violations.Load())
	}
	counts, err := w.recipients.Counts(context.Background(), w.campaign.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	if s := w.state().Slots; counts.Sent > s.Used || s.Used > s.Paid {
		w.t.Fatalf("sent %d, used %d, paid %d: want sent <= used <= paid", counts.Sent, s.Used, s.Paid)
	}
	repotest.AssertLedgerChains(w.t, w.db, w.ws, w.start)
}

func (w *billingWorld) concurrentSenders(workers int, crashEvery int) {
	w.t.Helper()
	ids := w.pendingIDs()
	queue := make(chan string, len(ids)*2)
	for _, id := range ids {
		queue <- id
	}
	var crashed sync.Map
	var outstanding atomic.Int64
	outstanding.Store(int64(len(ids)))
	var wg sync.WaitGroup
	var failure atomic.Value
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for outstanding.Load() > 0 {
				var id string
				select {
				case id = <-queue:
				default:
					time.Sleep(time.Millisecond)
					continue
				}
				if _, again := crashed.LoadOrStore(id, true); !again && crashEvery > 0 && int(id[len(id)-1])%crashEvery == 0 {
					if err := w.billing.Claim(context.Background(), w.campaign, id); err != nil {
						failure.CompareAndSwap(nil, err.Error())
					}
					queue <- id
					continue
				}
				err := w.send(id)
				var short *balance_usecase.InsufficientEmailBalanceError
				switch {
				case err == nil:
					outstanding.Add(-1)
				case errors.Is(err, emailcampaign_usecase.ErrNoSlot):
					queue <- id
				case errors.As(err, &short):
					outstanding.Add(-1)
				default:
					failure.CompareAndSwap(nil, err.Error())
					outstanding.Add(-1)
				}
			}
		}()
	}
	wg.Wait()
	if msg := failure.Load(); msg != nil {
		w.t.Fatalf("a sender failed: %v", msg)
	}
}

func TestRunBillingIntegration_ConcurrentSendersPayFiveBatchesForFiveThousand(t *testing.T) {
	w := newBillingWorld(t, 5_000, scenarioWallet)
	w.concurrentSenders(32, 0)
	if got := w.sent.Load(); got != 5_000 {
		t.Fatalf("sent = %d", got)
	}
	if got := len(w.batchDebits()); got != 5 {
		t.Fatalf("batch debits = %d, want exactly 5", got)
	}
	w.settle()
	if got, want := w.wallet(), w.start-5_000*w.unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertNoOversend()
}

func TestRunBillingIntegration_RedeliveriesReuseTheSlotTheyHold(t *testing.T) {
	w := newBillingWorld(t, 5_000, scenarioWallet)
	w.concurrentSenders(32, 5)
	sent := w.sent.Load()
	if sent != 5_000 {
		t.Fatalf("sent = %d", sent)
	}
	if got, want := int64(len(w.batchDebits())), (sent+domain.BillingBatchSize-1)/domain.BillingBatchSize; got != want {
		t.Fatalf("batch debits = %d, want %d", got, want)
	}
	if s := w.state().Slots; s.Used != sent || s.Paid != sent {
		t.Fatalf("slots = %+v, want one slot per sent recipient", s)
	}
	w.settle()
	if got, want := w.wallet(), w.start-sent*w.unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertNoOversend()
}

func TestRunBillingIntegration_ACrashedClaimIsReusedOnRedelivery(t *testing.T) {
	w := newBillingWorld(t, 3, scenarioWallet)
	ids := w.pendingIDs()
	if err := w.billing.Claim(context.Background(), w.campaign, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := w.billing.Claim(context.Background(), w.campaign, ids[0]); err != nil {
		t.Fatal(err)
	}
	if s := w.state().Slots; s.Used != 1 || s.Paid != 3 {
		t.Fatalf("slots = %+v, want one slot for the redelivered recipient", s)
	}
	w.deliver(ids)
	if s := w.state().Slots; s.Used != 3 {
		t.Fatalf("slots = %+v", s)
	}
}

func TestRunBillingIntegration_AWalletFundedForTwoThousandStopsAtTwoThousand(t *testing.T) {
	w := newBillingWorld(t, 5_000, 0)
	if err := w.db.Model(&schema.Balance{}).Where("workspace_id = ?", w.ws).Update("amount", 2_000*w.unit).Error; err != nil {
		t.Fatal(err)
	}
	w.start = 2_000 * w.unit
	w.concurrentSenders(32, 0)
	if got := w.sent.Load(); got != 2_000 {
		t.Fatalf("sent = %d, want exactly the 2000 paid", got)
	}
	if got := len(w.batchDebits()); got != 2 {
		t.Fatalf("batch debits = %d", got)
	}
	if got := w.wallet(); got != 0 {
		t.Fatalf("wallet = %d, want 0", got)
	}
	w.assertNoOversend()
}

func pausedAt1400(t *testing.T) *billingWorld {
	t.Helper()
	w := newBillingWorld(t, 3_000, scenarioWallet)
	w.deliver(w.pendingIDs()[:1_400])
	w.swap(domain.StatusRunning, domain.StatusPaused)
	return w
}

func TestRunBillingIntegration_PauseKeepsThePaidSlotsAndChargesNothing(t *testing.T) {
	w := pausedAt1400(t)
	if s := w.state().Slots; s.Paid != 2_000 || s.Used != 1_400 {
		t.Fatalf("slots = %+v", s)
	}
	err := w.billing.Claim(context.Background(), w.campaign, w.pendingIDs()[0])
	if !errors.Is(err, emailcampaign_usecase.ErrNotSending) {
		t.Fatalf("a claim while paused = %v", err)
	}
	if got := len(w.batchDebits()); got != 2 || len(w.batchRefunds()) != 0 {
		t.Fatalf("debits %d refunds %d, want no charge and no refund on pause", got, len(w.batchRefunds()))
	}
	if got, want := w.wallet(), w.start-2_000*w.unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertNoOversend()
}

func TestRunBillingIntegration_ResumeUsesBatchTwoThenChargesBatchThreeOnce(t *testing.T) {
	w := pausedAt1400(t)
	w.swap(domain.StatusPaused, domain.StatusRunning)
	pending := w.pendingIDs()
	w.deliver(pending[:600])
	if got := len(w.batchDebits()); got != 2 {
		t.Fatalf("batch 2's slots need no new debit, debits = %d", got)
	}
	w.deliver(pending[600:601])
	if got := len(w.batchDebits()); got != 3 {
		t.Fatalf("the 2001st send charges batch 3, debits = %d", got)
	}
	w.deliver(pending[601:])
	w.settle()
	if got := len(w.batchDebits()); got != 3 || len(w.batchRefunds()) != 0 {
		t.Fatalf("debits %d refunds %d", got, len(w.batchRefunds()))
	}
	if got, want := w.wallet(), w.start-3_000*w.unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertNoOversend()
}

func TestRunBillingIntegration_StopWhilePausedRefundsBatchTwoOnce(t *testing.T) {
	w := pausedAt1400(t)
	w.swap(domain.StatusPaused, domain.StatusStopped)
	w.settle()
	refunds := w.batchRefunds()
	if len(refunds) != 1 || refunds[0].Amount != 600*w.unit || *refunds[0].ReferenceID != "refund:"+balance_usecase.EmailCampaignBatchReference(w.campaign.ID, 2) {
		t.Fatalf("refunds = %+v, want 600 of batch 2", refunds)
	}
	if got, want := w.wallet(), w.start-1_400*w.unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.settle()
	late := w.pendingIDs()[0]
	if err := w.billing.Claim(context.Background(), w.current(), late); !errors.Is(err, emailcampaign_usecase.ErrNotSending) {
		t.Fatalf("a late redelivery after the stop = %v", err)
	}
	if got := len(w.batchDebits()); got != 2 || len(w.batchRefunds()) != 1 {
		t.Fatalf("debits %d refunds %d, want nothing more", got, len(w.batchRefunds()))
	}
	w.assertNoOversend()
}

func TestRunBillingIntegration_RefusedRecipientsAreRefundedAtCompletion(t *testing.T) {
	w := pausedAt1400(t)
	w.swap(domain.StatusPaused, domain.StatusRunning)
	pending := w.pendingIDs()
	w.deliver(pending[:1_550])
	for _, id := range pending[1_550:] {
		if err := w.billing.Claim(context.Background(), w.campaign, id); err != nil {
			t.Fatal(err)
		}
		if ok, err := w.recipients.MarkSkipped(context.Background(), id, domain.SkipUnsubscribed); err != nil || !ok {
			t.Fatalf("skip = %v %v", ok, err)
		}
	}
	if s := w.state().Slots; s.Paid != 3_000 || s.Used != 2_950 {
		t.Fatalf("slots = %+v, want the 50 refused slots given back", s)
	}
	w.settle()
	refunds := w.batchRefunds()
	if len(refunds) != 1 || refunds[0].Amount != 50*w.unit {
		t.Fatalf("refunds = %+v, want 50 of batch 3", refunds)
	}
	if got, want := w.wallet(), w.start-2_950*w.unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertNoOversend()
}

func TestRunBillingIntegration_APriceChangeKeepsEachBatchAtItsOwnPrice(t *testing.T) {
	w := pausedAt1400(t)
	oldUnit := w.unit
	newUnit := oldUnit + 117
	w.setSendPrice(newUnit)
	w.swap(domain.StatusPaused, domain.StatusRunning)
	pending := w.pendingIDs()
	w.deliver(pending[:600])
	if got := len(w.batchDebits()); got != 2 {
		t.Fatalf("batch 2's slots stay at the old price, debits = %d", got)
	}
	w.deliver(pending[600:601])
	debits := w.batchDebits()
	if len(debits) != 3 || debits[1].Amount != 1_000*oldUnit || debits[2].Amount != 1_000*newUnit {
		t.Fatalf("debits = %+v, want batch 2 at %d and batch 3 at %d", debits, oldUnit, newUnit)
	}
	w.swap(domain.StatusRunning, domain.StatusStopped)
	w.settle()
	refunds := w.batchRefunds()
	if len(refunds) != 1 || refunds[0].Amount != 999*newUnit {
		t.Fatalf("refunds = %+v, want 999 of batch 3 at %d", refunds, newUnit)
	}
	if got, want := w.wallet(), w.start-2_000*oldUnit-newUnit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertNoOversend()
}

func TestRunBillingIntegration_WithoutTheLockTheDatabaseStillNeverOversends(t *testing.T) {
	w := newBillingWorld(t, 5_000, scenarioWallet)
	w.locks.broken = true
	w.concurrentSenders(32, 0)
	if got := w.sent.Load(); got != 5_000 {
		t.Fatalf("sent = %d", got)
	}
	if got := len(w.batchDebits()); got < 5 {
		t.Fatalf("batch debits = %d, want at least 5", got)
	}
	w.settle()
	if got, want := w.wallet(), w.start-5_000*w.unit; got != want {
		t.Fatalf("wallet = %d, want %d: any batch bought twice is refunded at settlement", got, want)
	}
	w.assertNoOversend()
}
