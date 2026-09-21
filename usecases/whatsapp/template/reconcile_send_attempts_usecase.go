package template_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"vozko/domain/balance"
	"vozko/domain/billing"
	"vozko/domain/whatsapp/template"
)

const DefaultReconcileTTL = time.Hour

type reconcileSendAttemptsUseCase struct {
	attempts template.SendAttemptRepository
	consume  balance.ConsumeWhatsappTemplateUseCase
	ledger   balance.Repository
	alerter  billing.OpsAlerter
	ttl      time.Duration
	batch    int
	now      func() time.Time
}

func NewReconcileSendAttemptsUseCase(
	attempts template.SendAttemptRepository,
	consume balance.ConsumeWhatsappTemplateUseCase,
	ledger balance.Repository,
	alerter billing.OpsAlerter,
) template.ReconcileSendAttemptsUseCase {
	return &reconcileSendAttemptsUseCase{
		attempts: attempts,
		consume:  consume,
		ledger:   ledger,
		alerter:  alerter,
		ttl:      DefaultReconcileTTL,
		batch:    200,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (uc *reconcileSendAttemptsUseCase) Execute(ctx context.Context) (int, error) {
	if uc.attempts == nil || uc.consume == nil {
		return 0, template.ErrBillingNotConfigured
	}

	cutoff := uc.now().Add(-uc.ttl)
	stale, err := uc.attempts.ListNeedingReconciliation(ctx, cutoff, uc.batch)
	if err != nil {
		return 0, err
	}

	reconciled := 0
	for _, attempt := range stale {
		if attempt == nil {
			continue
		}
		if fresh, readErr := uc.attempts.FindByID(ctx, attempt.ID); readErr == nil && fresh != nil {
			if fresh.Status.IsTerminal() || fresh.ProviderMessageID != "" {
				continue
			}
			attempt = fresh
		}

		refundRef := template.RefundReferenceID(attempt.ID)
		if uc.ledger != nil {
			already, existsErr := uc.ledger.ExistsTransactionByReferenceID(refundRef)
			if existsErr != nil {
				log.Printf("[reconcile-template-sends] could not check refund for %s: %v", attempt.ID, existsErr)
				continue
			}
			if already {
				_ = uc.attempts.MarkRefunded(ctx, attempt.ID, uc.now())
				continue
			}
		}

		if err := uc.consume.Refund(attempt.WorkspaceID, template.ChargeReferenceID(attempt.ID), attempt.Category); err != nil {
			uc.alert(ctx, "WhatsApp template reconcile refund failed",
				fmt.Sprintf("attempt=%s workspace=%s err=%v", attempt.ID, attempt.WorkspaceID, err))
			continue
		}
		if err := uc.attempts.MarkRefunded(ctx, attempt.ID, uc.now()); err != nil {
			log.Printf("[reconcile-template-sends] refunded but could not mark %s: %v", attempt.ID, err)
		}
		reconciled++
	}

	if reconciled > 0 {
		uc.alert(ctx, "WhatsApp template sends reconciled",
			fmt.Sprintf("%d unsettled send(s) older than %s were refunded", reconciled, uc.ttl))
	}
	return reconciled, nil
}

func (uc *reconcileSendAttemptsUseCase) alert(ctx context.Context, subject, detail string) {
	log.Printf("[reconcile-template-sends] %s — %s", subject, detail)
	if uc.alerter == nil {
		return
	}
	if err := uc.alerter.Alert(ctx, subject, detail); err != nil {
		log.Printf("[reconcile-template-sends] failed to raise alert: %v", err)
	}
}
