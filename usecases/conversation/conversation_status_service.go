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

type AISessionEnder interface {
	EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string)
}

type ConversationStatusStore interface {
	Status(ctx context.Context, entryID string) (string, error)
	SetStatus(ctx context.Context, entryID, status, closeSource, closeReason string, closedAt *time.Time) error
}

type ConversationStatusService struct {
	whatsappRepo     wce.Repository
	stores           map[shared.EntryType]ConversationStatusStore
	counters         map[shared.EntryType]ConversationStatusCounter
	events           conv_event.Logger
	resolveWorkspace func(entryID, entryType string) string
	aiSessions       AISessionEnder
}

type ConversationStatusCounter func(ctx context.Context, workspaceID, accountID string) (map[string]int64, error)

func (s *ConversationStatusService) SetConversationCounter(entryType shared.EntryType, counter ConversationStatusCounter) {
	if s == nil || counter == nil || entryType == "" {
		return
	}
	if s.counters == nil {
		s.counters = make(map[shared.EntryType]ConversationStatusCounter, 2)
	}
	s.counters[entryType] = counter
}

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

func (s *ConversationStatusService) SetConversationStatus(entryID, entryType string, status conversation.ConversationStatus) error {
	if status == conversation.ConversationStatusFinished {
		return s.Finish(entryID, entryType, conversation.FinishOptions{
			Source: conversation.CloseSourceHuman,
			Reason: conversation.CloseReasonManual,
		})
	}
	return s.applyStatus(entryID, entryType, status, false, "", "", false)
}

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
		err = store.SetStatus(context.Background(), entryID, string(status), closeSource, closeReason, closedAt)
	}
	if err != nil {
		return err
	}
	if string(from) != string(status) {
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
	channel := entryType
	details := map[string]string{"from": from, "to": to}
	if to == string(conversation.ConversationStatusFinished) && source.Valid() {
		details["close_source"] = string(source)
		details["close_reason"] = string(reason)
	}
	builder := conv_event.New(wsID, entryID, entryType, evType).
		WithChannel(channel).
		WithDetails(details)
	switch {
	case source == conversation.CloseSourceHuman && actorID != "":
		builder = builder.WithActorHuman(actorID)
	case source == conversation.CloseSourceAI:
		builder = builder.WithActorAI(actorID)
	default:
		builder = builder.WithActorSystem()
	}
	s.events.Log(builder.Build())
}

func (s *ConversationStatusService) TransitionOnMessage(
	entryID, entryType string,
	msgType conversation.MessageType,
	direction conversation.MessageHistoryDirection,
) error {
	current := s.GetConversationStatus(entryID, entryType)

	inbound := msgType.IsInbound()
	if direction.Valid() {
		inbound = !direction.IsOutbound()
	}

	if inbound {
		switch current {
		case "", conversation.ConversationStatusFinished:
			return s.applyStatus(entryID, entryType, conversation.ConversationStatusNew, false, "", "", true)
		}
		return nil
	}

	if !answersTheCustomer(msgType) {
		return nil
	}
	if current == conversation.ConversationStatusNew || current == "" {
		return s.applyStatus(entryID, entryType, conversation.ConversationStatusOngoing, false, "", "", false)
	}
	return nil
}

func answersTheCustomer(msgType conversation.MessageType) bool {
	switch msgType {
	case conversation.MessageTypeOperator,
		conversation.MessageTypeAIResponse,
		conversation.MessageTypeTemplate,
		conversation.MessageTypeUserMessage,
		conversation.MessageTypeAudio,
		conversation.MessageTypeMedia:
		return true
	}
	return false
}

var _ conversation.ConversationStatusUpdater = (*ConversationStatusService)(nil)

func (s *ConversationStatusService) GetStatusCounts(workspaceID, campaignID, entryType string) (map[string]int64, error) {
	counts := map[string]int64{"new": 0, "ongoing": 0, "finished": 0}

	merge := func(src map[string]int64) {
		for k, v := range src {
			counts[k] += v
		}
	}

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
		channelCounts, err := count(context.Background(), workspaceID, campaignID)
		if err != nil {
			log.Printf("[ConversationStatus] %s status counts failed: %v", channelType, err)
			continue
		}
		merge(channelCounts)
	}

	return counts, nil
}
