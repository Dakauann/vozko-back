package template_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/balance"
	"vozko/domain/billing"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

const inFlightGrace = 2 * time.Minute

var (
	ErrRecipientRequired    = errors.New("recipient phone number is required")
	ErrTemplateNameRequired = errors.New("template name is required")
)

type BilledTemplateSenderDeps struct {
	Templates     template.Repository
	Attempts      template.SendAttemptRepository
	ClientFactory template.WhatsAppClientFactory

	Consume        balance.ConsumeWhatsappTemplateUseCase
	Ledger         balance.Repository
	Inflight       balance.InflightReserver
	BalanceChecker balance.CachedBalanceChecker
	Alerter        billing.OpsAlerter
	Now            func() time.Time
}

type billedTemplateSendUseCase struct {
	deps BilledTemplateSenderDeps
}

func NewBilledTemplateSendUseCase(deps BilledTemplateSenderDeps) (template.BilledTemplateSendUseCase, error) {
	var missing []string
	if deps.Templates == nil {
		missing = append(missing, "template repository")
	}
	if deps.Attempts == nil {
		missing = append(missing, "send attempt repository")
	}
	if deps.ClientFactory == nil {
		missing = append(missing, "whatsapp client factory")
	}
	if deps.Consume == nil {
		missing = append(missing, "whatsapp template billing")
	}
	if deps.Ledger == nil {
		missing = append(missing, "balance ledger")
	}
	if deps.Inflight == nil {
		missing = append(missing, "inflight reserver")
	}
	if deps.BalanceChecker == nil {
		missing = append(missing, "cached balance checker")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", template.ErrBillingNotConfigured, strings.Join(missing, ", "))
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &billedTemplateSendUseCase{deps: deps}, nil
}

func (uc *billedTemplateSendUseCase) Execute(ctx context.Context, in template.BilledSendInput) (*template.BilledSendResult, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, template.ErrWorkspaceRequired
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, template.ErrIdempotencyKeyRequired
	}
	if strings.TrimSpace(in.TemplateID) == "" {
		return nil, template.ErrTemplateNotFound
	}
	if strings.TrimSpace(in.BusinessPhoneID) == "" {
		return nil, ErrRecipientRequired
	}
	if strings.TrimSpace(in.ToNumber) == "" {
		return nil, ErrRecipientRequired
	}

	tmpl, err := uc.deps.Templates.FindByID(in.TemplateID)
	if err != nil || tmpl == nil {
		return nil, template.ErrTemplateNotFound
	}

	wabaID, wabaErr := uc.deps.ClientFactory.WABAIdForPhone(in.BusinessPhoneID)
	if wabaErr != nil || strings.TrimSpace(wabaID) == "" {
		return nil, fmt.Errorf("resolve WABA for template billing: %w", template.ErrTemplateCategoryUnavailable)
	}
	if strings.TrimSpace(tmpl.WABAId) != "" && !strings.EqualFold(strings.TrimSpace(tmpl.WABAId), strings.TrimSpace(wabaID)) {
		return nil, template.ErrTemplatePhoneMismatch
	}

	if !tmpl.IsReadyToSend() {
		msg := tmpl.GetUsabilityMessage()
		if msg == "" {
			msg = fmt.Sprintf("template status is %s", tmpl.Status)
		}
		return nil, fmt.Errorf("%w: %s", template.ErrTemplateNotSendable, msg)
	}

	sendInput, err := buildTemplateSendInput(tmpl, in, "")
	if err != nil {
		return nil, err
	}

	category, err := tmpl.BillingCategory()
	if err != nil {
		return nil, fmt.Errorf("template metadata unavailable for billing: %w", err)
	}

	costMicros, err := uc.deps.Consume.GetTemplateCostMicros(in.WorkspaceID, category)
	if err != nil {
		return nil, err
	}
	if costMicros <= 0 {
		uc.alert(ctx, "WhatsApp template send refused: no price configured",
			fmt.Sprintf("workspace=%s category=%s template=%s", in.WorkspaceID, category, tmpl.Name))
		return nil, template.ErrPricingUnavailable
	}

	attempt := &template.SendAttempt{
		ID:              uuid.New().String(),
		WorkspaceID:     in.WorkspaceID,
		UserID:          in.UserID,
		IdempotencyKey:  in.IdempotencyKey,
		BusinessPhoneID: in.BusinessPhoneID,
		TemplateID:      tmpl.ID,
		TemplateName:    tmpl.Name,
		Language:        tmpl.Language,
		Category:        category,
		ToNumber:        in.ToNumber,
		Status:          template.SendAttemptPending,
	}
	if strings.TrimSpace(in.CampaignID) != "" {
		id := in.CampaignID
		attempt.CampaignID = &id
	}
	if strings.TrimSpace(in.EntryID) != "" {
		id := in.EntryID
		attempt.EntryID = &id
	}

	stored, created, err := uc.deps.Attempts.CreateIfAbsent(ctx, attempt)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("whatsapp template send: attempt could not be stored")
	}
	attempt = stored

	if !created {
		switch {
		case attempt.Status.IsTerminal() || attempt.Status == template.SendAttemptRejected || attempt.Status == template.SendAttemptUnknown:
			return replayOf(attempt, tmpl), nil
		case attempt.Status == template.SendAttemptCharged:
			return nil, template.ErrSendInProgress
		case attempt.Status == template.SendAttemptPending && uc.deps.Now().Sub(attempt.UpdatedAt) < inFlightGrace:
			return nil, template.ErrSendInProgress
		}
		log.Printf("[billed-template-send] resuming stale attempt %s (workspace %s)", attempt.ID, attempt.WorkspaceID)
	}

	budget, err := uc.deps.BalanceChecker.GetBalance(in.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("could not read balance: %w", err)
	}
	reserved, err := uc.deps.Inflight.Reserve(in.WorkspaceID, costMicros, budget)
	if err != nil {
		return nil, fmt.Errorf("could not reserve balance: %w", err)
	}
	if !reserved {
		return nil, balance.ErrInsufficientBalance
	}
	defer func() {
		if releaseErr := uc.deps.Inflight.Release(in.WorkspaceID, costMicros); releaseErr != nil {
			log.Printf("[billed-template-send] failed to release inflight reservation for %s: %v", in.WorkspaceID, releaseErr)
		}
	}()

	chargeRef := template.ChargeReferenceID(attempt.ID)

	alreadyCharged, existsErr := uc.deps.Ledger.ExistsTransactionByReferenceID(chargeRef)
	if existsErr != nil {
		return nil, fmt.Errorf("could not verify existing charge: %w", existsErr)
	}
	if !alreadyCharged {
		if _, consumeErr := uc.deps.Consume.Execute(in.WorkspaceID, chargeRef, category); consumeErr != nil {
			return nil, consumeErr
		}
	}

	if err := uc.deps.Attempts.MarkCharged(ctx, attempt.ID, costMicros, uc.deps.Now()); err != nil {
		if errors.Is(err, template.ErrSendAttemptConflict) {
			return nil, template.ErrSendInProgress
		}
		return nil, err
	}
	attempt.Status = template.SendAttemptCharged
	attempt.ChargedMicros = costMicros

	client, err := uc.deps.ClientFactory.ClientForPhone(in.BusinessPhoneID)
	if err != nil {
		uc.refund(ctx, attempt, category)
		return nil, err
	}

	sendInput.BizOpaqueCallbackData = attempt.ID
	out, sendErr := client.SendTemplateMessage(ctx, sendInput)
	outcome := template.ClassifySendOutcome(out, sendErr)

	result := &template.BilledSendResult{
		AttemptID:     attempt.ID,
		Outcome:       outcome,
		ChargedMicros: costMicros,
		Template:      tmpl,
	}
	if out != nil {
		result.MessageID = out.MessageID
	}

	switch outcome {
	case template.OutcomeAccepted, template.OutcomeAcceptedNoID:
		responseStatus := 0
		if out != nil {
			responseStatus = out.ResponseStatus
		}
		if markErr := uc.deps.Attempts.MarkSent(ctx, attempt.ID, result.MessageID, responseStatus, uc.deps.Now()); markErr != nil {
			log.Printf("[billed-template-send] delivered but could not mark attempt %s as sent: %v", attempt.ID, markErr)
		}
		result.Status = template.SendAttemptSent
		if outcome == template.OutcomeAcceptedNoID {
			uc.alert(ctx, "WhatsApp template accepted without a message id",
				fmt.Sprintf("attempt=%s workspace=%s template=%s", attempt.ID, in.WorkspaceID, tmpl.Name))
		}

	case template.OutcomeRejected:
		code, message := metaErrorFrom(out, sendErr)
		responseStatus := 0
		if out != nil {
			responseStatus = out.ResponseStatus
		}
		if markErr := uc.deps.Attempts.MarkRejected(ctx, attempt.ID, code, message, responseStatus); markErr != nil {
			log.Printf("[billed-template-send] could not mark attempt %s rejected: %v", attempt.ID, markErr)
		}
		uc.refund(ctx, attempt, category)
		result.Status = template.SendAttemptRefunded
		result.ChargedMicros = 0
		return result, sendErr

	default:
		_, message := metaErrorFrom(out, sendErr)
		responseStatus := 0
		if out != nil {
			responseStatus = out.ResponseStatus
		}
		if markErr := uc.deps.Attempts.MarkUnknown(ctx, attempt.ID, message, responseStatus); markErr != nil {
			log.Printf("[billed-template-send] could not mark attempt %s unknown: %v", attempt.ID, markErr)
		}
		result.Status = template.SendAttemptUnknown
		uc.alert(ctx, "WhatsApp template send outcome unknown",
			fmt.Sprintf("attempt=%s workspace=%s template=%s err=%v", attempt.ID, in.WorkspaceID, tmpl.Name, sendErr))
		return result, sendErr
	}

	return result, nil
}

func (uc *billedTemplateSendUseCase) refund(ctx context.Context, attempt *template.SendAttempt, category string) {
	refundRef := template.RefundReferenceID(attempt.ID)
	already, err := uc.deps.Ledger.ExistsTransactionByReferenceID(refundRef)
	if err != nil {
		log.Printf("[billed-template-send] could not verify existing refund for %s: %v", attempt.ID, err)
		return
	}
	if already {
		return
	}
	if err := uc.deps.Consume.Refund(attempt.WorkspaceID, template.ChargeReferenceID(attempt.ID), category); err != nil {
		uc.alert(ctx, "WhatsApp template refund failed",
			fmt.Sprintf("attempt=%s workspace=%s category=%s err=%v", attempt.ID, attempt.WorkspaceID, category, err))
		return
	}
	if err := uc.deps.Attempts.MarkRefunded(ctx, attempt.ID, uc.deps.Now()); err != nil {
		log.Printf("[billed-template-send] refunded but could not mark attempt %s: %v", attempt.ID, err)
	}
}

func (uc *billedTemplateSendUseCase) alert(ctx context.Context, subject, detail string) {
	log.Printf("[billed-template-send] %s — %s", subject, detail)
	if uc.deps.Alerter == nil {
		return
	}
	if err := uc.deps.Alerter.Alert(ctx, subject, detail); err != nil {
		log.Printf("[billed-template-send] failed to raise alert %q: %v", subject, err)
	}
}

func replayOf(attempt *template.SendAttempt, tmpl *template.Template) *template.BilledSendResult {
	outcome := template.OutcomeAccepted
	switch attempt.Status {
	case template.SendAttemptRejected, template.SendAttemptRefunded:
		outcome = template.OutcomeRejected
	case template.SendAttemptUnknown:
		outcome = template.OutcomeUnknown
	}
	return &template.BilledSendResult{
		AttemptID:     attempt.ID,
		Status:        attempt.Status,
		Outcome:       outcome,
		MessageID:     attempt.ProviderMessageID,
		ChargedMicros: attempt.ChargedMicros,
		Template:      tmpl,
		Replayed:      true,
	}
}

func buildTemplateSendInput(tmpl *template.Template, in template.BilledSendInput, attemptID string) (conversation.SendTemplateMessageInput, error) {
	return tmpl.BuildSendInput(template.SendInputParams{
		To:                    in.ToNumber,
		BodyParams:            in.BodyParams,
		HeaderParams:          in.HeaderParams,
		BizOpaqueCallbackData: attemptID,
	})
}

func metaErrorFrom(out *conversation.SendTextMessageOutput, err error) (int, string) {
	code, message := template.ParseProviderError(out)
	if message == "" && err != nil {
		message = err.Error()
	}
	if len(message) > 500 {
		message = message[:500]
	}
	return code, message
}
