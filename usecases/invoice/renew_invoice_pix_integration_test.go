package invoice_usecase

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"vozko/domain/invoice"
	"vozko/domain/user"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	invoice_repository "vozko/infra/repositories/invoice"
	"vozko/infra/repositories/repotest"
)

func renewalDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "invoice_renewal", &schema.Invoice{}, &schema.InvoiceSupersededCharge{})
	for _, name := range []string{"ux_invoices_idempotency_key", "ux_invoices_provider_external_id"} {
		sql, ok := database.PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	return db
}

func seedOverdueInvoice(t *testing.T, repo invoice.Repository) *invoice.Invoice {
	t.Helper()
	seed := overdueMonthlyInvoice()
	seed.ID = "7b0f1c9e-2d4a-4c1b-9a8e-3f5d6c7b8a90"
	seed.WorkspaceID = "1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	seed.UserID = "9f8e7d6c-5b4a-4392-8170-6f5e4d3c2b1a"
	seed.ExternalID = ""
	seed.Status = invoice.StatusPending
	saved, isNew, err := repo.Reserve(seed)
	if err != nil || !isNew {
		t.Fatalf("Reserve: isNew=%v err=%v", isNew, err)
	}
	if err := repo.AttachCharge(saved.ID, invoice.ChargeAttachment{
		ExternalID: "pay_old", PixQrCode: seed.PixQrCode, PixCopy: seed.PixCopy, ExpiresAt: seed.ChargeExpiresAt,
	}); err != nil {
		t.Fatalf("AttachCharge: %v", err)
	}
	if err := repo.UpdateStatus(saved.ID, invoice.StatusOverdue); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	return seed
}

func TestParallelRenewalsIssueExactlyOneCharge(t *testing.T) {
	db := renewalDB(t)
	repo := invoice_repository.NewRepository(db)
	seed := seedOverdueInvoice(t, repo)
	gw := &renewGateway{onCreate: func() { time.Sleep(50 * time.Millisecond) }}
	uc := newRenewInvoicePix(repo, invoice_repository.NewChargeRenewals(db),
		&stubUserRepo{user: &user.User{ID: seed.UserID, Username: "Tester", CPF: "12345678900"}},
		gw, nil, &recordingAlerter{}, func() time.Time { return renewNow })

	const parallel = 16
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		renewed    = map[string]int{}
		inProgress int
	)
	start := make(chan struct{})
	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := uc.Execute(context.Background(), invoice.RenewPixInput{WorkspaceID: seed.WorkspaceID, InvoiceID: seed.ID})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				renewed[got.ExternalID]++
			case errors.Is(err, invoice.ErrPixRenewInProgress):
				inProgress++
			default:
				t.Errorf("Execute: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if gw.createCount() != 1 {
		t.Fatalf("%d parallel renewals created %d charges, want 1", parallel, gw.createCount())
	}
	if len(renewed) != 1 || renewed["pay_old"] != 0 {
		t.Fatalf("every successful renewal must return the one new charge, got %v", renewed)
	}

	stored, err := repo.GetByID(seed.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if stored.ExternalID == "pay_old" || stored.Status != invoice.StatusOverdue {
		t.Fatalf("the invoice must carry the new charge and keep its status, got %+v", stored)
	}
	if stored.ChargeExpiresAt == nil {
		t.Fatal("the renewed charge must store its expiry")
	}
	var superseded int64
	if err := db.Model(&schema.InvoiceSupersededCharge{}).Where("invoice_id = ?", seed.ID).Count(&superseded).Error; err != nil {
		t.Fatalf("count superseded: %v", err)
	}
	if superseded != 1 {
		t.Fatalf("superseded charges = %d, want 1", superseded)
	}
	var lease sql.NullTime
	if err := db.Raw("SELECT renewal_lease_until FROM invoices WHERE id = ?", seed.ID).Row().Scan(&lease); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if lease.Valid {
		t.Fatalf("the lease must be released, got %v", lease.Time)
	}

	found, err := repo.GetBySupersededCharge(seed.Provider, "pay_old")
	if err != nil || found == nil || found.ID != seed.ID {
		t.Fatalf("the replaced charge must lead back to its invoice, got %v, %v", found, err)
	}
	if current, _ := repo.GetBySupersededCharge(seed.Provider, stored.ExternalID); current != nil {
		t.Fatal("the current charge is not superseded")
	}
}

func TestAnAbortedRenewalRestoresThePreviousChargeWhenItIsStillPayable(t *testing.T) {
	db := renewalDB(t)
	repo := invoice_repository.NewRepository(db)
	renewals := invoice_repository.NewChargeRenewals(db)
	seed := seedOverdueInvoice(t, repo)
	previous := invoice.SupersededCharge{Provider: seed.Provider, ExternalID: "pay_old"}

	acquired, err := renewals.BeginRenewal(seed.ID, previous, renewNow.Add(invoice.ChargeRenewalLease), renewNow)
	if err != nil || !acquired {
		t.Fatalf("BeginRenewal: acquired=%v err=%v", acquired, err)
	}
	if again, _ := renewals.BeginRenewal(seed.ID, previous, renewNow.Add(invoice.ChargeRenewalLease), renewNow); again {
		t.Fatal("a held lease must refuse a second renewal")
	}
	if err := renewals.AbortRenewal(seed.ID, previous, true); err != nil {
		t.Fatalf("AbortRenewal: %v", err)
	}
	if found, _ := repo.GetBySupersededCharge(seed.Provider, "pay_old"); found != nil {
		t.Fatal("a charge that is still payable must not stay superseded")
	}
	if retried, _ := renewals.BeginRenewal(seed.ID, previous, renewNow.Add(invoice.ChargeRenewalLease), renewNow); !retried {
		t.Fatal("an aborted renewal must release its lease")
	}
}

func TestAnExpiredLeaseCanBeTakenOver(t *testing.T) {
	db := renewalDB(t)
	repo := invoice_repository.NewRepository(db)
	renewals := invoice_repository.NewChargeRenewals(db)
	seed := seedOverdueInvoice(t, repo)
	previous := invoice.SupersededCharge{Provider: seed.Provider, ExternalID: "pay_old"}

	if ok, _ := renewals.BeginRenewal(seed.ID, previous, renewNow.Add(invoice.ChargeRenewalLease), renewNow); !ok {
		t.Fatal("first renewal must acquire the lease")
	}
	later := renewNow.Add(invoice.ChargeRenewalLease + time.Second)
	if ok, err := renewals.BeginRenewal(seed.ID, previous, later.Add(invoice.ChargeRenewalLease), later); err != nil || !ok {
		t.Fatalf("a crashed renewal must not lock the invoice forever: ok=%v err=%v", ok, err)
	}
}

func TestASettledInvoiceCannotBeginARenewal(t *testing.T) {
	db := renewalDB(t)
	repo := invoice_repository.NewRepository(db)
	renewals := invoice_repository.NewChargeRenewals(db)
	seed := seedOverdueInvoice(t, repo)
	if _, err := repo.MarkPaid(seed.ID, 1); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	ok, err := renewals.BeginRenewal(seed.ID, invoice.SupersededCharge{Provider: seed.Provider, ExternalID: "pay_old"},
		renewNow.Add(invoice.ChargeRenewalLease), renewNow)
	if err != nil || ok {
		t.Fatalf("a paid invoice must refuse the lease: ok=%v err=%v", ok, err)
	}
	attached, err := renewals.CompleteRenewal(seed.ID, "pay_old", invoice.ChargeAttachment{ExternalID: "pay_late"})
	if err != nil || attached {
		t.Fatalf("a paid invoice must never take a new charge: attached=%v err=%v", attached, err)
	}
}
