package crm_telemetry_usecase

import (
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"

	"vozko/domain/actor"
	ce "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
)

// Emitter is the single facade hot paths use for timeline + ops telemetry.
// All methods are best-effort (never return product-failing errors).
type Emitter struct {
	pub crm_telemetry.Publisher
}

func NewEmitter(pub crm_telemetry.Publisher) *Emitter {
	return &Emitter{pub: pub}
}

func (e *Emitter) enabled() bool { return e != nil && e.pub != nil }

// ConversationEvent publishes a timeline event (queue only). This is the single
// choke point every timeline event flows through, so it is where a malformed
// event is dropped: an empty/non-UUID workspace_id or entry_id can never be
// persisted (uuid NOT NULL columns), and publishing it would poison the consumer
// (one such event looped forever, flooding the DB and logs). We drop it here so
// it never reaches the queue.
func (e *Emitter) ConversationEvent(ev *ce.ConversationEvent) {
	if !e.enabled() || ev == nil {
		return
	}
	if ev.ID == "" {
		ev.ID = uuid.New().String()
	}
	ev.Normalize()
	if err := ev.Validate(); err != nil {
		log.Printf("[crm_telemetry] dropping invalid conversation_event id=%s type=%s: %v", ev.ID, ev.EventType, err)
		return
	}
	_ = e.pub.Publish(crm_telemetry.KindConversationEvent, ev)
}

// Transfer publishes a transfer lifecycle timeline event.
func (e *Emitter) Transfer(workspaceID, entryID, entryType string, eventType ce.EventType, actorKind actor.Kind, actorID, transferID, callID, detailTarget, note string) {
	if !e.enabled() || workspaceID == "" {
		return
	}
	// entry may be empty for pure call transfers — still record under call correlation.
	if entryID == "" {
		entryID = callID
	}
	if entryType == "" {
		entryType = "voice"
	}
	b := ce.New(workspaceID, entryID, entryType, eventType).
		WithChannel("voice").
		WithCorrelation(transferID).
		WithDetails(map[string]string{
			"transfer_id": transferID,
			"call_id":     callID,
			"target":      detailTarget,
			"note":        note,
		})
	switch actorKind {
	case actor.KindAI:
		b = b.WithActorAI(actor.ParseAI(actorID))
		if actor.ParseAI(actorID) == "" {
			b = b.WithActorAI(actorID)
		}
	case actor.KindHuman:
		b = b.WithActorHuman(actorID)
	default:
		b = b.WithActorSystem()
	}
	e.ConversationEvent(b.Build())
}

// AnalysisCreated records analysis_created on the timeline.
func (e *Emitter) AnalysisCreated(workspaceID, entryID, entryType, analysisID, disposition string, quality int) {
	if !e.enabled() {
		return
	}
	details := map[string]string{
		"analysis_id": analysisID,
		"disposition": disposition,
	}
	if quality > 0 {
		details["attendance_quality"] = strconv.Itoa(quality)
	}
	e.ConversationEvent(ce.New(workspaceID, entryID, entryType, ce.EventAnalysisCreated).
		WithActorSystem().
		WithChannel(channelFor(entryType)).
		WithCorrelation(analysisID).
		WithDetails(details).
		Build())
}

// AIToggle records ai_enabled / ai_disabled.
func (e *Emitter) AIToggle(workspaceID, entryID, entryType, actorUserID string, enabled bool) {
	if !e.enabled() {
		return
	}
	et := ce.EventAIDisabled
	if enabled {
		et = ce.EventAIEnabled
	}
	e.ConversationEvent(ce.New(workspaceID, entryID, entryType, et).
		WithActorHuman(actorUserID).
		WithChannel(channelFor(entryType)).
		WithDetails(map[string]string{"enabled": boolStr(enabled)}).
		Build())
}

// Presence publishes a presence transition.
func (e *Emitter) Presence(workspaceID, userID, state, source string) {
	if !e.enabled() {
		return
	}
	_ = e.pub.Publish(crm_telemetry.KindPresence, crm_telemetry.PresencePayload{
		WorkspaceID: workspaceID,
		UserID:      userID,
		State:       state,
		Source:      source,
		At:          time.Now().UTC(),
	})
}

// AISession publishes an AI attendance op.
func (e *Emitter) AISession(p crm_telemetry.AISessionPayload) {
	if !e.enabled() {
		return
	}
	_ = e.pub.Publish(crm_telemetry.KindAISession, p)
}

func (e *Emitter) AISessionEndContainedChat(workspaceID, entryID, entryType, reason string) {
	if !e.enabled() || workspaceID == "" || entryID == "" {
		return
	}
	if entryType == "" {
		entryType = "whatsapp"
	}
	e.AISession(crm_telemetry.AISessionPayload{
		Op:          crm_telemetry.AISessionOpEndOpen,
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   entryType,
		Outcome:     "contained",
		Reason:      reason,
	})
}

// CallLinked records call_linked when a CDR is associated with an entry.
func (e *Emitter) CallLinked(workspaceID, entryID, entryType, callID, direction, callType string) {
	if !e.enabled() {
		return
	}
	e.ConversationEvent(ce.New(workspaceID, entryID, entryType, ce.EventCallLinked).
		WithActorSystem().
		WithChannel("voice").
		WithCorrelation(callID).
		WithDetails(map[string]string{
			"call_id":   callID,
			"direction": direction,
			"type":      callType,
		}).
		Build())
}

func channelFor(entryType string) string {
	switch entryType {
	case "voice":
		return "voice"
	case "support":
		return "support"
	default:
		return "whatsapp"
	}
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
