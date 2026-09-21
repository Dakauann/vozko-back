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

const alertSendTimeout = 2 * time.Minute

type AlertSender interface {
	Send(ctx context.Context, alert ca.Alert) error
}

type AlertConsumerDeps struct {
	Subscriber messaging.MessageQueueSub
	Dispatcher ca.AlertDispatcher
	Rules      ca.AlertRuleRepository
	Briefer    ca.AlertBriefer
	Settings   ca.SettingsRepository
	Balance    balance.CachedBalanceChecker
	Batches    ca.BatchRepository
	Clock      ca.Clock
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

func (c *AlertConsumer) Handle(message []byte, ack messaging.MessageAck) {
	var alert ca.Alert
	if err := json.Unmarshal(message, &alert); err != nil {
		log.Printf("[comment-analysis] alerts: unreadable queued alert, dropping: %v", err)
		_ = ack.Nack(false)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), alertSendTimeout)
	defer cancel()
	_ = c.Send(ctx, alert)
	_ = ack.Ack()
}

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

func (c *AlertConsumer) brief(ctx context.Context, alert ca.Alert) ca.AlertBriefing {
	if c.Briefer == nil {
		return ca.AlertBriefing{}
	}
	rule := alert.Rule
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
