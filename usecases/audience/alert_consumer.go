package audience_usecase

import (
	"context"
	"encoding/json"
	"log"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/balance"
	"vozko/domain/messaging"
)

// Sending the alerts the evaluator claimed.
//
// The split is between what must happen exactly once and what is merely slow.
// Claiming a rule is one conditional write and it decides which replica owns a
// firing, so it stays on the analysis path. Everything after it lives here: the
// model call for the briefing and the outbound WhatsApp send.
//
// That split exists because of the shape of the flush job. It walks every due
// container of every workspace SEQUENTIALLY, in one goroutine, every thirty
// seconds. Dispatching inline put an outbound send, and optionally a model
// call, on that walk: one workspace's alert delayed every other workspace's
// analysis in the same tick, and one slow provider stalled all of them.
//
// Sending is one function with two callers. The queue is the normal path; the
// evaluator calls the same function directly when it cannot publish, so a
// broker that is down makes alerts slow again rather than silent.

// alertSendTimeout bounds one alert's briefing and send. Generous on purpose:
// nothing waits on this, the analysis pass finished long ago, and giving up
// early costs an alert nobody receives.
const alertSendTimeout = 2 * time.Minute

// AlertSender is the send itself, named as a port so the evaluator can fall
// back to it without importing the consumer's dependencies.
type AlertSender interface {
	Send(ctx context.Context, alert ca.Alert) error
}

type AlertConsumerDeps struct {
	Subscriber messaging.MessageQueueSub
	Dispatcher ca.AlertDispatcher
	// Rules records a send that failed, where the operator looks for it.
	Rules ca.AlertRuleRepository
	// The briefing and everything it answers to. It moved here from the
	// evaluator wholesale rather than being shared: one owner, and it is the
	// slowest thing an alert does.
	Briefer  ca.AlertBriefer
	Settings ca.SettingsRepository
	Balance  balance.CachedBalanceChecker
	Batches  ca.BatchRepository
	Clock    ca.Clock
}

type AlertConsumer struct {
	AlertConsumerDeps
	guard balanceGuard
}

func NewAlertConsumer(d AlertConsumerDeps) *AlertConsumer {
	return &AlertConsumer{AlertConsumerDeps: d, guard: newBalanceGuard(d.Balance, "alert briefing")}
}

func (c *AlertConsumer) Start() error {
	if c.Subscriber == nil {
		return nil
	}
	return c.Subscriber.Subscribe(ca.TopicAlertSend, c.Handle)
}

// Handle sends one queued alert.
//
// It ACKS whatever happens, and that is deliberate. The inline path never
// retried a failed send, because a refusal cannot be told apart from a message
// that arrived just before the connection dropped, and a duplicate alert at 3am
// is unrecoverable. Moving to a queue must not quietly turn that into
// at-least-once redelivery: the cooldown is still the retry interval, and the
// reason is stored on the rule.
func (c *AlertConsumer) Handle(message []byte, ack messaging.MessageAck) {
	var alert ca.Alert
	if err := json.Unmarshal(message, &alert); err != nil {
		// Nack without requeue: it will never parse, and redelivering it
		// forever is how a queue fills with one bad message.
		log.Printf("[comment-analysis] alerts: unreadable queued alert, dropping: %v", err)
		_ = ack.Nack(false)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), alertSendTimeout)
	defer cancel()
	_ = c.Send(ctx, alert)
	_ = ack.Ack()
}

// Send briefs and dispatches. Errors are recorded rather than returned to a
// retry loop; the caller only learns whether it got that far.
func (c *AlertConsumer) Send(ctx context.Context, alert ca.Alert) error {
	if c.Dispatcher == nil {
		log.Printf("[comment-analysis] alerts: no dispatcher, dropping alert for rule %s", alert.Rule.ID)
		return nil
	}
	if alert.Rule.Brief && alert.Observation.Briefing.Empty() {
		alert.Observation.Briefing = c.brief(ctx, alert)
	}

	err := c.Dispatcher.Dispatch(ctx, ca.AlertDelivery{
		WorkspaceID:     alert.Rule.WorkspaceID,
		Channel:         alert.Rule.Channel,
		Recipient:       alert.Rule.Recipient,
		BusinessPhoneID: alert.Rule.BusinessPhoneID,
		TemplateID:      alert.Rule.TemplateID,
		TemplateParams:  alert.TemplateParams(),
		Facts:           alert.Facts(),
		InstanceID:      alert.Rule.InstanceID,
		Text:            alert.Message(),
		IdempotencyKey:  alert.IdempotencyKey(),
		ActorUserID:     alert.Rule.CreatedByUserID,
	})
	if err != nil {
		log.Printf("[comment-analysis] alerts: dispatching %s: %v", alert.Rule.ID, err)
		c.recordFailure(ctx, alert, err)
	}
	return err
}

// brief asks the model to read the alert. Never fatal: an alert without advice
// still tells somebody something is wrong, and one that did not arrive tells
// them nothing.
func (c *AlertConsumer) brief(ctx context.Context, alert ca.Alert) ca.AlertBriefing {
	if c.Briefer == nil {
		return ca.AlertBriefing{}
	}
	rule := alert.Rule
	// The alert itself is free and always goes out; only the briefing is a
	// model call, so only the briefing answers to the balance floor.
	if err := c.guard.Allow(rule.WorkspaceID); err != nil {
		return ca.AlertBriefing{}
	}
	req := ca.AlertBriefRequest{
		WorkspaceID: rule.WorkspaceID,
		RuleName:    rule.Name,
		Measurement: alert.Headline(),
	}
	if cm := alert.Observation.Comment; cm != nil {
		req.Comment, req.Stance, req.Severity = cm.Excerpt, cm.Stance, cm.Severity
	}
	if c.Settings != nil {
		if s, err := c.Settings.Find(ctx, rule.Source, rule.AccountID); err == nil && s != nil {
			req.Model, req.Instructions = s.Model, s.Instructions
		}
	}

	briefing, err := c.Briefer.Brief(ctx, req)
	if err != nil || briefing == nil {
		if err != nil {
			log.Printf("[comment-analysis] alerts: briefing %s: %v", rule.ID, err)
		}
		return ca.AlertBriefing{}
	}
	recordAICall(ctx, c.Batches, c.Clock, aiCall{
		WorkspaceID: rule.WorkspaceID, Source: rule.Source, AccountID: rule.AccountID,
		Kind: ca.BatchKindAlertBrief, Model: briefing.Model,
		PromptTokens: briefing.PromptTokens, CompletionTokens: briefing.CompletionTokens,
	})
	briefing.Normalize()
	return *briefing
}

func (c *AlertConsumer) recordFailure(ctx context.Context, alert ca.Alert, cause error) {
	if c.Rules == nil {
		return
	}
	at := time.Now().UTC()
	if c.Clock != nil {
		at = c.Clock.Now()
	}
	if err := c.Rules.RecordFailure(ctx, alert.Rule.WorkspaceID, alert.Rule.ID, cause.Error(), at); err != nil {
		log.Printf("[comment-analysis] alerts: recording failure for %s: %v", alert.Rule.ID, err)
	}
}
