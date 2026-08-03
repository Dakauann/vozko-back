package conversation_usecase

import (
	"context"
	"log"
	"time"

	"vozko/domain/conversation"
	conv_event "vozko/domain/conversation_event"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// AISessionEnder ends open AI attendance sessions (queue publish on hot path).
type AISessionEnder interface {
	EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string)
}

// ConversationStatusStore is the per-channel read/write port for conversation
// status. Declared here rather than importing each channel's domain, so the
// service stays channel-agnostic and testable with a plain fake.
type ConversationStatusStore interface {
	// Status reads the current status, or "" when the entry has none.
	Status(ctx context.Context, entryID string) (string, error)
	// SetStatus writes the status and its close provenance. A nil closedAt with
	// empty source/reason clears the provenance, which is how a reopen is
	// expressed.
	SetStatus(ctx context.Context, entryID, status, closeSource, closeReason string, closedAt *time.Time) error
}

type ConversationStatusService struct {
	whatsappRepo wce.Repository
	// stores carries the same conversation-status contract as the WhatsApp entry
	// repository, keyed by entry type. Registering one never displaces another,
	// and a channel that is switched off simply has no entry.
	//
	// WhatsApp deliberately keeps its own branch below: its write goes through a
	// distinct struct, and lifting it would mean changing a working revenue path
	// for no behavioural gain. That is the strangler order documented in
	// domain/channel/channel.go.
	stores map[shared.EntryType]ConversationStatusStore
	// counters supply the per-status counts shown in the inbox header, keyed by
	// entry type. Separate from stores because a channel can carry status without
	// being able to count it cheaply.
	counters map[shared.EntryType]ConversationStatusCounter
	events   conv_event.Logger
	// resolveWorkspace is optional; when set, status_changed events are logged with workspace scope.
	resolveWorkspace func(entryID, entryType string) string
	// aiSessions ends open AI sessions when a conversation is marked finished (contained).
	aiSessions AISessionEnder
}

// ConversationStatusCounter returns per-status conversation counts, scoped to a
// container (accountID) when given, else to the whole workspace.
type ConversationStatusCounter func(ctx context.Context, workspaceID, accountID string) (map[string]int64, error)

// SetConversationCounter registers a channel's status counter.
func (s *ConversationStatusService) SetConversationCounter(entryType shared.EntryType, counter ConversationStatusCounter) {
	if s == nil || counter == nil || entryType == "" {
		return
	}
	if s.counters == nil {
		s.counters = make(map[shared.EntryType]ConversationStatusCounter, 2)
	}
	s.counters[entryType] = counter
}

// SetConversationStatusStore registers a channel's status store.
func (s *ConversationStatusService) SetConversationStatusStore(entryType shared.EntryType, store ConversationStatusStore) {
	if s == nil || store == nil || entryType == "" {
		return
	}
	if s.stores == nil {
		s.stores = make(map[shared.EntryType]ConversationStatusStore, 2)
	}
	s.stores[entryType] = store
}

func (s *ConversationStatusService) storeFor(entryType string) (ConversationStatusStore, bool) {
	if s == nil || s.stores == nil {
		return nil, false
	}
	store, ok := s.stores[shared.EntryType(entryType)]
	return store, ok && store != nil
}

func NewConversationStatusService(whatsappRepo wce.Repository) *ConversationStatusService {
	return &ConversationStatusService{
		whatsappRepo: whatsappRepo,
	}
}

func (s *ConversationStatusService) SetEventLogger(l conv_event.Logger) {
	s.events = l
}

func (s *ConversationStatusService) SetWorkspaceResolver(fn func(entryID, entryType string) string) {
	s.resolveWorkspace = fn
}

func (s *ConversationStatusService) SetAISessionEnder(e AISessionEnder) {
	if s != nil {
		s.aiSessions = e
	}
}

func (s *ConversationStatusService) GetConversationStatus(entryID, entryType string) conversation.ConversationStatus {
	if entryType == string(shared.EntryTypeWhatsApp) {
		if e, err := s.whatsappRepo.FindByID(entryID); err == nil && e != nil {
			return conversation.ConversationStatus(e.ConversationStatus)
		}
		return ""
	}
	if store, ok := s.storeFor(entryType); ok {
		if status, err := store.Status(context.Background(), entryID); err == nil {
			return conversation.ConversationStatus(status)
		}
	}
	return ""
}

// SetConversationStatus applies a status change. When target is finished without
// going through Finish, stamps human/manual (WS path convenience).
func (s *ConversationStatusService) SetConversationStatus(entryID, entryType string, status conversation.ConversationStatus) error {
	if status == conversation.ConversationStatusFinished {
		return s.Finish(entryID, entryType, conversation.FinishOptions{
			Source: conversation.CloseSourceHuman,
			Reason: conversation.CloseReasonManual,
		})
	}
	return s.applyStatus(entryID, entryType, status, false, "", "", false)
}

// Finish is the single choke point for moving to finished with provenance.
func (s *ConversationStatusService) Finish(entryID, entryType string, opts conversation.FinishOptions) error {
	source := opts.Source
	reason := opts.Reason
	if !source.Valid() {
		source = conversation.CloseSourceHuman
	}
	if !reason.Valid() {
		switch source {
		case conversation.CloseSourceAI:
			reason = conversation.CloseReasonAIResolved
		case conversation.CloseSourceSystem:
			// Default system path is customer idle; max_age must pass reason explicitly.
			reason = conversation.CloseReasonCustomerIdle
		default:
			reason = conversation.CloseReasonManual
		}
	}
	return s.applyStatusActor(entryID, entryType, conversation.ConversationStatusFinished, true, source, reason, false, opts.ActorID)
}

func (s *ConversationStatusService) applyStatus(
	entryID, entryType string,
	status conversation.ConversationStatus,
	setClose bool,
	source conversation.CloseSource,
	reason conversation.CloseReason,
	clearClose bool,
) error {
	return s.applyStatusActor(entryID, entryType, status, setClose, source, reason, clearClose, "")
}

// applyStatusActor is applyStatus plus the acting user/agent id, threaded through
// to the timeline event so human closes are attributable.
func (s *ConversationStatusService) applyStatusActor(
	entryID, entryType string,
	status conversation.ConversationStatus,
	setClose bool,
	source conversation.CloseSource,
	reason conversation.CloseReason,
	clearClose bool,
	actorID string,
) error {
	from := s.GetConversationStatus(entryID, entryType)
	// Idempotent finish: already finished with same status, no-op side effects.
	if from == status && status == conversation.ConversationStatusFinished && !clearClose {
		return nil
	}

	var err error
	switch {
	case entryType == string(shared.EntryTypeWhatsApp):
		write := wce.ConversationStatusWrite{
			Status:         string(status),
			SetCloseMeta:   setClose,
			ClearCloseMeta: clearClose,
		}
		if setClose {
			now := time.Now().UTC()
			write.CloseSource = string(source)
			write.CloseReason = string(reason)
			write.ClosedAt = &now
		}
		err = s.whatsappRepo.UpdateConversationStatus(entryID, write)

	default:
		store, ok := s.storeFor(entryType)
		if !ok {
			return nil
		}
		var closedAt *time.Time
		closeSource, closeReason := "", ""
		if setClose {
			now := time.Now().UTC()
			closedAt = &now
			closeSource = string(source)
			closeReason = string(reason)
		}
		// clearClose reopens the conversation, so the close provenance is wiped
		// rather than left pointing at a stale closure.
		err = store.SetStatus(context.Background(), entryID, string(status), closeSource, closeReason, closedAt)
	}
	if err != nil {
		return err
	}
	if string(from) != string(status) {
		// One workspace resolve for both event + AI session end (avoids N×2 on batch finish).
		wsID := ""
		if s.resolveWorkspace != nil {
			wsID = s.resolveWorkspace(entryID, entryType)
		}
		s.emitStatusChanged(entryID, entryType, string(from), string(status), source, reason, wsID, actorID)
		if status == conversation.ConversationStatusFinished {
			s.endAISessionContained(entryID, entryType, wsID)
		}
	}
	return nil
}

func (s *ConversationStatusService) endAISessionContained(entryID, entryType, workspaceID string) {
	if s == nil || s.aiSessions == nil {
		return
	}
	if workspaceID == "" {
		return
	}
	s.aiSessions.EndOpenRaw(workspaceID, entryID, entryType, "contained", "conversation_finished", "")
}

func (s *ConversationStatusService) emitStatusChanged(entryID, entryType, from, to string, source conversation.CloseSource, reason conversation.CloseReason, workspaceID, actorID string) {
	if s.events == nil {
		return
	}
	wsID := workspaceID
	if wsID == "" {
		return
	}
	evType := conv_event.EventStatusChanged
	if to == string(conversation.ConversationStatusFinished) {
		evType = conv_event.EventFinished
	}
	if to == string(conversation.ConversationStatusNew) && from == string(conversation.ConversationStatusFinished) {
		evType = conv_event.EventReopened
	}
	// The entry type IS the channel here, every value in the messaging set maps
	// 1:1 onto a MessageChannel. It was hardcoded to "whatsapp", which labelled
	// every Instagram close as a WhatsApp event on the timeline.
	channel := entryType
	details := map[string]string{"from": from, "to": to}
	if to == string(conversation.ConversationStatusFinished) && source.Valid() {
		details["close_source"] = string(source)
		details["close_reason"] = string(reason)
	}
	builder := conv_event.New(wsID, entryID, entryType, evType).
		WithChannel(channel).
		WithDetails(details)
	// Attribute the event to whoever actually acted. Previously both branches of
	// this condition called WithActorSystem(), so every close - including agent
	// clicks - was logged as actor_kind=system with no actor_id, making "who
	// finalized this?" unanswerable on the timeline.
	switch {
	case source == conversation.CloseSourceHuman && actorID != "":
		builder = builder.WithActorHuman(actorID)
	case source == conversation.CloseSourceAI:
		// Kind matters even when the agent id is unknown, so the timeline's AI
		// filter catches it. FormatAI("") is empty-safe.
		builder = builder.WithActorAI(actorID)
	default:
		builder = builder.WithActorSystem()
	}
	s.events.Log(builder.Build())
}

func (s *ConversationStatusService) TransitionOnMessage(entryID, entryType string, msgType conversation.MessageType) error {
	current := s.GetConversationStatus(entryID, entryType)

	if msgType.IsInbound() {
		switch current {
		case "", conversation.ConversationStatusFinished:
			// Reopen: finished → new and clear close provenance on the entry.
			return s.applyStatus(entryID, entryType, conversation.ConversationStatusNew, false, "", "", true)
		}
		return nil
	}

	switch msgType {
	case conversation.MessageTypeOperator, conversation.MessageTypeAIResponse, conversation.MessageTypeTemplate:
		if current == conversation.ConversationStatusNew || current == "" {
			return s.applyStatus(entryID, entryType, conversation.ConversationStatusOngoing, false, "", "", false)
		}
	}

	return nil
}

var _ conversation.ConversationStatusUpdater = (*ConversationStatusService)(nil)

func (s *ConversationStatusService) GetStatusCounts(workspaceID, campaignID, entryType string) (map[string]int64, error) {
	counts := map[string]int64{"new": 0, "ongoing": 0, "finished": 0}

	merge := func(src map[string]int64) {
		for k, v := range src {
			counts[k] += v
		}
	}

	// An empty entryType means "every channel". The counts are what the inbox
	// header shows above the list, so a channel missing from here reads as "there
	// is no work on this channel" while its conversations sit in the list below,
	// which is exactly what happened to Instagram.
	includeWhatsApp := entryType == "" || entryType == string(shared.EntryTypeWhatsApp)

	if includeWhatsApp {
		var waCounts map[string]int64
		var err error
		if campaignID != "" {
			waCounts, err = s.whatsappRepo.CountByConversationStatus(campaignID)
		} else if workspaceID != "" {
			waCounts, err = s.whatsappRepo.CountByConversationStatusForWorkspace(workspaceID)
		}
		if err != nil {
			return nil, err
		}
		if waCounts != nil {
			merge(waCounts)
		}
	}

	for channelType, count := range s.counters {
		if entryType != "" && entryType != string(channelType) {
			continue
		}
		// campaignID is the container id for channels with no campaign concept:
		// the account row. Passing it through unchanged is what makes a
		// per-account inbox count the right conversations.
		channelCounts, err := count(context.Background(), workspaceID, campaignID)
		if err != nil {
			// One channel's failure must not blank the whole header.
			log.Printf("[ConversationStatus] %s status counts failed: %v", channelType, err)
			continue
		}
		merge(channelCounts)
	}

	return counts, nil
}
