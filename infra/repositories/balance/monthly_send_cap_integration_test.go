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
	"vozko/infra/database/schema"
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
	if err := db.AutoMigrate(&schema.User{}, &schema.Workspace{}, &schema.Balance{}, &schema.BalanceTransaction{}, &schema.WorkspaceMonthlySendCap{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedCappedWorkspace(t *testing.T, db *gorm.DB, name string, amount int64) string {
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

func templateDebit(workspaceID string, guard *balance.MonthlySendCapGuard) balance.DebitBalanceInput {
	ref := uuid.New().String()
	return balance.DebitBalanceInput{
		WorkspaceID:        workspaceID,
		Amount:             1_000,
		ServiceType:        balance.ServiceWhatsAppCampaign,
		ReferenceID:        &ref,
		Description:        "Template WhatsApp marketing (ref: " + ref + ")",
		ExchangeRateMicros: 6_000_000,
		MonthlyCap:         guard,
	}
}

func TestSendCapIntegration_CountsNetSendsOfTheMonthOnly(t *testing.T) {
	db := sendCapIntegrationDB(t)
	repo := NewRepository(db)
	ws := seedCappedWorkspace(t, db, "Acme", 1_000_000)
	now := time.Now()
	guard := balance.MonthlySendCap{WorkspaceID: ws, Limit: 3}.Guard(now)

	for i := 0; i < 3; i++ {
		if _, err := repo.DebitBalance(templateDebit(ws, &guard)); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if _, err := repo.DebitBalance(templateDebit(ws, &guard)); !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("the fourth send must hit the cap, got %v", err)
	}

	refundRef := "refund:" + uuid.New().String()
	if _, err := repo.CreditBalance(balance.CreditBalanceInput{
		WorkspaceID: ws, Amount: 1_000, ServiceType: balance.ServiceWhatsAppCampaign,
		ReferenceID: &refundRef, Description: "Reembolso", IsRefund: true, ExchangeRateMicros: 6_000_000,
	}); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if _, err := repo.DebitBalance(templateDebit(ws, &guard)); err != nil {
		t.Fatalf("a refunded send frees one slot, got %v", err)
	}

	lastMonth := guard.Since.Add(-time.Hour)
	if err := db.Model(&schema.BalanceTransaction{}).Where("workspace_id = ?", ws).Update("created_at", lastMonth).Error; err != nil {
		t.Fatalf("age rows: %v", err)
	}
	if _, err := repo.DebitBalance(templateDebit(ws, &guard)); err != nil {
		t.Fatalf("sends from last month must not count, got %v", err)
	}
}

func TestSendCapIntegration_OtherServicesDoNotCount(t *testing.T) {
	db := sendCapIntegrationDB(t)
	repo := NewRepository(db)
	ws := seedCappedWorkspace(t, db, "Acme", 1_000_000)
	guard := balance.MonthlySendCap{WorkspaceID: ws, Limit: 1}.Guard(time.Now())

	aiRef := uuid.New().String()
	if _, err := repo.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID: ws, Amount: 1_000, ServiceType: balance.ServiceAI, ReferenceID: &aiRef, ExchangeRateMicros: 6_000_000,
	}); err != nil {
		t.Fatalf("ai debit: %v", err)
	}
	if _, err := repo.DebitBalance(templateDebit(ws, &guard)); err != nil {
		t.Fatalf("an AI charge is not a template send, got %v", err)
	}
}

func TestSendCapIntegration_ConcurrentSendersNeverOvershoot(t *testing.T) {
	db := sendCapIntegrationDB(t)
	repo := NewRepository(db)
	ws := seedCappedWorkspace(t, db, "Acme", 1_000_000)
	const limit, senders = 5, 20
	guard := balance.MonthlySendCap{WorkspaceID: ws, Limit: limit}.Guard(time.Now())

	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted, capped := 0, 0
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.DebitBalance(templateDebit(ws, &guard))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				admitted++
			case errors.Is(err, balance.ErrMonthlySendCapReached):
				capped++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if admitted != limit || capped != senders-limit {
		t.Fatalf("admitted %d, capped %d; want %d and %d", admitted, capped, limit, senders-limit)
	}
	var stored int64
	db.Model(&schema.BalanceTransaction{}).Where("workspace_id = ?", ws).Count(&stored)
	if stored != limit {
		t.Fatalf("ledger holds %d debits, want %d", stored, limit)
	}
}

func TestSendCapIntegration_RepositoryRoundTripAndUsage(t *testing.T) {
	db := sendCapIntegrationDB(t)
	caps := NewMonthlySendCapRepository(db)
	ledger := NewRepository(db)
	ws := seedCappedWorkspace(t, db, "Acme", 1_000_000)
	other := seedCappedWorkspace(t, db, "Beta", 1_000_000)
	now := time.Now().UTC().Truncate(time.Second)

	missing, err := caps.GetMonthlySendCap(ws)
	if err != nil || missing != nil {
		t.Fatalf("no row means no cap, got %+v, %v", missing, err)
	}

	cap, _ := balance.NewMonthlySendCap(ws, 10, uuid.New().String(), now)
	if err := caps.UpsertMonthlySendCap(cap); err != nil {
		t.Fatalf("create: %v", err)
	}
	unlocked, _ := cap.Unlocked(20, uuid.New().String(), now)
	if err := caps.UpsertMonthlySendCap(unlocked); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, err := caps.GetMonthlySendCap(ws)
	if err != nil || got == nil || got.Limit != 20 || got.UnlockedBy == nil || *got.UnlockedBy != *unlocked.UnlockedBy {
		t.Fatalf("round trip lost data: %+v, %v", got, err)
	}

	if err := caps.UpsertMonthlySendCap(balance.MonthlySendCap{WorkspaceID: other, Limit: 0, UpdatedBy: uuid.New().String(), UpdatedAt: now}); err == nil {
		t.Fatal("the database must refuse a non positive limit")
	}

	guard := got.Guard(time.Now())
	for i := 0; i < 4; i++ {
		if _, err := ledger.DebitBalance(templateDebit(ws, &guard)); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	usages, err := caps.ListMonthlySendCapUsage(guard.Since)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(usages) != 1 || usages[0].WorkspaceName != "Acme" || usages[0].Used != 4 || usages[0].Cap.Limit != 20 {
		t.Fatalf("unexpected usage %+v", usages)
	}

	if err := caps.DeleteMonthlySendCap(ws); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if gone, err := caps.GetMonthlySendCap(ws); err != nil || gone != nil {
		t.Fatalf("cap must be gone, got %+v, %v", gone, err)
	}
}
