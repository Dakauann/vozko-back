package invoice_repository

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func invoiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "invoice_reservation", &schema.Invoice{})
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

func reservation(key string) *invoice.Invoice {
	return &invoice.Invoice{
		ID: uuid.NewString(), WorkspaceID: uuid.NewString(), UserID: uuid.NewString(),
		Purpose: invoice.PurposeTopUp, AmountBRL: 50, AmountUSD: 10_000_000, CreditableUSD: 10_000_000,
		ExchangeRate: 5, Status: invoice.StatusPending, BillingType: "PIX",
		Provider: payment.ProviderAsaas, IdempotencyKey: key,
	}
}

func TestOneKeyReservesOneInvoiceUnderConcurrency(t *testing.T) {
	repo := NewRepository(invoiceDB(t))
	key := "client:ws:" + uuid.NewString()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		ids     = map[string]bool{}
	)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, isNew, err := repo.Reserve(reservation(key))
			if err != nil {
				t.Errorf("Reserve: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ids[got.ID] = true
			if isNew {
				created++
			}
		}()
	}
	wg.Wait()

	if created != 1 || len(ids) != 1 {
		t.Fatalf("20 concurrent reservations with one key created %d invoice(s) and returned %d id(s); want 1 and 1", created, len(ids))
	}
}

func TestReservationsWithoutAChargeDoNotCollide(t *testing.T) {
	repo := NewRepository(invoiceDB(t))
	for range 2 {
		if _, isNew, err := repo.Reserve(reservation("")); err != nil || !isNew {
			t.Fatalf("two reservations without a charge must both be saved: isNew=%v err=%v", isNew, err)
		}
	}
}

func TestAnAttachedChargeIsFoundByItsProvider(t *testing.T) {
	repo := NewRepository(invoiceDB(t))
	reserved, _, err := repo.Reserve(reservation(""))
	if err != nil {
		t.Fatal(err)
	}
	if !reserved.Reserved() {
		t.Fatal("a fresh reservation has no charge yet")
	}
	pix := "pix-copy"
	if err := repo.AttachCharge(reserved.ID, invoice.ChargeAttachment{ExternalID: "pay_1", PixCopy: &pix}); err != nil {
		t.Fatalf("AttachCharge: %v", err)
	}

	got, err := repo.GetByExternalID(payment.ProviderAsaas, "pay_1")
	if err != nil || got == nil || got.ID != reserved.ID || got.PixCopy == nil {
		t.Fatalf("GetByExternalID = %+v, %v", got, err)
	}
	if other, _ := repo.GetByExternalID(payment.ProviderMercadoPago, "pay_1"); other != nil {
		t.Fatal("a charge id from one provider must never match another provider's invoice")
	}
}

func TestAChargeCannotBeReattachedToAnotherCharge(t *testing.T) {
	repo := NewRepository(invoiceDB(t))
	reserved, _, _ := repo.Reserve(reservation(""))
	if err := repo.AttachCharge(reserved.ID, invoice.ChargeAttachment{ExternalID: "pay_1"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachCharge(reserved.ID, invoice.ChargeAttachment{ExternalID: "pay_1"}); err != nil {
		t.Fatalf("re-attaching the same charge is a harmless retry: %v", err)
	}
	if err := repo.AttachCharge(reserved.ID, invoice.ChargeAttachment{ExternalID: "pay_2"}); err == nil {
		t.Fatal("an invoice must never switch to a second charge")
	}
}

func TestExternalIdsAreUniquePerProvider(t *testing.T) {
	repo := NewRepository(invoiceDB(t))
	asaas := reservation("")
	mp := reservation("")
	mp.Provider = payment.ProviderMercadoPago
	for _, inv := range []*invoice.Invoice{asaas, mp} {
		if _, _, err := repo.Reserve(inv); err != nil {
			t.Fatal(err)
		}
		if err := repo.AttachCharge(inv.ID, invoice.ChargeAttachment{ExternalID: "123"}); err != nil {
			t.Fatalf("the same id at two providers is two different charges: %v", err)
		}
	}
	got, _ := repo.GetByExternalID(payment.ProviderMercadoPago, "123")
	if got == nil || got.ID != mp.ID {
		t.Fatalf("expected the Mercado Pago invoice, got %+v", got)
	}
}

func TestALegacyInvoiceWithoutAProviderIsStillFound(t *testing.T) {
	db := invoiceDB(t)
	repo := NewRepository(db)
	external := "pay_legacy"
	legacy := &schema.Invoice{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), UserID: uuid.NewString(),
		AmountBRL: 10, ExchangeRate: 5, Status: "PAID", BillingType: "PIX", ExternalID: &external}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByExternalID(payment.ProviderAsaas, external); got == nil || got.ID != legacy.ID {
		t.Fatalf("a row whose provider could not be inferred must still be found, got %+v", got)
	}
}

func TestStaleReservationsAreCancelled(t *testing.T) {
	db := invoiceDB(t)
	repo := NewRepository(db)
	stale, _, _ := repo.Reserve(reservation(""))
	fresh, _, _ := repo.Reserve(reservation(""))
	charged, _, _ := repo.Reserve(reservation(""))
	_ = repo.AttachCharge(charged.ID, invoice.ChargeAttachment{ExternalID: "pay_charged"})
	old := time.Now().Add(-time.Hour)
	db.Model(&schema.Invoice{}).Where("id IN ?", []string{stale.ID, charged.ID}).Update("created_at", old)

	n, err := repo.CancelStaleReservations(time.Now().Add(-invoice.ReservationStaleAfter))
	if err != nil || n != 1 {
		t.Fatalf("CancelStaleReservations = %d, %v; want exactly the stale one", n, err)
	}
	for id, want := range map[string]invoice.Status{stale.ID: invoice.StatusCancelled, fresh.ID: invoice.StatusPending, charged.ID: invoice.StatusPending} {
		got, _ := repo.GetByID(id)
		if got.Status != want {
			t.Errorf("invoice %s status = %s, want %s", id, got.Status, want)
		}
	}
}

func TestOpenObligationsCountWhatThePayersBankCanStillReverse(t *testing.T) {
	db := invoiceDB(t)
	repo := NewRepository(db)
	now := time.Now()
	seed := func(provider payment.Provider, billingType string, status invoice.Status, age time.Duration) {
		created := now.Add(-age)
		row := &schema.Invoice{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), UserID: uuid.NewString(),
			AmountBRL: 10, ExchangeRate: 5, Status: string(status), BillingType: billingType, Provider: string(provider),
			CreatedAt: created}
		if status == invoice.StatusPaid || status == invoice.StatusReversed {
			paid := created.Unix()
			row.PaidAt = &paid
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	day := 24 * time.Hour
	seed(payment.ProviderAsaas, "PIX", invoice.StatusPaid, 10*day)
	seed(payment.ProviderAsaas, "PIX", invoice.StatusPaid, 100*day)
	seed(payment.ProviderAsaas, "BOLETO", invoice.StatusPaid, day)
	seed(payment.ProviderAsaas, "CREDIT_CARD", invoice.StatusReversed, 100*day)
	seed(payment.ProviderMercadoPago, "PIX", invoice.StatusPending, 5*day)
	seed(payment.ProviderMercadoPago, "PIX", invoice.StatusPending, 90*day)
	seed(payment.ProviderMercadoPago, "PIX", invoice.StatusCancelled, day)
	seed("", "PIX", invoice.StatusPaid, day)

	got, err := repo.OpenObligations(now)
	if err != nil {
		t.Fatal(err)
	}
	byProvider := map[payment.Provider]payment.Obligations{}
	for _, o := range got {
		byProvider[o.Provider] = o
	}
	if a := byProvider[payment.ProviderAsaas]; a.Unsettled != 0 || a.Reversible != 2 {
		t.Errorf("asaas = %+v, want 0 unsettled and 2 reversible (recent PIX, card inside 180 days)", a)
	}
	if m := byProvider[payment.ProviderMercadoPago]; m.Unsettled != 1 || m.Reversible != 0 {
		t.Errorf("mercadopago = %+v, want 1 recent unsettled", m)
	}
	if _, ok := byProvider[""]; ok {
		t.Error("rows without a known provider cannot name credentials and must not be reported as a provider")
	}
}
