package payment_usecase

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/affiliate"
	"vozko/domain/balance"
	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/notification"
	"vozko/domain/order"
	"vozko/domain/payment"
	"vozko/domain/ticket"
	"vozko/domain/user"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type handleAsaasWebhookUseCase struct {
	paymentRepo           payment.PaymentRepository
	orderRepo             order.OrderRepository
	emailService          notification.EmailService
	userRepo              user.UserRepository
	createTicket          ticket.CreateTicketUseCase
	invoiceRepo           invoice.Repository
	activateSubscription  workspace_plan.SubscribeWorkspaceUseCase
	creditBalance         balance.CreditBalanceUseCase
	debitBalance          balance.DebitBalanceUseCase
	recordEarning         affiliate.RecordEarningUseCase
	notifier              notification.Notifier
	dashboardURL          string
	confirmMonthlyBilling billing.ConfirmMonthlyBillingUseCase
}

func NewHandleAsaasWebhookUseCase(
	paymentRepo payment.PaymentRepository,
	orderRepo order.OrderRepository,
	emailService notification.EmailService,
	userRepo user.UserRepository,
	createTicket ticket.CreateTicketUseCase,
	invoiceRepo invoice.Repository,
	activateSubscription workspace_plan.SubscribeWorkspaceUseCase,
	creditBalance balance.CreditBalanceUseCase,
	debitBalance balance.DebitBalanceUseCase,
	recordEarning affiliate.RecordEarningUseCase,
) *handleAsaasWebhookUseCase {
	return &handleAsaasWebhookUseCase{
		paymentRepo:          paymentRepo,
		orderRepo:            orderRepo,
		emailService:         emailService,
		userRepo:             userRepo,
		createTicket:         createTicket,
		invoiceRepo:          invoiceRepo,
		activateSubscription: activateSubscription,
		creditBalance:        creditBalance,
		debitBalance:         debitBalance,
		recordEarning:        recordEarning,
	}
}

var _ payment.HandleAsaasWebhookUseCase = (*handleAsaasWebhookUseCase)(nil)

// WithNotifier enables the "wallet top-up confirmed" email. Returns the use case
// for chaining at wiring time.
func (uc *handleAsaasWebhookUseCase) WithNotifier(n notification.Notifier, dashboardURL string) *handleAsaasWebhookUseCase {
	uc.notifier = n
	uc.dashboardURL = dashboardURL
	return uc
}

// WithMonthlyBilling enables extending plan and addon subscription periods when a unified
// MONTHLY_BILLING invoice is confirmed paid. Returns the use case for chaining at wiring time.
func (uc *handleAsaasWebhookUseCase) WithMonthlyBilling(confirmer billing.ConfirmMonthlyBillingUseCase) *handleAsaasWebhookUseCase {
	uc.confirmMonthlyBilling = confirmer
	return uc
}

func (uc *handleAsaasWebhookUseCase) notifyTopUpConfirmed(inv *invoice.Invoice, amountUSDMicros int64) {
	if uc.notifier == nil {
		return
	}
	_ = uc.notifier.Notify(notification.Notification{
		WorkspaceID: inv.WorkspaceID,
		Subject:     "Saldo adicionado - " + brand.Active().Name,
		Template:    "wallet_topup_confirmed.html",
		Placeholders: map[string]interface{}{
			"Amount":       fmt.Sprintf("US$ %.2f", float64(amountUSDMicros)/1e6),
			"DashboardURL": uc.dashboardURL,
		},
		DedupKey: "wallet_topup:" + inv.ID,
		DedupTTL: 30 * 24 * time.Hour,
	})
}

func (uc *handleAsaasWebhookUseCase) Execute(event *payment.AsaasWebhookEvent) error {
	if event == nil || event.Payment.ID == "" {
		return nil
	}

	normalizedEvent := strings.ToUpper(event.Event)

	if uc.isInvoicePayment(event.Payment.ID) {
		return uc.handleInvoicePayment(event, normalizedEvent)
	}

	paymentStatus, orderStatus := mapAsaasEventToStatuses(normalizedEvent)

	orderID := ""
	shouldSendPaymentEmail := false
	paymentAmount := event.Payment.Value
	paymentMethod := event.Payment.BillingType

	if event.Payment.ExternalReference != "" {
		existingPayment, err := uc.paymentRepo.GetPaymentByID(event.Payment.ExternalReference)
		if err != nil {
			return err
		}
		if existingPayment == nil {
			return payment.ErrPaymentNotFound
		}

		if paymentStatus != nil {
			if *paymentStatus == payment.StatusReceived || *paymentStatus == payment.StatusReceivedInCash {
				if existingPayment.Status != *paymentStatus {
					shouldSendPaymentEmail = true
					if paymentAmount == 0 {
						paymentAmount = existingPayment.Amount
					}
					if paymentMethod == "" {
						paymentMethod = string(existingPayment.BillingType)
					}
				}
			}

			if err := uc.paymentRepo.UpdatePaymentStatus(event.Payment.ExternalReference, *paymentStatus); err != nil {
				return err
			}
		}

		if existingPayment.OrderID != nil {
			orderID = *existingPayment.OrderID
		}
	}

	if orderStatus != nil && orderID != "" {
		if err := uc.orderRepo.UpdateStatus(orderID, *orderStatus); err != nil {
			return err
		}

		if *orderStatus == order.StatusPaid && uc.createTicket != nil {
			orderData, err := uc.orderRepo.GetByIDForSystem(orderID)
			if err != nil {
				return err
			}
			if orderData != nil {
				if _, err := uc.createTicket.Execute(orderID, orderData.UserID); err != nil && err != ticket.ErrTicketAlreadyExists {
					return err
				}
			}
		}
	}

	if shouldSendPaymentEmail && orderID != "" {
		orderData, err := uc.orderRepo.GetByIDForSystem(orderID)
		if err != nil {
			return err
		}

		go uc.sendPaymentConfirmationEmail(orderData, paymentAmount, paymentMethod)
	}

	return nil
}

func (uc *handleAsaasWebhookUseCase) sendPaymentConfirmationEmail(ord *order.Order, amount float64, billingType string) {
	if ord == nil || uc.emailService == nil {
		return
	}

	email := uc.getUserEmail(ord.UserID)
	if email == "" {
		email = ord.CustomerName
	}
	if email == "" {
		return
	}

	amountStr := fmt.Sprintf("%.2f", amount)
	method := formatPaymentMethod(billingType)
	paymentDate := time.Now().Format("02/01/2006 15:04")

	subject := "Pagamento Confirmado - " + brand.Active().Name
	if err := uc.emailService.SendTemplate(email, subject, "payment_confirmation.html", map[string]interface{}{
		"CustomerName":  ord.CustomerName,
		"OrderID":       ord.ID,
		"Amount":        amountStr,
		"PaymentMethod": method,
		"PaymentDate":   paymentDate,
	}); err != nil {
		return
	}
}

func (uc *handleAsaasWebhookUseCase) getUserEmail(userID string) string {
	if userID == "" || uc.userRepo == nil {
		return ""
	}

	u, err := uc.userRepo.FindByID(userID)
	if err != nil {
		return ""
	}

	return u.Email
}

func formatPaymentMethod(method string) string {
	if method == "" {
		return "PIX"
	}

	switch strings.ToUpper(method) {
	case "PIX":
		return "PIX"
	case "BOLETO":
		return "Boleto"
	case "CREDIT_CARD":
		return "Cartao de Credito"
	default:
		return strings.ToUpper(method)
	}
}

func mapAsaasEventToStatuses(event string) (*payment.Status, *order.Status) {
	var payStatus *payment.Status
	var ordStatus *order.Status

	switch event {
	case "PAYMENT_CREATED":
		ps := payment.StatusPending
		os := order.StatusPending
		payStatus = &ps
		ordStatus = &os
	case "PAYMENT_CONFIRMED", "PAYMENT_APPROVED_BY_RISK_ANALYSIS", "PAYMENT_AUTHORIZED":
		ps := payment.StatusPending
		os := order.StatusProcessing
		payStatus = &ps
		ordStatus = &os
	case "PAYMENT_RECEIVED":
		ps := payment.StatusReceived
		os := order.StatusPaid
		payStatus = &ps
		ordStatus = &os
	case "PAYMENT_RECEIVED_IN_CASH_UNDONE":
		ps := payment.StatusPending
		os := order.StatusProcessing
		payStatus = &ps
		ordStatus = &os
	case "PAYMENT_OVERDUE":
		ps := payment.StatusOverdue
		os := order.StatusProcessing
		payStatus = &ps
		ordStatus = &os
	case "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED":
		ps := payment.StatusRefunded
		os := order.StatusRefunded
		payStatus = &ps
		ordStatus = &os
	case "PAYMENT_REFUND_IN_PROGRESS":
		ps := payment.StatusRefundInProgress
		payStatus = &ps
	case "PAYMENT_RECEIVED_IN_CASH":
		ps := payment.StatusReceivedInCash
		os := order.StatusPaid
		payStatus = &ps
		ordStatus = &os
	case "PAYMENT_DELETED":
		ps := payment.StatusCancelled
		os := order.StatusCancelled
		payStatus = &ps
		ordStatus = &os
	default:
	}

	return payStatus, ordStatus
}

func (uc *handleAsaasWebhookUseCase) isInvoicePayment(asaasPaymentID string) bool {
	inv, err := uc.invoiceRepo.GetByExternalID(asaasPaymentID)
	if err != nil || inv == nil {
		return false
	}
	return true
}

func (uc *handleAsaasWebhookUseCase) handleInvoicePayment(event *payment.AsaasWebhookEvent, normalizedEvent string) error {
	inv, err := uc.invoiceRepo.GetByExternalID(event.Payment.ID)
	if err != nil {
		return fmt.Errorf("invoice lookup failed: %w", err)
	}
	if inv == nil {
		return nil
	}

	asaasInvoiceNumber := strings.TrimSpace(event.Payment.InvoiceNumber)

	switch inv.NormalizedPurpose() {
	case invoice.PurposeSubscription:
		return uc.handleSubscriptionInvoicePayment(inv, normalizedEvent, asaasInvoiceNumber)
	case invoice.PurposeMonthlyBilling:
		return uc.handleMonthlyBillingInvoicePayment(inv, normalizedEvent, asaasInvoiceNumber)
	default:
		return uc.handleTopUpInvoicePayment(inv, normalizedEvent, asaasInvoiceNumber)
	}
}

// monthlyBillingCreditableUSD is the saldo portion of a unified monthly invoice (the plan part).
// It falls back to the full AmountUSD for invoices created before CreditableUSD existed.
func monthlyBillingCreditableUSD(inv *invoice.Invoice) int64 {
	if inv.CreditableUSD > 0 {
		return inv.CreditableUSD
	}
	return inv.AmountUSD
}

func (uc *handleAsaasWebhookUseCase) handleMonthlyBillingInvoicePayment(inv *invoice.Invoice, normalizedEvent, asaasInvoiceNumber string) error {
	switch normalizedEvent {
	case "PAYMENT_RECEIVED", "PAYMENT_RECEIVED_IN_CASH":
		transitioned, err := uc.invoiceRepo.MarkPaid(inv.ID, inv.AmountUSD)
		if err != nil {
			return fmt.Errorf("failed to mark monthly invoice paid: %w", err)
		}
		if !transitioned {
			return nil // already processed; the MarkPaid transition is the idempotency guard
		}

		// Credit only the plan portion to saldo; the channel-license repasse never becomes saldo.
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

		// Advance the plan and active addon periods to the next anchor.
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

	case "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED":
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

	case "PAYMENT_DELETED":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusCancelled); err != nil {
			return err
		}
	}
	return nil
}

func (uc *handleAsaasWebhookUseCase) handleTopUpInvoicePayment(inv *invoice.Invoice, normalizedEvent, asaasInvoiceNumber string) error {

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

	case "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED":

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

	case "PAYMENT_DELETED":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusCancelled); err != nil {
			return err
		}

	default:

	}

	return nil
}

func (uc *handleAsaasWebhookUseCase) handleSubscriptionInvoicePayment(inv *invoice.Invoice, normalizedEvent, asaasInvoiceNumber string) error {
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

	case "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED":

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

	case "PAYMENT_DELETED":
		if err := uc.invoiceRepo.UpdateStatus(inv.ID, invoice.StatusCancelled); err != nil {
			return err
		}
	}

	return nil
}

func (uc *handleAsaasWebhookUseCase) recordAffiliateEarning(inv *invoice.Invoice, purpose string) {
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

func ptrTime(t time.Time) *time.Time { return &t }

func invoicePaidDate(inv *invoice.Invoice) time.Time {
	if inv != nil && inv.PaidAt != nil && *inv.PaidAt > 0 {
		return time.Unix(*inv.PaidAt, 0)
	}
	return time.Now()
}

func optionalString(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
