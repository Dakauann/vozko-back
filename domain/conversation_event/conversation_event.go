package conversation_event

import (
	"encoding/json"
	"errors"
	"fmt"
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

	// Lead memory
	EventLeadMemoryCreated EventType = "lead_memory_created"
	EventLeadMemoryUpdated EventType = "lead_memory_updated"
	EventLeadMemoryDeleted EventType = "lead_memory_deleted"

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
		EventLeadMemoryCreated, EventLeadMemoryUpdated, EventLeadMemoryDeleted,
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

	// ActorName, FromName and ToName are display names resolved when the
	// timeline is READ. They are never persisted: the repository's toSchema
	// ignores them, so no migration and no new column.
	//
	// Stored events only ever carried ids — a `replied` event knows the
	// sender's uuid, an `assigned` event knows from_user_id/to_user_id — so the
	// timeline could say "a human replied" and "somebody was assigned" but
	// never who, or to whom. Resolving on read rather than at emit time keeps
	// the send/assign hot paths free of a user lookup, follows a rename, and,
	// the reason it is worth doing at all, fills in every event already stored.
	ActorName string `json:"actor_name,omitempty"`
	FromName  string `json:"from_name,omitempty"`
	ToName    string `json:"to_name,omitempty"`
}

// DetailsMap decodes the Details JSON blob into a flat string map.
//
// A malformed or empty blob yields an empty map: details are decoration on a
// timeline row, never a reason to fail the read.
func (e *ConversationEvent) DetailsMap() map[string]string {
	if e == nil || strings.TrimSpace(e.Details) == "" {
		return map[string]string{}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(e.Details), &raw); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
			continue
		}
		out[k] = fmt.Sprint(v)
	}
	return out
}

// FromActorIDKeys and ToActorIDKeys are the details keys that name the two
// sides of an ownership change.
//
// They are listed here, next to the events that write them, rather than in the
// resolver: the assignment service writes from_user_id/to_user_id, the voice
// transfer emitter writes target, and a new producer that spells it differently
// belongs in this list, not in a switch somewhere downstream.
var (
	FromActorIDKeys = []string{"from_user_id", "from_actor_id", "previous_user_id"}
	ToActorIDKeys   = []string{"to_user_id", "assigned_user_id", "to_actor_id", "target"}
)

// LookupDetailID returns the first non-empty value among keys.
func LookupDetailID(details map[string]string, keys []string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(details[k]); v != "" {
			return v
		}
	}
	return ""
}

// Validate reports whether the event can be persisted. workspace_id and entry_id
// map to `uuid NOT NULL` columns, so an empty value is a permanent data error
// (Postgres 22P02) that no retry can fix, the exact poison that looped forever.
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

// WithActor routes a stored actor id to the right kind: "ai:<id>" is an AI
// attendant, "system" or empty is the platform, anything else is a user.
//
// It is for producers that carry an actor id WITHOUT knowing which kind it is —
// a use case reached by an operator, the AI and a background job alike. Each of
// them had written this same three-way switch inline, and a producer that
// forgot it filed the AI's actions under a human id.
func (b *Builder) WithActor(actorID string) *Builder {
	switch actor.KindOf(actorID) {
	case actor.KindAI:
		return b.WithActorAI(actor.ParseAI(actorID))
	case actor.KindHuman:
		return b.WithActorHuman(actorID)
	default:
		return b.WithActorSystem()
	}
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
