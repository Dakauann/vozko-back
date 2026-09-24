package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
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
	SetStatus(ctx context.Context, entryID string, write conversation.StatusWrite) error
}

type OutcomeCaptureReader interface {
	OutcomeCaptureFor(ctx context.Context, workspaceID string) (*conversation.OutcomeCapture, error)
}

type EntryDepartmentResolver interface {
	DepartmentIDForEntry(ctx context.Context, entryID, entryType string) (string, error)
}

type ConversationStatusService struct {
	whatsappRepo     wce.Repository
	stores           map[shared.EntryType]ConversationStatusStore
	counters         map[shared.EntryType]ConversationStatusCounter
	events           conv_event.Logger
	resolveWorkspace func(entryID, entryType string) string
	aiSessions       AISessionEnder
	outcomes         OutcomeCaptureReader
	announcer        StatusAnnouncer
	departments      EntryDepartmentResolver
	now              func() time.Time
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

// StatusAnnouncer tells open screens that a conversation's status changed, and
// for a finish, how it was closed.
type StatusAnnouncer interface {
	AnnounceStatus(entryID, entryType string, status conversation.ConversationStatus, close conversation.CloseRecord)
}

// SetStatusAnnouncer makes every status change, whoever makes it, reach open
// screens from this one place.
func (s *ConversationStatusService) SetStatusAnnouncer(a StatusAnnouncer) {
	if s != nil {
		s.announcer = a
	}
}

func (s *ConversationStatusService) SetOutcomeCaptureReader(r OutcomeCaptureReader) {
	if s != nil {
		s.outcomes = r
	}
}

func (s *ConversationStatusService) SetEntryDepartmentResolver(r EntryDepartmentResolver) {
	if s != nil {
		s.departments = r
	}
}

func (s *ConversationStatusService) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.now = clock
	}
}

func (s *ConversationStatusService) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now()
}

var ErrOutcomeWorkspaceUnknown = errors.New("conversation: cannot resolve the workspace that owns this conversation")

func (s *ConversationStatusService) OutcomeCapture(entryID, entryType string) (*conversation.OutcomeCapture, error) {
	if s == nil || s.outcomes == nil {
		return nil, nil
	}
	if s.resolveWorkspace == nil {
		return nil, ErrOutcomeWorkspaceUnknown
	}
	workspaceID := s.resolveWorkspace(entryID, entryType)
	if workspaceID == "" {
		return nil, ErrOutcomeWorkspaceUnknown
	}
	return s.outcomes.OutcomeCaptureFor(context.Background(), workspaceID)
}

// closedOutcome is the outcome a finish records, with the label it had when it
// was chosen (the catalogue may be renamed later).
type closedOutcome struct {
	code  string
	label string
}

func (s *ConversationStatusService) resolveOutcome(
	entryID, entryType string,
	source conversation.CloseSource,
	reason conversation.CloseReason,
	code string,
) (closedOutcome, error) {
	capture, err := s.OutcomeCapture(entryID, entryType)
	if err != nil {
		return closedOutcome{}, fmt.Errorf("conversation: reading the outcome capture policy: %w", err)
	}
	if capture == nil || !capture.Enabled {
		return closedOutcome{}, nil
	}

	departmentID := ""
	if len(capture.DepartmentIDs) > 0 {
		if s.departments == nil {
			return closedOutcome{}, conversation.ErrOutcomeRequired
		}
		departmentID, err = s.departments.DepartmentIDForEntry(context.Background(), entryID, entryType)
		if err != nil {
			return closedOutcome{}, fmt.Errorf("conversation: resolving the entry department for outcome capture: %w", err)
		}
	}
	resolved, err := capture.Resolve(source, reason, code, departmentID, s.clock())
	if err != nil || resolved == "" {
		return closedOutcome{}, err
	}
	out := closedOutcome{code: resolved}
	if o, found := capture.Lookup(resolved); found {
		out.label = o.Label
	}
	return out, nil
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
	return s.applyStatus(entryID, entryType, status, false, "", "", closedOutcome{}, false)
}

func normalizeCloseMeta(opts conversation.FinishOptions) (conversation.CloseSource, conversation.CloseReason) {
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
	return source, reason
}

func (s *ConversationStatusService) Finish(entryID, entryType string, opts conversation.FinishOptions) error {
	source, reason := normalizeCloseMeta(opts)
	outcome, err := s.resolveOutcome(entryID, entryType, source, reason, opts.OutcomeCode)
	if err != nil {
		return err
	}
	return s.applyStatusActor(entryID, entryType, conversation.ConversationStatusFinished, true, source, reason, outcome, false, opts.ActorID)
}

func (s *ConversationStatusService) applyStatus(
	entryID, entryType string,
	status conversation.ConversationStatus,
	setClose bool,
	source conversation.CloseSource,
	reason conversation.CloseReason,
	outcome closedOutcome,
	clearClose bool,
) error {
	return s.applyStatusActor(entryID, entryType, status, setClose, source, reason, outcome, clearClose, "")
}

func (s *ConversationStatusService) applyStatusActor(
	entryID, entryType string,
	status conversation.ConversationStatus,
	setClose bool,
	source conversation.CloseSource,
	reason conversation.CloseReason,
	outcome closedOutcome,
	clearClose bool,
	actorID string,
) error {
	from := s.GetConversationStatus(entryID, entryType)
	if from == status && status == conversation.ConversationStatusFinished && !clearClose {
		return nil
	}

	closedAt := s.clock()
	var err error
	switch {
	case entryType == string(shared.EntryTypeWhatsApp):
		write := wce.ConversationStatusWrite{
			Status:         string(status),
			SetCloseMeta:   setClose,
			ClearCloseMeta: clearClose,
		}
		if setClose {
			write.CloseSource = string(source)
			write.CloseReason = string(reason)
			write.CloseOutcome = outcome.code
			write.ClosedAt = &closedAt
		}
		err = s.whatsappRepo.UpdateConversationStatus(entryID, write)

	default:
		store, ok := s.storeFor(entryType)
		if !ok {
			return nil
		}
		write := conversation.StatusWrite{
			Status:         status,
			SetCloseMeta:   setClose,
			ClearCloseMeta: clearClose,
		}
		if setClose {
			write.CloseSource = source
			write.CloseReason = reason
			write.CloseOutcome = outcome.code
			write.ClosedAt = closedAt
		}
		err = store.SetStatus(context.Background(), entryID, write)
	}
	if err != nil {
		return err
	}
	if string(from) != string(status) {
		wsID := ""
		if s.resolveWorkspace != nil {
			wsID = s.resolveWorkspace(entryID, entryType)
		}
		s.emitStatusChanged(entryID, entryType, string(from), string(status), source, reason, outcome, wsID, actorID)
		if s.announcer != nil {
			var record conversation.CloseRecord
			if setClose {
				record = conversation.CloseRecord{Source: source, Reason: reason, Outcome: outcome.code, ClosedAt: &closedAt}
			}
			s.announcer.AnnounceStatus(entryID, entryType, status, record)
		}
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

func (s *ConversationStatusService) emitStatusChanged(entryID, entryType, from, to string, source conversation.CloseSource, reason conversation.CloseReason, outcome closedOutcome, workspaceID, actorID string) {
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
		if outcome.code != "" {
			details["close_outcome"] = outcome.code
			details["close_outcome_label"] = outcome.label
		}
	}
	builder := conv_event.New(wsID, entryID, entryType, evType).
		WithChannel(channel).
		WithDetails(details)
	switch {
	case actorID != "":
		builder = builder.WithActor(actorID)
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
			return s.applyStatus(entryID, entryType, conversation.ConversationStatusNew, false, "", "", closedOutcome{}, true)
		}
		return nil
	}

	if !answersTheCustomer(msgType) {
		return nil
	}
	if current == conversation.ConversationStatusNew || current == "" {
		return s.applyStatus(entryID, entryType, conversation.ConversationStatusOngoing, false, "", "", closedOutcome{}, false)
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
