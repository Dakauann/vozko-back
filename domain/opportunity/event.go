package opportunity

import "time"

type EventType string

const (
	EventCreated      EventType = "created"
	EventStageMoved   EventType = "stage_moved"
	EventWon          EventType = "won"
	EventLost         EventType = "lost"
	EventReopened     EventType = "reopened"
	EventValueChanged EventType = "value_changed"
	EventOwnerChanged EventType = "owner_changed"
	EventLinked       EventType = "linked"
)

type Event struct {
	ID            string         `json:"id"`
	WorkspaceID   string         `json:"workspaceId"`
	OpportunityID string         `json:"opportunityId"`
	Type          EventType      `json:"type"`
	ActorID       string         `json:"actorId"`
	FromStageID   string         `json:"fromStageId,omitempty"`
	ToStageID     string         `json:"toStageId,omitempty"`
	ValueCents    int64          `json:"valueCents"`
	Currency      string         `json:"currency"`
	Details       map[string]any `json:"details,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

func Changes(before, after *Opportunity, actorID string, at time.Time) []Event {
	event := func(t EventType) Event {
		return Event{
			WorkspaceID:   after.WorkspaceID,
			OpportunityID: after.ID,
			Type:          t,
			ActorID:       actorID,
			ToStageID:     after.StageID,
			ValueCents:    after.ValueCents,
			Currency:      after.Currency,
			CreatedAt:     at,
		}
	}
	if before == nil {
		return []Event{event(EventCreated)}
	}

	var out []Event
	if before.StageID != after.StageID {
		moved := event(EventStageMoved)
		moved.FromStageID = before.StageID
		out = append(out, moved)
	}
	if before.Status != after.Status {
		switch after.Status {
		case StatusWon:
			out = append(out, event(EventWon))
		case StatusLost:
			lost := event(EventLost)
			lost.Details = map[string]any{"lost_reason_id": after.LostReasonID}
			out = append(out, lost)
		case StatusOpen:
			out = append(out, event(EventReopened))
		}
	}
	if before.ValueCents != after.ValueCents || before.Currency != after.Currency {
		changed := event(EventValueChanged)
		changed.Details = map[string]any{"from_value_cents": before.ValueCents, "from_currency": before.Currency}
		out = append(out, changed)
	}
	if before.OwnerID != after.OwnerID {
		owner := event(EventOwnerChanged)
		owner.Details = map[string]any{"from_owner_id": before.OwnerID, "to_owner_id": after.OwnerID}
		out = append(out, owner)
	}
	return out
}

func LinkedEvent(o *Opportunity, entryID, entryType, actorID string, at time.Time) Event {
	return Event{
		WorkspaceID:   o.WorkspaceID,
		OpportunityID: o.ID,
		Type:          EventLinked,
		ActorID:       actorID,
		ToStageID:     o.StageID,
		ValueCents:    o.ValueCents,
		Currency:      o.Currency,
		Details:       map[string]any{"entry_id": entryID, "entry_type": entryType},
		CreatedAt:     at,
	}
}
