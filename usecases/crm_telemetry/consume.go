package crm_telemetry_usecase

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	ap "vozko/domain/agent_presence"
	aa "vozko/domain/ai_attendance"
	ce "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/messaging"
	qe "vozko/domain/queue_event"
	aa_usecase "vozko/usecases/ai_attendance"
)

type deduper interface {
	Claim(id, kind string) (bool, error)
	Release(id string) error
}

type consumer struct {
	queueSub  messaging.MessageQueueSub
	events    ce.Repository
	history   ia.HistoryRepository
	aiSess    *aa_usecase.SessionService
	queueRepo qe.Repository
	presence  ap.Repository
	dedupe    deduper
	drops     crm_telemetry.DropRecorder
	semaphore chan struct{}
}

type ConsumerDeps struct {
	QueueSub  messaging.MessageQueueSub
	Events    ce.Repository
	History   ia.HistoryRepository
	AIRepo    aa.Repository
	QueueRepo qe.Repository
	Presence  ap.Repository
	Dedupe    deduper
	Drops     crm_telemetry.DropRecorder
}

func NewConsumer(
	queueSub messaging.MessageQueueSub,
	events ce.Repository,
	history ia.HistoryRepository,
	aiRepo aa.Repository,
	queueRepo qe.Repository,
	presence ap.Repository,
) crm_telemetry.Consumer {
	return NewConsumerWithDeps(ConsumerDeps{
		QueueSub:  queueSub,
		Events:    events,
		History:   history,
		AIRepo:    aiRepo,
		QueueRepo: queueRepo,
		Presence:  presence,
	})
}

func NewConsumerWithDeps(d ConsumerDeps) crm_telemetry.Consumer {
	directLog := &directEventLogger{repo: d.Events}
	aiSvc := aa_usecase.NewSessionService(d.AIRepo, directLog)
	return &consumer{
		queueSub:  d.QueueSub,
		events:    d.Events,
		history:   d.History,
		aiSess:    aiSvc,
		queueRepo: d.QueueRepo,
		presence:  d.Presence,
		dedupe:    d.Dedupe,
		drops:     d.Drops,
		semaphore: make(chan struct{}, 20),
	}
}

type directEventLogger struct {
	repo ce.Repository
}

func (l *directEventLogger) Log(event *ce.ConversationEvent) {
	if l == nil || l.repo == nil || event == nil {
		return
	}
	event.Normalize()
	if err := l.repo.Create(event); err != nil {
		log.Printf("[crm_telemetry] event create: %v", err)
	}
}

func (c *consumer) Start() error {
	go func() {
		if err := c.queueSub.Subscribe(crm_telemetry.Topic, func(payload []byte, ack messaging.MessageAck) {
			c.handle(payload, ack)
		}); err != nil {
			log.Printf("[crm_telemetry] subscribe failed: %v", err)
		}
	}()
	return nil
}

func (c *consumer) handle(payload []byte, ack messaging.MessageAck) {
	var env crm_telemetry.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		log.Printf("[crm_telemetry] unmarshal envelope: %v", err)
		if c.drops != nil {
			c.drops.IncTelemetryConsumeError("unknown", "unmarshal")
		}
		_ = ack.Nack(false)
		return
	}

	c.semaphore <- struct{}{}
	go func(e crm_telemetry.Envelope, a messaging.MessageAck) {
		defer func() { <-c.semaphore }()

		if c.dedupe != nil && e.ID != "" {
			claimed, err := c.dedupe.Claim(e.ID, string(e.Kind))
			if err != nil {
				log.Printf("[crm_telemetry] dedupe claim %s: %v", e.ID, err)
				c.fail(e, a, "dedupe", err, false)
				return
			}
			if !claimed {
				_ = a.Ack()
				return
			}
		}

		if err := c.dispatch(e); err != nil {
			permanent := isPermanentError(err)
			if c.dedupe != nil && e.ID != "" && !permanent {
				_ = c.dedupe.Release(e.ID)
			}
			c.fail(e, a, "dispatch", err, !permanent)
			return
		}
		if err := a.Ack(); err != nil {
			log.Printf("[crm_telemetry] ack: %v", err)
		}
	}(env, ack)
}

func (c *consumer) fail(e crm_telemetry.Envelope, a messaging.MessageAck, reason string, err error, mayRequeue bool) {
	log.Printf("[crm_telemetry] handle %s id=%s: %v", e.Kind, e.ID, err)
	if c.drops != nil {
		c.drops.IncTelemetryConsumeError(string(e.Kind), reason)
	}
	attempts := 0
	if a != nil {
		attempts = a.DeliveryCount()
	}
	requeue := mayRequeue
	if attempts >= crm_telemetry.MaxDeliveryAttempts {
		requeue = false
	}
	if attempts == 0 && mayRequeue {
		requeue = true
	}
	if !requeue && c.drops != nil {
		c.drops.IncTelemetryDropped(string(e.Kind), "max_attempts")
	}
	_ = a.Nack(requeue)
}

func isPermanentError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ce.ErrInvalidEvent) {
		return true
	}
	return hasPermanentSQLState(err.Error())
}

func hasPermanentSQLState(msg string) bool {
	i := strings.Index(msg, "SQLSTATE ")
	if i < 0 {
		return false
	}
	code := msg[i+len("SQLSTATE "):]
	if len(code) < 2 {
		return false
	}
	switch code[:2] {
	case "22", "23":
		return true
	}
	return false
}

func (c *consumer) dispatch(env crm_telemetry.Envelope) error {
	switch env.Kind {
	case crm_telemetry.KindConversationEvent:
		var ev ce.ConversationEvent
		if err := json.Unmarshal(env.Payload, &ev); err != nil {
			return err
		}
		if ev.ID == "" {
			ev.ID = env.ID
		}
		ev.Normalize()
		if err := ev.Validate(); err != nil {
			return err
		}
		return c.events.Create(&ev)

	case crm_telemetry.KindAssignmentHistory:
		var p crm_telemetry.AssignmentHistoryPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return err
		}
		if p.ID == "" {
			p.ID = env.ID
		}
		return c.applyAssignmentHistory(p)

	case crm_telemetry.KindAISession:
		var p crm_telemetry.AISessionPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return err
		}
		return c.applyAISession(p)

	case crm_telemetry.KindQueueEvent:
		var p crm_telemetry.QueueEventPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return err
		}
		id := p.ID
		if id == "" {
			id = env.ID
		}
		return c.queueRepo.Create(&qe.Event{
			ID:          id,
			WorkspaceID: p.WorkspaceID,
			TransferID:  p.TransferID,
			CallID:      p.CallID,
			TargetKind:  p.TargetKind,
			TargetID:    p.TargetID,
			Type:        p.Type,
			Position:    p.Position,
			WaitedMS:    p.WaitedMS,
			OccurredAt:  p.OccurredAt,
		})

	case crm_telemetry.KindPresence:
		var p crm_telemetry.PresencePayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return err
		}
		at := p.At
		if at.IsZero() {
			at = time.Now().UTC()
		}
		return c.presence.Transition(p.WorkspaceID, p.UserID, ap.State(p.State), p.Source, at)

	default:
		log.Printf("[crm_telemetry] unknown kind %q", env.Kind)
		return nil
	}
}

func (c *consumer) applyAssignmentHistory(p crm_telemetry.AssignmentHistoryPayload) error {
	if c.history == nil {
		return nil
	}
	at := p.StartedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if err := c.history.CloseOpen(p.WorkspaceID, p.EntryID, p.EntryType, at); err != nil {
		return err
	}
	if p.AssignedActorID == "" {
		return nil
	}
	return c.history.Append(&ia.AssignmentHistory{
		ID:                p.ID,
		WorkspaceID:       p.WorkspaceID,
		EntryID:           p.EntryID,
		EntryType:         p.EntryType,
		ActorKind:         p.ActorKind,
		AssignedActorID:   p.AssignedActorID,
		PreviousActorID:   p.PreviousActorID,
		Trigger:           p.Trigger,
		AssignedByActorID: p.AssignedByActorID,
		BusinessPhoneID:   p.BusinessPhoneID,
		DepartmentID:      p.DepartmentID,
		StartedAt:         at,
	})
}

func (c *consumer) applyAISession(p crm_telemetry.AISessionPayload) error {
	if c.aiSess == nil {
		return nil
	}
	switch p.Op {
	case crm_telemetry.AISessionOpEnsureOpen, crm_telemetry.AISessionOpRecordReply:
		if p.Op == crm_telemetry.AISessionOpEnsureOpen {
			_ = c.aiSess.EnsureOpen(aa.StartInput{
				WorkspaceID: p.WorkspaceID,
				EntryID:     p.EntryID,
				EntryType:   p.EntryType,
				AgentID:     p.AgentID,
				Channel:     p.Channel,
				CallID:      p.CallID,
				CampaignID:  p.CampaignID,
				Model:       p.Model,
			})
			return nil
		}
		c.aiSess.RecordAIReply(aa.StartInput{
			WorkspaceID: p.WorkspaceID,
			EntryID:     p.EntryID,
			EntryType:   p.EntryType,
			AgentID:     p.AgentID,
			Channel:     p.Channel,
			CallID:      p.CallID,
			CampaignID:  p.CampaignID,
			Model:       p.Model,
		}, p.MessageID)
	case crm_telemetry.AISessionOpEndOpen:
		c.aiSess.EndOpenWithCallID(p.WorkspaceID, p.EntryID, p.EntryType, p.CallID, aa.Outcome(p.Outcome), p.Reason, p.HandoffTargetUserID)
	case crm_telemetry.AISessionOpTouchInbound:
		c.aiSess.TouchInbound(p.WorkspaceID, p.EntryID, p.EntryType)
	}
	return nil
}
