package conversation_event

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"vozko/domain/actor"
)

// ErrInvalidEvent marks a conversation event that can never be persisted (empty
// or non-UUID workspace_id/entry_id, unknown event_type). It is a PERMANENT
// error: the telemetry consumer must drop it, never requeue it. A single such
// event previously looped forever against the uuid NOT NULL columns, flooding
// the logs and the DB (SQLSTATE 22P02).
var ErrInvalidEvent = errors.New("conversation_event: invalid event")

type EventType string

const (
	// Existing (keep for backward compatibility)
	EventAssigned     EventType = "assigned"
	EventAutoAssigned EventType = "auto_assigned"
	EventReplied      EventType = "replied"
	EventReopened     EventType = "reopened"
	EventTagAdded     EventType = "tag_added"
	EventTagRemoved   EventType = "tag_removed"
	EventLabelAdded   EventType = "label_added"
	EventLabelRemoved EventType = "label_removed"

	// Assignment / ownership
	EventUnassigned EventType = "unassigned"

	// AI as attendant
	EventAIReplied        EventType = "ai_replied"
	EventAIEnabled        EventType = "ai_enabled"
	EventAIDisabled       EventType = "ai_disabled"
	EventAISessionStarted EventType = "ai_session_started"
	EventAISessionEnded   EventType = "ai_session_ended"

	// Lifecycle
	EventStatusChanged   EventType = "status_changed"
	EventStageChanged    EventType = "stage_changed"
	EventFinished        EventType = "finished"
	EventAnalysisCreated EventType = "analysis_created"

	// Voice / transfer / queue
	EventTransferOffered   EventType = "transfer_offered"
	EventTransferAccepted  EventType = "transfer_accepted"
	EventTransferDeclined  EventType = "transfer_declined"
	EventTransferCompleted EventType = "transfer_completed"
	EventTransferFailed    EventType = "transfer_failed"
	EventTransferQueued    EventType = "transfer_queued"
	EventQueueEnqueued     EventType = "queue_enqueued"
	EventQueueConnected    EventType = "queue_connected"
	EventQueueAbandoned    EventType = "queue_abandoned"
	EventQueueOverflow     EventType = "queue_overflow"
	EventCallLinked        EventType = "call_linked"
)

func (t EventType) Valid() bool {
	switch t {
	case EventAssigned, EventAutoAssigned, EventReplied, EventReopened,
		EventTagAdded, EventTagRemoved, EventLabelAdded, EventLabelRemoved,
		EventUnassigned, EventAIReplied, EventAIEnabled, EventAIDisabled,
		EventAISessionStarted, EventAISessionEnded, EventStatusChanged,
		EventStageChanged, EventFinished, EventAnalysisCreated,
		EventTransferOffered, EventTransferAccepted, EventTransferDeclined,
		EventTransferCompleted, EventTransferFailed, EventTransferQueued,
		EventQueueEnqueued, EventQueueConnected, EventQueueAbandoned,
		EventQueueOverflow, EventCallLinked:
		return true
	}
	return false
}

type ConversationEvent struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	EntryID       string     `json:"entry_id"`
	EntryType     string     `json:"entry_type"`
	EventType     EventType  `json:"event_type"`
	ActorID       string     `json:"actor_id"`
	ActorKind     actor.Kind `json:"actor_kind,omitempty"`
	Channel       string     `json:"channel,omitempty"`
	CorrelationID string     `json:"correlation_id,omitempty"`
	Details       string     `json:"details,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Validate reports whether the event can be persisted. workspace_id and entry_id
// map to `uuid NOT NULL` columns, so an empty value is a permanent data error
// (Postgres 22P02) that no retry can fix — the exact poison that looped forever.
// It returns ErrInvalidEvent so the emitter drops it before publishing and the
// consumer drops it instead of requeuing. Non-empty-but-malformed values (rare;
// producers pass real UUIDs or empty) are caught downstream by the consumer's
// permanent-SQLSTATE (22/23) classifier, so this stays a cheap non-empty check.
func (e *ConversationEvent) Validate() error {
	if e == nil {
		return ErrInvalidEvent
	}
	if strings.TrimSpace(e.WorkspaceID) == "" {
		return errors.Join(ErrInvalidEvent, errors.New(`empty workspace_id`))
	}
	if strings.TrimSpace(e.EntryID) == "" {
		return errors.Join(ErrInvalidEvent, errors.New(`empty entry_id`))
	}
	if e.EventType != "" && !e.EventType.Valid() {
		return errors.Join(ErrInvalidEvent, errors.New("unknown event_type: "+string(e.EventType)))
	}
	return nil
}

// Normalize fills actor_kind from actor_id when missing.
func (e *ConversationEvent) Normalize() {
	if e == nil {
		return
	}
	if e.ActorKind == "" || !e.ActorKind.Valid() {
		e.ActorKind = actor.KindOf(e.ActorID)
	}
	k, id := actor.Normalize(e.ActorKind, e.ActorID)
	e.ActorKind = k
	e.ActorID = id
}

type Repository interface {
	Create(event *ConversationEvent) error
	ListByEntry(workspaceID, entryID, entryType string, limit, offset int) ([]*ConversationEvent, int64, error)
	ListByEntryFiltered(workspaceID, entryID, entryType string, filter ListFilter) ([]*ConversationEvent, int64, error)
}

// ListFilter is optional; zero values mean no filter.
type ListFilter struct {
	Limit     int
	Offset    int
	ActorKind actor.Kind
	EventType EventType
	Since     *time.Time
}

type Logger interface {
	Log(event *ConversationEvent)
}

type ListEventsUseCase interface {
	Execute(workspaceID, entryID, entryType string, limit, offset int) ([]*ConversationEvent, int64, error)
}

func DetailsJSON(kv map[string]string) string {
	if len(kv) == 0 {
		return ""
	}
	b, _ := json.Marshal(kv)
	return string(b)
}

// Builder constructs a ConversationEvent with correct actor normalization.
type Builder struct {
	ev ConversationEvent
}

func New(workspaceID, entryID, entryType string, eventType EventType) *Builder {
	return &Builder{ev: ConversationEvent{
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   entryType,
		EventType:   eventType,
		ActorKind:   actor.KindSystem,
		ActorID:     actor.SystemID,
	}}
}

func (b *Builder) WithActorHuman(userID string) *Builder {
	b.ev.ActorKind = actor.KindHuman
	b.ev.ActorID = userID
	return b
}

func (b *Builder) WithActorAI(agentID string) *Builder {
	b.ev.ActorKind = actor.KindAI
	b.ev.ActorID = actor.FormatAI(agentID)
	return b
}

func (b *Builder) WithActorSystem() *Builder {
	b.ev.ActorKind = actor.KindSystem
	b.ev.ActorID = actor.SystemID
	return b
}

func (b *Builder) WithChannel(channel string) *Builder {
	b.ev.Channel = channel
	return b
}

func (b *Builder) WithCorrelation(id string) *Builder {
	b.ev.CorrelationID = id
	return b
}

func (b *Builder) WithDetails(kv map[string]string) *Builder {
	b.ev.Details = DetailsJSON(kv)
	return b
}

func (b *Builder) Build() *ConversationEvent {
	ev := b.ev
	ev.Normalize()
	return &ev
}
