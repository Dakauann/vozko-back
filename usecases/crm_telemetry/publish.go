package crm_telemetry_usecase

import (
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/crm_telemetry"
	"vozko/domain/messaging"
)

type publisher struct {
	queuePub messaging.MessageQueuePub
	drops    crm_telemetry.DropRecorder
}

func NewPublisher(queuePub messaging.MessageQueuePub) crm_telemetry.Publisher {
	return &publisher{queuePub: queuePub}
}

func NewPublisherWithDrops(queuePub messaging.MessageQueuePub, drops crm_telemetry.DropRecorder) crm_telemetry.Publisher {
	return &publisher{queuePub: queuePub, drops: drops}
}

func (p *publisher) Publish(kind crm_telemetry.Kind, payload any) error {
	if p == nil || p.queuePub == nil {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[crm_telemetry] marshal %s: %v", kind, err)
		if p.drops != nil {
			p.drops.IncTelemetryPublishError(string(kind))
		}
		return nil
	}
	env := crm_telemetry.Envelope{
		ID:         uuid.New().String(),
		Kind:       kind,
		Payload:    body,
		OccurredAt: time.Now().UTC(),
	}
	if kind == crm_telemetry.KindConversationEvent {
		var partial struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(body, &partial) == nil && partial.ID != "" {
			env.ID = partial.ID
		}
	}
	if kind == crm_telemetry.KindAssignmentHistory {
		var partial struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(body, &partial) == nil && partial.ID != "" {
			env.ID = partial.ID
		}
	}
	raw, err := json.Marshal(env)
	if err != nil {
		log.Printf("[crm_telemetry] marshal envelope %s: %v", kind, err)
		if p.drops != nil {
			p.drops.IncTelemetryPublishError(string(kind))
		}
		return nil
	}
	if err := p.queuePub.Publish(crm_telemetry.Topic, raw); err != nil {
		log.Printf("[crm_telemetry] publish %s failed: %v", kind, err)
		if p.drops != nil {
			p.drops.IncTelemetryPublishError(string(kind))
		}
		return nil
	}
	return nil
}

type PresenceAdapter struct {
	pub crm_telemetry.Publisher
}

func NewPresenceAdapter(pub crm_telemetry.Publisher) *PresenceAdapter {
	return &PresenceAdapter{pub: pub}
}

func (a *PresenceAdapter) Transition(workspaceID, userID, state, source string) error {
	if a == nil || a.pub == nil {
		return nil
	}
	return a.pub.Publish(crm_telemetry.KindPresence, crm_telemetry.PresencePayload{
		WorkspaceID: workspaceID,
		UserID:      userID,
		State:       state,
		Source:      source,
		At:          time.Now().UTC(),
	})
}

func EmitEvent(pub crm_telemetry.Publisher, ev interface {
}) {
	if pub == nil || ev == nil {
		return
	}
	_ = pub.Publish(crm_telemetry.KindConversationEvent, ev)
}
