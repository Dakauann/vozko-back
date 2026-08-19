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
	"vozko/domain/business_metrics"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

// inFlightGrace is how long a `pending` row is assumed to belong to a caller
// that is still running.
//
// It separates two situations that look identical in the database. A second
// request arriving while the first is mid-flight must be refused, or both charge.
// A second request arriving after the first process DIED must be allowed to
// finish the job, or the customer's money stays held forever. The only thing
// telling them apart is how long the row has sat still.
const inFlightGrace = 2 * time.Minute

var (
	ErrRecipientRequired    = errors.New("recipient phone number is required")
	ErrTemplateNameRequired = errors.New("template name is required")
)

// BilledTemplateSenderDeps is everything a paid send needs.
//
// It is a struct rather than a long parameter list because the constructor
// validates it: a missing billing dependency must be a boot failure, and a
// positional constructor makes "which one was nil" a guessing game.
type BilledTemplateSenderDeps struct {
	Templates     template.Repository
	Attempts      template.SendAttemptRepository
	ClientFactory template.WhatsAppClientFactory

	Consume balance.ConsumeWhatsappTemplateUseCase
	// Ledger answers "was this reference already charged", the belt that makes a
	// resumed attempt safe.
	Ledger         balance.Repository
	Inflight       balance.InflightReserver
	BalanceChecker balance.CachedBalanceChecker
	RecordMetric   business_metrics.RecordMetricUseCase
	Alerter        billing.OpsAlerter
	Now            func() time.Time
}

type billedTemplateSendUseCase struct {
	deps BilledTemplateSenderDeps
}

// NewBilledTemplateSendUseCase refuses to build a sender that could send for
// free.
//
// Returning an error rather than a value is the entire point. The previous
// generation of this code guarded billing with `if dep != nil`, which turned a
// wiring mistake into unbilled sends that nobody notices until the invoice
// arrives. Here the same mistake stops the process at boot.
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

// Execute charges once and sends once.
//
// The order of the steps below is the design. Everything that can refuse the
// send is asked BEFORE the money moves, because a refusal after the debit is a
// charge-then-refund cycle: two ledger rows, a confused operator, and a customer
// balance that dips for reasons they cannot see. Everything that can only be
// learned from the provider happens after, and is classified rather than
// assumed.
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

	// ---- 1. everything that can refuse, before the row exists ---------------

	tmpl, err := uc.deps.Templates.FindByID(in.TemplateID)
	if err != nil || tmpl == nil {
		return nil, template.ErrTemplateNotFound
	}

	// Tenancy: the template and the number must belong to the same WhatsApp
	// Business Account. Enforced HERE rather than in the caller so every caller
	// inherits it — a rule that lives in one handler protects one handler.
	wabaID, wabaErr := uc.deps.ClientFactory.WABAIdForPhone(in.BusinessPhoneID)
	if wabaErr != nil || strings.TrimSpace(wabaID) == "" {
		return nil, fmt.Errorf("resolve WABA for template billing: %w", template.ErrTemplateCategoryUnavailable)
	}
	if strings.TrimSpace(tmpl.WABAId) != "" && !strings.EqualFold(strings.TrimSpace(tmpl.WABAId), strings.TrimSpace(wabaID)) {
		return nil, template.ErrTemplatePhoneMismatch
	}

	// An unusable template is refused before the debit. The campaign consumer
	// learned this the expensive way: without it, every unapproved template and
	// every missing header image became a charge followed by a refund.
	if !tmpl.IsReadyToSend() {
		msg := tmpl.GetUsabilityMessage()
		if msg == "" {
			msg = fmt.Sprintf("template status is %s", tmpl.Status)
		}
		return nil, fmt.Errorf("%w: %s", template.ErrTemplateNotSendable, msg)
	}

	category, err := tmpl.BillingCategory()
	if err != nil {
		return nil, fmt.Errorf("template metadata unavailable for billing: %w", err)
	}

	// Price first, because a workspace with no price must STOP rather than send.
	// The consume use case answers a zero price with (nil, nil) — success-shaped
	// — so a caller that only checked the error would send for free.
	costMicros, err := uc.deps.Consume.GetTemplateCostMicros(in.WorkspaceID, category)
	if err != nil {
		return nil, err
	}
	if costMicros <= 0 {
		uc.alert(ctx, "WhatsApp template send refused: no price configured",
			fmt.Sprintf("workspace=%s category=%s template=%s", in.WorkspaceID, category, tmpl.Name))
		return nil, template.ErrPricingUnavailable
	}

	// ---- 2. the row, and who is allowed to spend ---------------------------

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
		// This request has been seen before. What to do depends on how far the
		// first one got, and guessing in either direction costs money.
		switch {
		case attempt.Status.IsTerminal() || attempt.Status == template.SendAttemptRejected || attempt.Status == template.SendAttemptUnknown:
			return replayOf(attempt, tmpl), nil
		case attempt.Status == template.SendAttemptCharged:
			// Money left and we do not know whether a message did. Resending could
			// deliver twice; refunding could credit a delivered message. Neither is
			// ours to decide here — reconciliation settles it.
			return nil, template.ErrSendInProgress
		case attempt.Status == template.SendAttemptPending && uc.deps.Now().Sub(attempt.UpdatedAt) < inFlightGrace:
			// Somebody is mid-flight right now.
			return nil, template.ErrSendInProgress
		}
		// A stale pending row: the process that created it died before charging.
		// Resuming is safe because the debit is idempotent by reference below.
		log.Printf("[billed-template-send] resuming stale attempt %s (workspace %s)", attempt.ID, attempt.WorkspaceID)
	}

	// ---- 3. reserve, charge, and only then send ----------------------------

	budget, err := uc.deps.BalanceChecker.GetBalance(in.WorkspaceID)
	if err != nil {
		// Fail closed. A balance we cannot read is not a balance we may spend.
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

	// The belt. Not the primary guard — that is the unique index above — but it
	// is what makes resuming a stale attempt safe: if the dead process managed to
	// debit before dying, we must not debit again.
	alreadyCharged, existsErr := uc.deps.Ledger.ExistsTransactionByReferenceID(chargeRef)
	if existsErr != nil {
		return nil, fmt.Errorf("could not verify existing charge: %w", existsErr)
	}
	if !alreadyCharged {
		if _, consumeErr := uc.deps.Consume.Execute(in.WorkspaceID, chargeRef, category); consumeErr != nil {
			return nil, consumeErr
		}
	}

	// MarkCharged is the money gate, and it is a compare-and-set. Losing it means
	// another replica charged and is already sending; stopping here is what keeps
	// "one debit" and "one send" the same number.
	if err := uc.deps.Attempts.MarkCharged(ctx, attempt.ID, costMicros, uc.deps.Now()); err != nil {
		if errors.Is(err, template.ErrSendAttemptConflict) {
			return nil, template.ErrSendInProgress
		}
		return nil, err
	}
	attempt.Status = template.SendAttemptCharged
	attempt.ChargedMicros = costMicros

	// ---- 4. send, and classify what came back ------------------------------

	client, err := uc.deps.ClientFactory.ClientForPhone(in.BusinessPhoneID)
	if err != nil {
		// Charged but unable to send at all: refund immediately, this one is
		// unambiguous.
		uc.refund(ctx, attempt, category)
		return nil, err
	}

	sendInput := buildTemplateSendInput(tmpl, in, attempt.ID)
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
			// Worth a human's attention: we are keeping money for a send we cannot
			// prove, which is correct but should be rare.
			uc.alert(ctx, "WhatsApp template accepted without a message id",
				fmt.Sprintf("attempt=%s workspace=%s template=%s", attempt.ID, in.WorkspaceID, tmpl.Name))
		}
		uc.recordSentMetric(tmpl, in, result.MessageID)

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

	default: // OutcomeUnknown
		// Deliberately no refund. We cannot tell a request that never arrived from
		// one that arrived and was delivered, and crediting the second case costs
		// real money. The reconcile sweep settles it once the answer is knowable.
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

// refund credits the charge back exactly once.
//
// Guarded by the ledger rather than by the caller being careful: the refund path
// is reachable from here, from the delivery-status webhook and from the
// reconcile sweep, and any two of them firing for one attempt would credit twice.
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
		// A failed refund is money we owe and did not return. It must reach a human.
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

func (uc *billedTemplateSendUseCase) recordSentMetric(tmpl *template.Template, in template.BilledSendInput, messageID string) {
	if uc.deps.RecordMetric == nil {
		return
	}
	entityID := strings.TrimSpace(messageID)
	if entityID == "" {
		entityID = uuid.NewString()
	}
	if err := uc.deps.RecordMetric.Execute(business_metrics.RecordMetricInput{
		EventType:  business_metrics.EventWhatsAppTemplateMessageSent,
		EntityID:   entityID,
		EntityType: business_metrics.EntityTypeMessage,
		Metadata: map[string]string{
			"to":            strings.TrimSpace(in.ToNumber),
			"template_name": strings.TrimSpace(tmpl.Name),
			"template_id":   strings.TrimSpace(tmpl.ID),
			"language":      strings.TrimSpace(tmpl.Language),
			"source":        "whatsapp_outreach",
		},
	}); err != nil {
		log.Printf("[billed-template-send] failed to record sent metric: %v", err)
	}
}

// replayOf answers a repeated request from what the first one already did.
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

// buildTemplateSendInput is the single place template metadata becomes a
// provider payload, so the named/positional decision and the media-header
// decision are made once for every caller.
func buildTemplateSendInput(tmpl *template.Template, in template.BilledSendInput, attemptID string) conversation.SendTemplateMessageInput {
	bodyParamNames, _ := tmpl.GetBodyAndHeaderParameterNames()

	out := conversation.SendTemplateMessageInput{
		To:                     in.ToNumber,
		TemplateName:           tmpl.Name,
		Language:               tmpl.Language,
		Parameters:             in.BodyParams,
		ParameterNames:         bodyParamNames,
		IsNamedParameterFormat: tmpl.IsNamedParameterFormat(),
		HeaderTextParams:       in.HeaderParams,
		// The attempt id rides to Meta and comes back on every delivery-status
		// webhook, which is how a status event finds the charge that paid for it
		// even when we never learned the message id.
		BizOpaqueCallbackData: attemptID,
	}

	if tmpl.HasMediaHeader() {
		out.HeaderType = strings.ToLower(tmpl.GetHeaderFormat())
		out.HeaderMediaID = tmpl.GetHeaderMediaID()
	}
	return out
}

// metaErrorFrom prefers the provider's own error over our transport error: "this
// template is paused" is actionable, "unexpected status 400" is not.
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
