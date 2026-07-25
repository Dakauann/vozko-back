package invoice_usecase

import (
	"testing"

	"vozko/domain/invoice"
	"vozko/domain/user"
)

// The stub pricing repo fixes the exchange rate at 6.0 BRL/USD, so amountUSD = round(BRL/6 * 1e6).
// These tests pin the CreditableUSD (saldo portion) rule, the most money-sensitive part of the
// unified monthly charge: only the plan portion of a MONTHLY_BILLING invoice may become saldo, the
// channel-license repasse must not, and a caller error must never credit more than was charged.

func newCreditableUC(repo *stubInvoiceRepo, asaasSvc *stubAsaasService) invoice.CreateInvoiceUseCase {
	return NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Tester", CPF: "12345678900"}},
		asaasSvc,
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
}

func TestCreditableUSD_MonthlyBillingCreditsOnlyPlanPortion(t *testing.T) {
	repo := &stubInvoiceRepo{}
	asaasSvc := &stubAsaasService{}
	uc := newCreditableUC(repo, asaasSvc)

	// Plan R$1.099 + two channels R$150 each = R$1.399 total; only the plan becomes saldo.
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:   "ws-1",
		UserID:        "user-1",
		Purpose:       invoice.PurposeMonthlyBilling,
		AmountBRL:     1399,
		CreditableBRL: 1099,
		BillingType:   "PIX",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if out.Invoice.AmountUSD != 233_166_667 {
		t.Fatalf("AmountUSD = %d, want 233_166_667 (1399/6)", out.Invoice.AmountUSD)
	}
	if out.Invoice.CreditableUSD != 183_166_667 {
		t.Fatalf("CreditableUSD = %d, want 183_166_667 (only the 1099 plan portion)", out.Invoice.CreditableUSD)
	}
	if out.Invoice.CreditableUSD >= out.Invoice.AmountUSD {
		t.Fatal("channel portion must be excluded: CreditableUSD must be strictly less than AmountUSD here")
	}
	if asaasSvc.createCalls != 1 || repo.created == nil {
		t.Fatalf("expected exactly one Asaas charge and a persisted invoice, calls=%d created=%v", asaasSvc.createCalls, repo.created)
	}
	if repo.created.NormalizedPurpose() != invoice.PurposeMonthlyBilling {
		t.Fatalf("persisted purpose = %q, want MONTHLY_BILLING", repo.created.Purpose)
	}
}

func TestCreditableUSD_MonthlyBillingZeroCreditsNothing(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := newCreditableUC(repo, &stubAsaasService{})

	// A channels-only charge (no plan portion) credits no saldo.
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 300, CreditableBRL: 0, BillingType: "PIX",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if out.Invoice.CreditableUSD != 0 {
		t.Fatalf("CreditableUSD = %d, want 0 for a channels-only charge", out.Invoice.CreditableUSD)
	}
	if out.Invoice.AmountUSD != 50_000_000 {
		t.Fatalf("AmountUSD = %d, want 50_000_000 (300/6)", out.Invoice.AmountUSD)
	}
}

func TestCreditableUSD_MonthlyBillingClampsOvercredit(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := newCreditableUC(repo, &stubAsaasService{})

	// A caller bug (plan portion > total) must never credit more saldo than was charged.
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 100, CreditableBRL: 200, BillingType: "PIX",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if out.Invoice.CreditableUSD != out.Invoice.AmountUSD {
		t.Fatalf("CreditableUSD = %d, want it clamped to AmountUSD %d", out.Invoice.CreditableUSD, out.Invoice.AmountUSD)
	}
}

func TestCreditableUSD_MonthlyBillingClampsNegative(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := newCreditableUC(repo, &stubAsaasService{})

	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 100, CreditableBRL: -50, BillingType: "PIX",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if out.Invoice.CreditableUSD != 0 {
		t.Fatalf("CreditableUSD = %d, want 0 (negative plan portion clamped)", out.Invoice.CreditableUSD)
	}
}

func TestCreditableUSD_MonthlyBillingMatchesAmountWhenAllPlan(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := newCreditableUC(repo, &stubAsaasService{})

	// All-plan, no channels: the saldo portion equals the total, at the same exchange rate.
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 600, CreditableBRL: 600, BillingType: "PIX",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if out.Invoice.CreditableUSD != out.Invoice.AmountUSD || out.Invoice.CreditableUSD != 100_000_000 {
		t.Fatalf("CreditableUSD = %d, AmountUSD = %d, want both 100_000_000",
			out.Invoice.CreditableUSD, out.Invoice.AmountUSD)
	}
}

func TestCreditableUSD_NonMonthlyPurposesCreditFullAmount(t *testing.T) {
	cases := []struct {
		name    string
		purpose invoice.Purpose
		planID  string
	}{
		{"top_up ignores stray CreditableBRL", invoice.PurposeTopUp, ""},
		{"subscription credits the full amount", invoice.PurposeSubscription, "plan-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubInvoiceRepo{}
			uc := newCreditableUC(repo, &stubAsaasService{})

			// A stray CreditableBRL must not reduce the credit for non-monthly purposes.
			out, err := uc.Execute(invoice.CreateInvoiceInput{
				WorkspaceID: "ws-1", UserID: "user-1",
				Purpose: tc.purpose, PlanDefinitionID: tc.planID,
				AmountBRL: 60, CreditableBRL: 10, BillingType: "PIX",
			})
			if err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			if out.Invoice.CreditableUSD != out.Invoice.AmountUSD {
				t.Fatalf("%s: CreditableUSD = %d, want full AmountUSD %d",
					tc.name, out.Invoice.CreditableUSD, out.Invoice.AmountUSD)
			}
			if out.Invoice.AmountUSD != 10_000_000 {
				t.Fatalf("%s: AmountUSD = %d, want 10_000_000 (60/6)", tc.name, out.Invoice.AmountUSD)
			}
		})
	}
}

func TestCreditableUSD_MonthlyBillingRoundsHalfAwayLikeAmount(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := newCreditableUC(repo, &stubAsaasService{})

	// 10/6 = 1.6666... -> rounds to 1_666_667 micros, identically for amount and creditable.
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 10, CreditableBRL: 10, BillingType: "PIX",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if out.Invoice.AmountUSD != 1_666_667 || out.Invoice.CreditableUSD != 1_666_667 {
		t.Fatalf("rounding mismatch: AmountUSD=%d CreditableUSD=%d, want both 1_666_667",
			out.Invoice.AmountUSD, out.Invoice.CreditableUSD)
	}
}
