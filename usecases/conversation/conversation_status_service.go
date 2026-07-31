package conversation_usecase

import (
	"context"
	"time"

	"vozko/domain/conversation"
	conv_event "vozko/domain/conversation_event"
	igdomain "vozko/domain/instagram"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// AISessionEnder ends open AI attendance sessions (queue publish on hot path).
type AISessionEnder interface {
	EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string)
}

type ConversationStatusService struct {
	whatsappRepo wce.Repository
	// instagramRepo carries the same conversation-status contract as the WhatsApp
	// entry repository. Optional so the service still works with the channel off.
	instagramRepo igdomain.ConversationRepository
	events        conv_event.Logger
	// resolveWorkspace is optional; when set, status_changed events are logged with workspace scope.
	resolveWorkspace func(entryID, entryType string) string
	// aiSessions ends open AI sessions when a conversation is marked finished (contained).
	aiSessions AISessionEnder
}

// SetInstagramRepo registers the Instagram conversation repository.
func (s *ConversationStatusService) SetInstagramRepo(repo igdomain.ConversationRepository) {
	if s != nil {
		s.instagramRepo = repo
	}
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
	switch entryType {
	case "whatsapp":
		if e, err := s.whatsappRepo.FindByID(entryID); err == nil && e != nil {
			return conversation.ConversationStatus(e.ConversationStatus)
		}
	case "instagram":
		if s.instagramRepo == nil {
			return ""
		}
		if c, err := s.instagramRepo.FindByID(context.Background(), entryID); err == nil && c != nil {
			return conversation.ConversationStatus(c.ConversationStatus)
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
	switch entryType {
	case "whatsapp":
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
	case "instagram":
		if s.instagramRepo == nil {
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
		err = s.instagramRepo.SetStatus(context.Background(), entryID, string(status), closeSource, closeReason, closedAt)
	default:
		return nil
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
	channel := "whatsapp"
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

	includeWhatsApp := entryType == "" || entryType == "whatsapp"

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

	return counts, nil
}
