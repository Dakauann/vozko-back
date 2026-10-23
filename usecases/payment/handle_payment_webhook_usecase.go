package payment_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"

	"vozko/brand"
	"vozko/domain/affiliate"
	"vozko/domain/balance"
	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/notification"
	"vozko/domain/payment"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type handlePaymentWebhookUseCase struct {
	invoiceRepo           invoice.Repository
	activateSubscription  workspace_plan.SubscribeWorkspaceUseCase
	creditBalance         balance.CreditBalanceUseCase
	debitBalance          balance.DebitBalanceUseCase
	recordEarning         affiliate.RecordEarningUseCase
	notifications         notification.Publisher
	dashboardURL          string
	confirmMonthlyBilling billing.ConfirmMonthlyBillingUseCase
	reversals             *reversalDeps
}

func NewHandlePaymentWebhookUseCase(
	invoiceRepo invoice.Repository,
	activateSubscription workspace_plan.SubscribeWorkspaceUseCase,
	creditBalance balance.CreditBalanceUseCase,
	debitBalance balance.DebitBalanceUseCase,
	recordEarning affiliate.RecordEarningUseCase,
) *handlePaymentWebhookUseCase {
	return &handlePaymentWebhookUseCase{
		invoiceRepo:          invoiceRepo,
		activateSubscription: activateSubscription,
		creditBalance:        creditBalance,
		debitBalance:         debitBalance,
		recordEarning:        recordEarning,
	}
}

var _ payment.HandlePaymentWebhookUseCase = (*handlePaymentWebhookUseCase)(nil)

func (uc *handlePaymentWebhookUseCase) WithNotifications(publisher notification.Publisher, dashboardURL string) *handlePaymentWebhookUseCase {
	uc.notifications = publisher
	uc.dashboardURL = strings.TrimRight(dashboardURL, "/")
	return uc
}

func (uc *handlePaymentWebhookUseCase) WithMonthlyBilling(confirmer billing.ConfirmMonthlyBillingUseCase) *handlePaymentWebhookUseCase {
	uc.confirmMonthlyBilling = confirmer
	return uc
}

const balancePath = "/dashboard/balance"

func (uc *handlePaymentWebhookUseCase) notifyTopUpConfirmed(inv *invoice.Invoice, amountUSDMicros int64) {
	if uc.notifications == nil {
		return
	}
	amount := shared.FormatBRL(inv.AmountBRL)
	_, err := uc.notifications.Publish(context.Background(), notification.Draft{
		WorkspaceID: inv.WorkspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeveritySuccess,
		Title:       "Saldo adicionado",
		Body:        "Recebemos o pagamento e " + amount + " já estão disponíveis na carteira.",
		ActionURL:   balancePath,
		ActionLabel: "Ver saldo",
		Audience:    notification.Holding(workspace.ResourceBalance, workspace.ActionRead),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: "wallet_topup_confirmed.html",
			Subject:  "Saldo adicionado - " + brand.Active().Name,
			Placeholders: map[string]interface{}{
				"Amount":       amount,
				"DashboardURL": notification.AbsoluteLink(uc.dashboardURL, balancePath),
			},
		},
		DedupKey: "wallet_topup:" + inv.ID,
		Source:   notification.SourceTopUp,
	})
	if err != nil {
		log.Printf("[invoice-webhook] balance added notification for invoice %s failed: %v", inv.ID, err)
	}
}

func (uc *handlePaymentWebhookUseCase) Execute(event *payment.WebhookEvent) error {
	if event == nil || event.Payment.ID == "" {
		return nil
	}
	meaning, known := payment.MeaningOf(event.Event)
	if !known {
		uc.alert("Unmapped payment event", fmt.Sprintf("provider=%s event=%q charge=%s", event.Provider, event.Event, event.Payment.ID))
		return fmt.Errorf("%w: %s %q", payment.ErrUnmappedEvent, event.Provider, event.Event)
	}
	if meaning == payment.MeaningInformational {
		return nil
	}

	inv, err := uc.findInvoice(event)
	if err != nil || inv == nil {
		return err
	}

	switch meaning {
	case payment.MeaningSettled:
		if inv.Status == invoice.StatusReversed {
			return uc.releaseReversal(inv)
		}
		return uc.settle(inv)
	case payment.MeaningOverdue:
		return uc.moveUnsettled(inv, invoice.StatusOverdue)
	case payment.MeaningCancelled:
		return uc.moveUnsettled(inv, invoice.StatusCancelled)
	case payment.MeaningReversalOpened, payment.MeaningReversalFinal:
		return uc.reverse(inv, event)
	case payment.MeaningReversalReleased:
		return uc.releaseReversal(inv)
	default:
		log.Printf("[invoice-webhook] %s for invoice %s moves no money", event.Event, inv.ID)
		return nil
	}
}

func (uc *handlePaymentWebhookUseCase) findInvoice(event *payment.WebhookEvent) (*invoice.Invoice, error) {
	inv, err := uc.invoiceRepo.GetByExternalID(event.Provider, event.Payment.ID)
	if err != nil {
		return nil, fmt.Errorf("invoice lookup failed: %w", err)
	}
	if inv != nil {
		return inv, nil
	}

	invoiceID, ours := invoice.IDFromExternalReference(event.Payment.ExternalReference)
	if !ours {
		return nil, nil
	}
	inv, err = uc.invoiceRepo.GetByID(invoiceID)
	if errors.Is(err, invoice.ErrInvoiceNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invoice lookup by reference failed: %w", err)
	}
	if inv.Provider != "" && event.Provider != "" && inv.Provider != event.Provider {
		return nil, nil
	}
	if !inv.Reserved() {
		uc.alert("Payment event for a second charge of one invoice",
			fmt.Sprintf("invoice=%s attached=%s event=%s charge=%s provider=%s", inv.ID, inv.ExternalID, event.Event, event.Payment.ID, event.Provider))
		return nil, nil
	}
	if err := uc.invoiceRepo.AttachCharge(inv.ID, invoice.ChargeAttachment{ExternalID: event.Payment.ID}); err != nil {
		return nil, fmt.Errorf("attach charge %s to reserved invoice %s: %w", event.Payment.ID, inv.ID, err)
	}
	inv.ExternalID = event.Payment.ID
	return inv, nil
}

func (uc *handlePaymentWebhookUseCase) moveUnsettled(inv *invoice.Invoice, status invoice.Status) error {
	if isSettled(inv.Status) {
		return nil
	}
	return uc.invoiceRepo.UpdateStatus(inv.ID, status)
}

func isSettled(status invoice.Status) bool {
	for _, settled := range invoice.SettledStatuses() {
		if string(status) == settled {
			return true
		}
	}
	return false
}

func (uc *handlePaymentWebhookUseCase) settle(inv *invoice.Invoice) error {
	switch inv.NormalizedPurpose() {
	case invoice.PurposeTopUp:
		return uc.settleTopUp(inv)
	case invoice.PurposeSubscription:
		return uc.settleSubscription(inv)
	case invoice.PurposeMonthlyBilling:
		return uc.settleMonthlyBilling(inv)
	default:
		uc.alert("Paid invoice with an unknown purpose", fmt.Sprintf("invoice=%s purpose=%q workspace=%s", inv.ID, inv.Purpose, inv.WorkspaceID))
		return fmt.Errorf("%w: invoice %s purpose %q", invoice.ErrUnknownPurpose, inv.ID, inv.Purpose)
	}
}

func monthlyBillingCreditableUSD(inv *invoice.Invoice) int64 {
	if inv.CreditableUSD > 0 {
		return inv.CreditableUSD
	}
	return inv.AmountUSD
}

func (uc *handlePaymentWebhookUseCase) settleMonthlyBilling(inv *invoice.Invoice) error {
	transitioned, err := uc.invoiceRepo.MarkPaid(inv.ID, inv.AmountUSD)
	if err != nil {
		return fmt.Errorf("failed to mark monthly invoice paid: %w", err)
	}
	if !transitioned {
		return nil
	}

	if creditable := monthlyBillingCreditableUSD(inv); creditable > 0 {
		refID := inv.ID
		if _, creditErr := uc.creditBalance.Execute(balance.CreditBalanceInput{
			WorkspaceID:        inv.WorkspaceID,
			Amount:             creditable,
			ServiceType:        balance.ServiceTopUp,
			ReferenceID:        &refID,
			Description:        fmt.Sprintf("Cobrança mensal: R$ %.2f → $%.4f", inv.AmountBRL, float64(creditable)/1_000_000),
			ExchangeRateMicros: invoiceRateMicros(inv),
		}); creditErr != nil {
			_ = uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusPending)
			return fmt.Errorf("failed to credit balance for monthly invoice %s: %w", inv.ID, creditErr)
		}
	}

	if uc.confirmMonthlyBilling != nil {
		if err := uc.confirmMonthlyBilling.Execute(inv.WorkspaceID); err != nil {
			log.Printf("[invoice-webhook] WARNING: monthly invoice %s paid + credited but subscription extension failed for workspace %s: %v", inv.ID, inv.WorkspaceID, err)
		}
	}

	uc.recordAffiliateEarning(inv, "MONTHLY_BILLING")
	return nil
}

func (uc *handlePaymentWebhookUseCase) settleTopUp(inv *invoice.Invoice) error {
	amountUSD := inv.AmountUSD
	if amountUSD <= 0 {
		log.Printf("[invoice-webhook] amountUSD <= 0 for invoice %s (BRL %.2f, rate %.2f)", inv.ID, inv.AmountBRL, inv.ExchangeRate)
		return nil
	}

	transitioned, err := uc.invoiceRepo.MarkPaid(inv.ID, amountUSD)
	if err != nil {
		return fmt.Errorf("failed to mark invoice paid: %w", err)
	}
	if !transitioned {
		return nil
	}

	refID := inv.ID
	_, err = uc.creditBalance.Execute(balance.CreditBalanceInput{
		WorkspaceID:        inv.WorkspaceID,
		Amount:             amountUSD,
		ServiceType:        balance.ServiceTopUp,
		ReferenceID:        &refID,
		Description:        fmt.Sprintf("Recarga: R$ %.2f → $%.4f", inv.AmountBRL, float64(amountUSD)/1_000_000),
		ExchangeRateMicros: invoiceRateMicros(inv),
	})
	if err != nil {
		_ = uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusPending)
		return fmt.Errorf("failed to credit balance for invoice %s: %w", inv.ID, err)
	}

	log.Printf("[invoice-webhook] credited %d USD micros to workspace %s for invoice %s", amountUSD, inv.WorkspaceID, inv.ID)

	uc.notifyTopUpConfirmed(inv, amountUSD)

	uc.recordAffiliateEarning(inv, "TOP_UP")
	return nil
}

func (uc *handlePaymentWebhookUseCase) settleSubscription(inv *invoice.Invoice) error {
	transitioned, err := uc.invoiceRepo.MarkPaid(inv.ID, inv.AmountUSD)
	if err != nil {
		return fmt.Errorf("failed to mark subscription invoice paid: %w", err)
	}
	if !transitioned {
		return nil
	}

	if inv.PlanDefinitionID == nil || strings.TrimSpace(*inv.PlanDefinitionID) == "" {
		_ = uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusPending)
		return fmt.Errorf("subscription invoice %s is missing a plan definition", inv.ID)
	}
	if uc.activateSubscription == nil {
		_ = uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusPending)
		return fmt.Errorf("subscription activation use case is not configured")
	}

	billingCycle := workspace_plan.BillingCycle(inv.BillingCycle)
	if !billingCycle.IsValid() {
		billingCycle = workspace_plan.BillingCycleMonthly
	}

	_, err = uc.activateSubscription.Execute(inv.WorkspaceID, strings.TrimSpace(*inv.PlanDefinitionID), billingCycle)
	if err != nil {
		_ = uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusPending)
		return fmt.Errorf("failed to activate subscription for invoice %s: %w", inv.ID, err)
	}

	log.Printf("[invoice-webhook] activated workspace subscription for workspace %s via invoice %s", inv.WorkspaceID, inv.ID)

	amountUSD := inv.AmountUSD
	if amountUSD > 0 {
		refID := inv.ID
		_, creditErr := uc.creditBalance.Execute(balance.CreditBalanceInput{
			WorkspaceID:        inv.WorkspaceID,
			Amount:             amountUSD,
			ServiceType:        balance.ServiceTopUp,
			ReferenceID:        &refID,
			Description:        fmt.Sprintf("Assinatura: R$ %.2f → $%.4f", inv.AmountBRL, float64(amountUSD)/1_000_000),
			ExchangeRateMicros: invoiceRateMicros(inv),
		})
		if creditErr != nil {
			log.Printf("[invoice-webhook] WARNING: subscription activated but balance credit failed for invoice %s: %v", inv.ID, creditErr)
			return fmt.Errorf("failed to credit balance for subscription invoice %s: %w", inv.ID, creditErr)
		}
		log.Printf("[invoice-webhook] credited %d USD micros to workspace %s for subscription invoice %s", amountUSD, inv.WorkspaceID, inv.ID)
	}

	uc.recordAffiliateEarning(inv, "SUBSCRIPTION")
	return nil
}

func (uc *handlePaymentWebhookUseCase) recordAffiliateEarning(inv *invoice.Invoice, purpose string) {
	if uc.recordEarning == nil || inv == nil {
		return
	}
	_, err := uc.recordEarning.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID:        inv.WorkspaceID,
		InvoiceID:          inv.ID,
		AmountUSDMicros:    inv.AmountUSD,
		ExchangeRateMicros: invoiceRateMicros(inv),
		Purpose:            purpose,
	})
	if err != nil {
		log.Printf("[invoice-webhook] WARNING: failed to record affiliate earning for invoice %s: %v", inv.ID, err)
	}
}

func invoiceRateMicros(inv *invoice.Invoice) int64 {
	if inv == nil || inv.ExchangeRate <= 0 {
		return 0
	}
	return int64(math.Round(inv.ExchangeRate * 1_000_000))
}
