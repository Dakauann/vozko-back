package payment_usecase

import (
	"context"
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
	return uc.handleInvoicePayment(event, strings.ToUpper(event.Event))
}

func (uc *handlePaymentWebhookUseCase) handleInvoicePayment(event *payment.WebhookEvent, normalizedEvent string) error {
	inv, err := uc.invoiceRepo.GetByExternalID(event.Payment.ID)
	if err != nil {
		return fmt.Errorf("invoice lookup failed: %w", err)
	}
	if inv == nil {
		return nil
	}

	providerInvoiceNumber := strings.TrimSpace(event.Payment.InvoiceNumber)

	switch inv.NormalizedPurpose() {
	case invoice.PurposeSubscription:
		return uc.handleSubscriptionInvoicePayment(inv, normalizedEvent, providerInvoiceNumber)
	case invoice.PurposeMonthlyBilling:
		return uc.handleMonthlyBillingInvoicePayment(inv, normalizedEvent, providerInvoiceNumber)
	default:
		return uc.handleTopUpInvoicePayment(inv, normalizedEvent, providerInvoiceNumber)
	}
}

func monthlyBillingCreditableUSD(inv *invoice.Invoice) int64 {
	if inv.CreditableUSD > 0 {
		return inv.CreditableUSD
	}
	return inv.AmountUSD
}

func (uc *handlePaymentWebhookUseCase) handleMonthlyBillingInvoicePayment(inv *invoice.Invoice, normalizedEvent, providerInvoiceNumber string) error {
	switch normalizedEvent {
	case "PAYMENT_RECEIVED", "PAYMENT_RECEIVED_IN_CASH":
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

	case "PAYMENT_OVERDUE":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusOverdue); err != nil {
			return err
		}

	case "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED", "PAYMENT_CHARGEBACK":
		if creditable := monthlyBillingCreditableUSD(inv); inv.Status == invoice.StatusPaid && creditable > 0 {
			refID := inv.ID
			if _, err := uc.debitBalance.Execute(balance.DebitBalanceInput{
				WorkspaceID:        inv.WorkspaceID,
				Amount:             creditable,
				ServiceType:        balance.ServiceTopUp,
				ReferenceID:        &refID,
				Description:        fmt.Sprintf("Reembolso cobrança mensal: R$ %.2f → -$%.4f", inv.AmountBRL, float64(creditable)/1_000_000),
				IsRefund:           true,
				ExchangeRateMicros: invoiceRateMicros(inv),
			}); err != nil {
				log.Printf("[invoice-webhook] WARNING: failed to debit balance for refunded monthly invoice %s: %v", inv.ID, err)
			}
		}
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusRefunded); err != nil {
			return err
		}

	case "PAYMENT_DELETED", "PAYMENT_REJECTED":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusCancelled); err != nil {
			return err
		}
	}
	return nil
}

func (uc *handlePaymentWebhookUseCase) handleTopUpInvoicePayment(inv *invoice.Invoice, normalizedEvent, providerInvoiceNumber string) error {

	switch normalizedEvent {
	case "PAYMENT_RECEIVED", "PAYMENT_RECEIVED_IN_CASH":

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

	case "PAYMENT_OVERDUE":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusOverdue); err != nil {
			return err
		}

	case "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED", "PAYMENT_CHARGEBACK":

		if inv.Status == invoice.StatusPaid && inv.AmountUSD > 0 {
			refID := inv.ID
			_, err := uc.debitBalance.Execute(balance.DebitBalanceInput{
				WorkspaceID:        inv.WorkspaceID,
				Amount:             inv.AmountUSD,
				ServiceType:        balance.ServiceTopUp,
				ReferenceID:        &refID,
				Description:        fmt.Sprintf("Reembolso: R$ %.2f → -$%.4f", inv.AmountBRL, float64(inv.AmountUSD)/1_000_000),
				IsRefund:           true,
				ExchangeRateMicros: invoiceRateMicros(inv),
			})
			if err != nil {
				log.Printf("[invoice-webhook] WARNING: failed to debit balance for refunded invoice %s: %v", inv.ID, err)
			}
		}
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusRefunded); err != nil {
			return err
		}

	case "PAYMENT_DELETED", "PAYMENT_REJECTED":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusCancelled); err != nil {
			return err
		}

	default:

	}

	return nil
}

func (uc *handlePaymentWebhookUseCase) handleSubscriptionInvoicePayment(inv *invoice.Invoice, normalizedEvent, providerInvoiceNumber string) error {
	switch normalizedEvent {
	case "PAYMENT_RECEIVED", "PAYMENT_RECEIVED_IN_CASH":
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

	case "PAYMENT_OVERDUE":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusOverdue); err != nil {
			return err
		}

	case "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED", "PAYMENT_CHARGEBACK":

		if inv.Status == invoice.StatusPaid && inv.AmountUSD > 0 {
			refID := inv.ID
			_, err := uc.debitBalance.Execute(balance.DebitBalanceInput{
				WorkspaceID:        inv.WorkspaceID,
				Amount:             inv.AmountUSD,
				ServiceType:        balance.ServiceTopUp,
				ReferenceID:        &refID,
				Description:        fmt.Sprintf("Reembolso assinatura: R$ %.2f → -$%.4f", inv.AmountBRL, float64(inv.AmountUSD)/1_000_000),
				IsRefund:           true,
				ExchangeRateMicros: invoiceRateMicros(inv),
			})
			if err != nil {
				log.Printf("[invoice-webhook] WARNING: failed to debit balance for refunded subscription invoice %s: %v", inv.ID, err)
			}
		}
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusRefunded); err != nil {
			return err
		}

	case "PAYMENT_DELETED", "PAYMENT_REJECTED":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusCancelled); err != nil {
			return err
		}
	}

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
