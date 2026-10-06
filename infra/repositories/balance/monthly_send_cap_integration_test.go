package balance_repository

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/balance"
	"vozko/domain/billing"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/database/schema"
	balance_usecase "vozko/usecases/balance"
)

var (
	september = balance.SendCapCycleStart(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC), 1)
	october   = balance.SendCapCycleStart(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC), 1)
)

func sendCapIntegrationDSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
}

func sendCapIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(sendCapIntegrationDSN()), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "sendcap_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(sendCapIntegrationDSN()+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&schema.User{}, &schema.Workspace{}, &schema.Balance{}, &schema.BalanceTransaction{},
		&schema.PricingItem{}, &schema.WorkspaceMonthlySendCap{}, &schema.WorkspaceMonthlySendSlot{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedWorkspace(t *testing.T, db *gorm.DB, name string, amount int64) string {
	t.Helper()
	owner := schema.User{ID: uuid.New().String(), Username: "owner-" + uuid.New().String()[:8], Email: uuid.New().String()[:8] + "@example.com", Password: "x"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := schema.Workspace{ID: uuid.New().String(), OwnerID: owner.ID, Name: name}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if err := db.Create(&schema.Balance{ID: uuid.New().String(), WorkspaceID: ws.ID, Amount: amount, Currency: "USD"}).Error; err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	return ws.ID
}

func capWorkspace(t *testing.T, caps *MonthlySendCapRepository, workspaceID string, limit int64) {
	t.Helper()
	cap, err := balance.NewMonthlySendCap(workspaceID, limit, 1, uuid.New().String(), time.Now())
	if err != nil {
		t.Fatalf("cap: %v", err)
	}
	if err := caps.UpsertMonthlySendCap(cap); err != nil {
		t.Fatalf("upsert cap: %v", err)
	}
}

func chargeTemplate(t *testing.T, ledger balance.Repository, workspaceID string, at time.Time, db *gorm.DB) {
	t.Helper()
	ref := uuid.New().String()
	tx, err := ledger.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID: workspaceID, Amount: 1_000, ServiceType: balance.ServiceWhatsAppCampaign,
		ReferenceID: &ref, Description: "Template WhatsApp marketing", ExchangeRateMicros: 6_000_000,
	})
	if err != nil {
		t.Fatalf("charge: %v", err)
	}
	if err := db.Model(&schema.BalanceTransaction{}).Where("id = ?", tx.ID).Update("created_at", at).Error; err != nil {
		t.Fatalf("date charge: %v", err)
	}
}

func capState(t *testing.T, db *gorm.DB, workspaceID string) (schema.WorkspaceMonthlySendCap, int64) {
	t.Helper()
	var row schema.WorkspaceMonthlySendCap
	if err := db.Where("workspace_id = ?", workspaceID).First(&row).Error; err != nil {
		t.Fatalf("read cap: %v", err)
	}
	var slots int64
	db.Model(&schema.WorkspaceMonthlySendSlot{}).Where("workspace_id = ?", workspaceID).Count(&slots)
	return row, slots
}

func TestSendCapIntegration_UncappedWorkspaceTakesNothing(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)

	took, err := caps.TakeMonthlySendSlot(ws, "entry-1", september)
	if err != nil || took {
		t.Fatalf("no cap means no slot, got %v, %v", took, err)
	}
	var slots int64
	db.Model(&schema.WorkspaceMonthlySendSlot{}).Count(&slots)
	if slots != 0 {
		t.Fatalf("an uncapped workspace writes nothing, found %d slots", slots)
	}
	if err := caps.GiveBackMonthlySendSlot(ws, "entry-1"); err != nil {
		t.Fatalf("giving back nothing is fine, got %v", err)
	}
}

func TestSendCapIntegration_FirstTakeSeedsFromTheLedgerOfThisMonthOnly(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	chargeTemplate(t, ledger, ws, september.Add(time.Hour), db)
	chargeTemplate(t, ledger, ws, september.Add(2*time.Hour), db)
	chargeTemplate(t, ledger, ws, september.Add(-time.Hour), db)
	capWorkspace(t, caps, ws, 3)

	if took, err := caps.TakeMonthlySendSlot(ws, "entry-1", september); err != nil || !took {
		t.Fatalf("the third send of the month fits, got %v, %v", took, err)
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "entry-2", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("the fourth send must be refused, got %v", err)
	}
	row, slots := capState(t, db, ws)
	if row.Used != 3 || row.CountedFrom == nil || !row.CountedFrom.Equal(september) || slots != 1 {
		t.Fatalf("used %d from %v with %d slots; want 3 from September with 1 slot", row.Used, row.CountedFrom, slots)
	}
}

func TestSendCapIntegration_TakeAndGiveBackAreIdempotent(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 5)

	if took, err := caps.TakeMonthlySendSlot(ws, "entry-1", september); err != nil || !took {
		t.Fatalf("first take: %v, %v", took, err)
	}
	if took, err := caps.TakeMonthlySendSlot(ws, "entry-1", september); err != nil || took {
		t.Fatalf("a redelivered take reports the slot as already held, got %v, %v", took, err)
	}
	if row, slots := capState(t, db, ws); row.Used != 1 || slots != 1 {
		t.Fatalf("used %d, slots %d; want 1 and 1", row.Used, slots)
	}

	for i := 0; i < 3; i++ {
		if err := caps.GiveBackMonthlySendSlot(ws, "entry-1"); err != nil {
			t.Fatalf("give back %d: %v", i, err)
		}
	}
	if err := caps.GiveBackMonthlySendSlot(ws, "never-taken"); err != nil {
		t.Fatalf("giving back an unknown slot: %v", err)
	}
	if row, slots := capState(t, db, ws); row.Used != 0 || slots != 0 {
		t.Fatalf("used %d, slots %d; a slot comes back exactly once", row.Used, slots)
	}
}

func TestSendCapIntegration_ConcurrentReplicasNeverOvershoot(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	const limit, senders = 5, 40
	capWorkspace(t, caps, ws, limit)

	var wg sync.WaitGroup
	var mu sync.Mutex
	took, refused := 0, 0
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := caps.TakeMonthlySendSlot(ws, fmt.Sprintf("entry-%d", i), september)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && ok:
				took++
			case errors.Is(err, balance.ErrMonthlySendCapReached):
				refused++
			default:
				t.Errorf("unexpected result %v, %v", ok, err)
			}
		}(i)
	}
	wg.Wait()

	row, slots := capState(t, db, ws)
	if took != limit || refused != senders-limit || row.Used != limit || slots != limit {
		t.Fatalf("took %d refused %d used %d slots %d; want %d, %d, %d, %d", took, refused, row.Used, slots, limit, senders-limit, limit, limit)
	}
}

func TestSendCapIntegration_SameSendDeliveredToManyReplicasTakesOneSlot(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 5)

	var wg sync.WaitGroup
	var mu sync.Mutex
	newSlots := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := caps.TakeMonthlySendSlot(ws, "entry-1", september)
			if err != nil {
				t.Errorf("take: %v", err)
				return
			}
			if ok {
				mu.Lock()
				newSlots++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if row, slots := capState(t, db, ws); newSlots != 1 || row.Used != 1 || slots != 1 {
		t.Fatalf("new slots %d, used %d, rows %d; want exactly one", newSlots, row.Used, slots)
	}
}

func TestSendCapIntegration_NewMonthStartsEmptyAndOldSlotsGoAway(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 2)

	for _, ref := range []string{"sep-1", "sep-2"} {
		if _, err := caps.TakeMonthlySendSlot(ws, ref, september); err != nil {
			t.Fatalf("september take: %v", err)
		}
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "sep-3", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("september is full, got %v", err)
	}

	if took, err := caps.TakeMonthlySendSlot(ws, "oct-1", october); err != nil || !took {
		t.Fatalf("october starts empty, got %v, %v", took, err)
	}
	if err := caps.GiveBackMonthlySendSlot(ws, "sep-1"); err != nil {
		t.Fatalf("late refund of september: %v", err)
	}
	row, slots := capState(t, db, ws)
	if row.Used != 1 || !row.CountedFrom.Equal(october) || slots != 1 {
		t.Fatalf("used %d from %v with %d slots; a september refund must not free an october slot", row.Used, row.CountedFrom, slots)
	}

	if took, err := caps.TakeMonthlySendSlot(ws, "late-sep", september); err != nil || !took {
		t.Fatalf("a replica whose clock is still in september counts into the open month, got %v, %v", took, err)
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "oct-2", october); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("october is now full, got %v", err)
	}
}

func TestSendCapIntegration_AdminChangesKeepTheCountAndDeleteClearsIt(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 2)
	for _, ref := range []string{"entry-1", "entry-2"} {
		if _, err := caps.TakeMonthlySendSlot(ws, ref, september); err != nil {
			t.Fatalf("take: %v", err)
		}
	}

	current, _ := caps.GetMonthlySendCap(ws)
	raised, _ := current.Unlocked(3, uuid.New().String(), time.Now())
	if err := caps.UpsertMonthlySendCap(raised); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if row, _ := capState(t, db, ws); row.Used != 2 || row.MonthlyLimit != 3 {
		t.Fatalf("raising keeps the count: used %d, limit %d", row.Used, row.MonthlyLimit)
	}
	if took, err := caps.TakeMonthlySendSlot(ws, "entry-3", september); err != nil || !took {
		t.Fatalf("one more fits after the raise, got %v, %v", took, err)
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "entry-4", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("and then it is full again, got %v", err)
	}

	if err := caps.DeleteMonthlySendCap(ws); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var capsLeft, slotsLeft int64
	db.Model(&schema.WorkspaceMonthlySendCap{}).Where("workspace_id = ?", ws).Count(&capsLeft)
	db.Model(&schema.WorkspaceMonthlySendSlot{}).Where("workspace_id = ?", ws).Count(&slotsLeft)
	if capsLeft != 0 || slotsLeft != 0 {
		t.Fatalf("delete leaves %d caps and %d slots", capsLeft, slotsLeft)
	}
	if took, err := caps.TakeMonthlySendSlot(ws, "entry-5", september); err != nil || took {
		t.Fatalf("without a cap nothing is taken, got %v, %v", took, err)
	}
}

func TestSendCapIntegration_ListShowsTheMonthUsage(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	counting := seedWorkspace(t, db, "Counting", 1_000_000)
	fresh := seedWorkspace(t, db, "Fresh", 1_000_000)
	stale := seedWorkspace(t, db, "Stale", 1_000_000)
	for _, ws := range []string{counting, fresh, stale} {
		capWorkspace(t, caps, ws, 10)
	}
	for _, ref := range []string{"a", "b", "c"} {
		if _, err := caps.TakeMonthlySendSlot(counting, ref, october); err != nil {
			t.Fatalf("take: %v", err)
		}
	}
	chargeTemplate(t, ledger, fresh, october.Add(time.Hour), db)
	chargeTemplate(t, ledger, fresh, october.Add(2*time.Hour), db)
	if _, err := caps.TakeMonthlySendSlot(stale, "old", september); err != nil {
		t.Fatalf("take: %v", err)
	}

	usages, err := caps.ListMonthlySendCapUsage(october)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]int64{}
	for _, u := range usages {
		got[u.WorkspaceName] = u.Used
	}
	want := map[string]int64{"Counting": 3, "Fresh": 2, "Stale": 0}
	for name, used := range want {
		if got[name] != used {
			t.Fatalf("usage %v, want %v", got, want)
		}
	}
}

func TestSendCapIntegration_DatabaseRefusesANonPositiveLimit(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)

	err := caps.UpsertMonthlySendCap(balance.MonthlySendCap{WorkspaceID: ws, Limit: 0, UpdatedBy: uuid.New().String(), UpdatedAt: time.Now()})
	if err == nil {
		t.Fatal("the database must refuse a non positive limit")
	}
}

type flatPricing struct{}

func (flatPricing) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return workspace_pricing.DefaultPricingCatalog, nil
}
func (flatPricing) GetPricingItem(string) (*workspace_pricing.PricingItem, error) { return nil, nil }
func (flatPricing) UpsertPricingItem(*workspace_pricing.PricingItem) error        { return nil }
func (flatPricing) DeletePricingItem(string) error                                { return nil }
func (flatPricing) SeedDefaults([]workspace_pricing.PricingItem) error            { return nil }
func (flatPricing) CreateAuditEntry(*workspace_pricing.PricingAuditEntry) error   { return nil }
func (flatPricing) ListAuditEntries(*string, int, int) ([]workspace_pricing.PricingAuditEntry, error) {
	return nil, nil
}

type activeSubscription struct{}

func (activeSubscription) Execute(string) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, nil
}

func TestSendCapIntegration_EndToEndThroughTheChargeUseCase(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	consume := balance_usecase.NewConsumeWhatsappTemplateUseCase(ledger, workspace_pricing.NewPricer(flatPricing{}), activeSubscription{}, caps)
	price, err := consume.GetTemplateCostMicros(uuid.New().String(), "MARKETING")
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	ws := seedWorkspace(t, db, "Acme", price*3)
	uncapped := seedWorkspace(t, db, "Free", price*10)
	capWorkspace(t, caps, ws, 2)

	var refs []string
	for i := 0; i < 2; i++ {
		ref := uuid.New().String()
		refs = append(refs, ref)
		if _, err := consume.Execute(ws, ref, "MARKETING"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if _, err := consume.Execute(ws, uuid.New().String(), "MARKETING"); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("the third send hits the cap, got %v", err)
	}

	if err := consume.Refund(ws, refs[0], "MARKETING"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if row, slots := capState(t, db, ws); row.Used != 1 || slots != 1 {
		t.Fatalf("a refunded send frees its slot: used %d, slots %d", row.Used, slots)
	}

	if _, err := consume.Execute(ws, uuid.New().String(), "MARKETING"); err != nil {
		t.Fatalf("the freed slot is usable: %v", err)
	}

	if _, err := consume.Execute(ws, uuid.New().String(), "MARKETING"); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("full again, got %v", err)
	}

	if err := db.Model(&schema.Balance{}).Where("workspace_id = ?", ws).Update("amount", 0).Error; err != nil {
		t.Fatalf("drain balance: %v", err)
	}
	current, _ := caps.GetMonthlySendCap(ws)
	raised, _ := current.Unlocked(10, uuid.New().String(), time.Now())
	if err := caps.UpsertMonthlySendCap(raised); err != nil {
		t.Fatalf("raise: %v", err)
	}
	usedBefore, _ := capState(t, db, ws)
	if _, err := consume.Execute(ws, uuid.New().String(), "MARKETING"); !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Fatalf("no money left, got %v", err)
	}
	if usedAfter, slots := capState(t, db, ws); usedAfter.Used != usedBefore.Used || slots != int64(usedBefore.Used) {
		t.Fatalf("a send that could not be charged gives its slot back: used %d -> %d, slots %d", usedBefore.Used, usedAfter.Used, slots)
	}

	for i := 0; i < 5; i++ {
		if _, err := consume.Execute(uncapped, uuid.New().String(), "MARKETING"); err != nil {
			t.Fatalf("an uncapped workspace sends freely: %v", err)
		}
	}
	var uncappedSlots int64
	db.Model(&schema.WorkspaceMonthlySendSlot{}).Where("workspace_id = ?", uncapped).Count(&uncappedSlots)
	if uncappedSlots != 0 {
		t.Fatalf("an uncapped workspace writes no slots, found %d", uncappedSlots)
	}

	var ledgerRows int64
	db.Model(&schema.BalanceTransaction{}).Where("workspace_id = ?", ws).Count(&ledgerRows)
	var netCharges int64
	db.Raw(netTemplateSendsSinceSQL("?"), ws, time.Now().AddDate(0, -1, 0)).Scan(&netCharges)
	if row, _ := capState(t, db, ws); row.Used != netCharges {
		t.Fatalf("the running count (%d) must equal the ledger's net sends (%d); ledger rows %d", row.Used, netCharges, ledgerRows)
	}
}

func TestSendCapIntegration_LimitBelowWhatWasAlreadySentIsSeededOnce(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	for i := 0; i < 4; i++ {
		chargeTemplate(t, ledger, ws, september.Add(time.Duration(i+1)*time.Hour), db)
	}
	capWorkspace(t, caps, ws, 2)

	if _, err := caps.TakeMonthlySendSlot(ws, "entry-1", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("four sent against a limit of two is already over, got %v", err)
	}
	row, slots := capState(t, db, ws)
	if row.CountedFrom == nil || !row.CountedFrom.Equal(september) || row.Used != 4 || slots != 0 {
		t.Fatalf("a refused seed must still be kept so the ledger is not recounted on every refusal: used %d from %v, slots %d", row.Used, row.CountedFrom, slots)
	}

	chargeTemplate(t, ledger, ws, september.Add(10*time.Hour), db)
	if _, err := caps.TakeMonthlySendSlot(ws, "entry-2", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("still over, got %v", err)
	}
	if row, _ := capState(t, db, ws); row.Used != 4 {
		t.Fatalf("the kept seed is reused, not recounted: used %d", row.Used)
	}
}

func TestSendCapIntegration_SeedNetsRefundsOut(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	chargeTemplate(t, ledger, ws, september.Add(time.Hour), db)
	chargeTemplate(t, ledger, ws, september.Add(2*time.Hour), db)
	refundRef := "refund:" + uuid.New().String()
	refund, err := ledger.CreditBalance(balance.CreditBalanceInput{
		WorkspaceID: ws, Amount: 1_000, ServiceType: balance.ServiceWhatsAppCampaign,
		ReferenceID: &refundRef, IsRefund: true, ExchangeRateMicros: 6_000_000,
	})
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	db.Model(&schema.BalanceTransaction{}).Where("id = ?", refund.ID).Update("created_at", september.Add(3*time.Hour))
	capWorkspace(t, caps, ws, 2)

	if took, err := caps.TakeMonthlySendSlot(ws, "entry-1", september); err != nil || !took {
		t.Fatalf("two charges minus one refund leaves one slot, got %v, %v", took, err)
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "entry-2", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("then it is full, got %v", err)
	}
}

func TestSendCapIntegration_LoweringBelowTheCountRefusesAtOnce(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 5)
	for _, ref := range []string{"a", "b", "c"} {
		if _, err := caps.TakeMonthlySendSlot(ws, ref, september); err != nil {
			t.Fatalf("take: %v", err)
		}
	}
	current, _ := caps.GetMonthlySendCap(ws)
	lowered, _ := current.Relimited(2, uuid.New().String(), time.Now())
	if err := caps.UpsertMonthlySendCap(lowered); err != nil {
		t.Fatalf("lower: %v", err)
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "d", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("three used against a new limit of two refuses, got %v", err)
	}
	if err := caps.GiveBackMonthlySendSlot(ws, "a"); err != nil {
		t.Fatalf("give back: %v", err)
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "d", september); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("two used against two still refuses, got %v", err)
	}
}

func TestSendCapIntegration_GiveBackIgnoresAnotherWorkspacesSlot(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	owner := seedWorkspace(t, db, "Owner", 1_000_000)
	other := seedWorkspace(t, db, "Other", 1_000_000)
	capWorkspace(t, caps, owner, 5)
	capWorkspace(t, caps, other, 5)
	if _, err := caps.TakeMonthlySendSlot(owner, "entry-1", september); err != nil {
		t.Fatalf("take: %v", err)
	}

	if err := caps.GiveBackMonthlySendSlot(other, "entry-1"); err != nil {
		t.Fatalf("give back: %v", err)
	}
	if row, slots := capState(t, db, owner); row.Used != 1 || slots != 1 {
		t.Fatalf("another workspace cannot free this slot: used %d, slots %d", row.Used, slots)
	}
}

func TestSendCapIntegration_RecreatedCapSeedsAgainFromTheLedger(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 5)
	if _, err := caps.TakeMonthlySendSlot(ws, "entry-1", september); err != nil {
		t.Fatalf("take: %v", err)
	}
	chargeTemplate(t, ledger, ws, september.Add(time.Hour), db)
	chargeTemplate(t, ledger, ws, september.Add(2*time.Hour), db)
	if err := caps.DeleteMonthlySendCap(ws); err != nil {
		t.Fatalf("delete: %v", err)
	}
	capWorkspace(t, caps, ws, 5)

	if took, err := caps.TakeMonthlySendSlot(ws, "entry-2", september); err != nil || !took {
		t.Fatalf("take after recreate: %v, %v", took, err)
	}
	if row, _ := capState(t, db, ws); row.Used != 3 {
		t.Fatalf("the new cap starts from the ledger (2 charges) plus this send, got %d", row.Used)
	}
}

func TestSendCapIntegration_GiveBackRacingTakesAtTheLimitAdmitsExactlyOne(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 3)
	for _, ref := range []string{"a", "b", "c"} {
		if _, err := caps.TakeMonthlySendSlot(ws, ref, september); err != nil {
			t.Fatalf("take: %v", err)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := caps.GiveBackMonthlySendSlot(ws, "a"); err != nil {
			t.Errorf("give back: %v", err)
		}
	}()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := caps.TakeMonthlySendSlot(ws, fmt.Sprintf("new-%d", i), september)
			if err != nil && !errors.Is(err, balance.ErrMonthlySendCapReached) {
				t.Errorf("take: %v", err)
			}
			if ok {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if row, slots := capState(t, db, ws); admitted > 1 || row.Used != int64(3-1+admitted) || slots != row.Used {
		t.Fatalf("admitted %d, used %d, slots %d; at most the one freed slot may be reused", admitted, row.Used, slots)
	}
}

func TestSendCapIntegration_AdminEditsDuringTakesNeverDeadlockOrOvershoot(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspace(t, caps, ws, 5)

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := caps.TakeMonthlySendSlot(ws, fmt.Sprintf("entry-%d", i), september); err != nil && !errors.Is(err, balance.ErrMonthlySendCapReached) {
				t.Errorf("take: %v", err)
			}
		}(i)
	}
	for _, limit := range []int64{8, 3, 10} {
		wg.Add(1)
		go func(limit int64) {
			defer wg.Done()
			current, err := caps.GetMonthlySendCap(ws)
			if err != nil || current == nil {
				t.Errorf("get: %v", err)
				return
			}
			next, _ := current.Relimited(limit, uuid.New().String(), time.Now())
			if err := caps.UpsertMonthlySendCap(next); err != nil {
				t.Errorf("upsert: %v", err)
			}
		}(limit)
	}
	wg.Wait()

	row, slots := capState(t, db, ws)
	if row.Used != slots || row.Used > 10 {
		t.Fatalf("used %d, slots %d; the count must match the slots and never pass the highest limit", row.Used, slots)
	}
}

func capWorkspaceOnDay(t *testing.T, caps *MonthlySendCapRepository, workspaceID string, limit int64, day int) {
	t.Helper()
	cap, err := balance.NewMonthlySendCap(workspaceID, limit, day, uuid.New().String(), time.Now())
	if err != nil {
		t.Fatalf("cap: %v", err)
	}
	if err := caps.UpsertMonthlySendCap(cap); err != nil {
		t.Fatalf("upsert cap: %v", err)
	}
}

func brtDay(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, billing.LocationBRT())
}

func TestSendCapIntegration_CycleRunsFromTheChosenDay(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspaceOnDay(t, caps, ws, 2, 15)

	for i, at := range []time.Time{brtDay(2026, 9, 20, 10), brtDay(2026, 10, 14, 23)} {
		if took, err := caps.TakeMonthlySendSlot(ws, fmt.Sprintf("sep-cycle-%d", i), at); err != nil || !took {
			t.Fatalf("September 15 to October 14 is one cycle, send %d: %v, %v", i, took, err)
		}
	}
	if _, err := caps.TakeMonthlySendSlot(ws, "sep-cycle-full", brtDay(2026, 10, 14, 23)); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("the cycle is full until the 15th, got %v", err)
	}
	if took, err := caps.TakeMonthlySendSlot(ws, "oct-cycle-1", brtDay(2026, 10, 15, 0)); err != nil || !took {
		t.Fatalf("a new cycle opens on the 15th, got %v, %v", took, err)
	}
	row, slots := capState(t, db, ws)
	if row.Used != 1 || !row.CountedFrom.Equal(brtDay(2026, 10, 15, 0)) || slots != 1 {
		t.Fatalf("used %d from %v with %d slots; want 1 from October 15 with 1 slot", row.Used, row.CountedFrom, slots)
	}
}

func TestSendCapIntegration_UsageListsEachCapInItsOwnCycle(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	first := seedWorkspace(t, db, "First", 1_000_000)
	fifteenth := seedWorkspace(t, db, "Fifteenth", 1_000_000)
	capWorkspaceOnDay(t, caps, first, 10, 1)
	capWorkspaceOnDay(t, caps, fifteenth, 10, 15)
	for _, ws := range []string{first, fifteenth} {
		if _, err := caps.TakeMonthlySendSlot(ws, ws+"-a", brtDay(2026, 10, 10, 9)); err != nil {
			t.Fatalf("take: %v", err)
		}
	}

	usages, err := caps.ListMonthlySendCapUsage(brtDay(2026, 10, 20, 9))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]balance.SendCapUsage{}
	for _, u := range usages {
		got[u.WorkspaceName] = u
	}
	if u := got["First"]; u.Used != 1 || !u.CycleStart.Equal(brtDay(2026, 10, 1, 0)) || u.Cap.CycleDay != 1 {
		t.Fatalf("a day 1 cap on October 20 still counts October 10: %+v", u)
	}
	if u := got["Fifteenth"]; u.Used != 0 || !u.CycleStart.Equal(brtDay(2026, 10, 15, 0)) || u.Cap.CycleDay != 15 {
		t.Fatalf("a day 15 cap on October 20 has a fresh cycle: %+v", u)
	}
}

func TestSendCapIntegration_ChangingTheCycleDayResyncsFromTheLedger(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)
	capWorkspaceOnDay(t, caps, ws, 10, 1)
	chargeTemplate(t, ledger, ws, brtDay(2026, 9, 20, 10), db)
	chargeTemplate(t, ledger, ws, brtDay(2026, 10, 3, 10), db)
	if _, err := caps.TakeMonthlySendSlot(ws, "oct-a", brtDay(2026, 10, 5, 10)); err != nil {
		t.Fatalf("take: %v", err)
	}

	current, _ := caps.GetMonthlySendCap(ws)
	moved, err := current.Recycled(15)
	if err != nil {
		t.Fatalf("recycle: %v", err)
	}
	if err := caps.UpsertMonthlySendCap(moved); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if row, _ := capState(t, db, ws); row.CycleDay != 15 || row.CountedFrom != nil {
		t.Fatalf("a new cycle day must clear the saved count: day %d, counted from %v", row.CycleDay, row.CountedFrom)
	}

	if _, err := caps.TakeMonthlySendSlot(ws, "oct-b", brtDay(2026, 10, 6, 10)); err != nil {
		t.Fatalf("take: %v", err)
	}
	if row, _ := capState(t, db, ws); row.Used != 3 || !row.CountedFrom.Equal(brtDay(2026, 9, 15, 0)) {
		t.Fatalf("the September 15 cycle counts both ledger charges plus this send: used %d from %v", row.Used, row.CountedFrom)
	}

	relimited, _ := moved.Relimited(5, uuid.New().String(), time.Now())
	if err := caps.UpsertMonthlySendCap(relimited); err != nil {
		t.Fatalf("relimit: %v", err)
	}
	if row, _ := capState(t, db, ws); row.Used != 3 || row.CountedFrom == nil {
		t.Fatalf("changing only the limit keeps the count: used %d from %v", row.Used, row.CountedFrom)
	}
}

func TestSendCapIntegration_DatabaseRefusesACycleDayOutsideTheMonth(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ws := seedWorkspace(t, db, "Acme", 1_000_000)

	err := caps.UpsertMonthlySendCap(balance.MonthlySendCap{WorkspaceID: ws, Limit: 10, CycleDay: 32, UpdatedBy: uuid.New().String(), UpdatedAt: time.Now()})
	if err == nil {
		t.Fatal("the database must refuse a cycle day of 32")
	}
}
