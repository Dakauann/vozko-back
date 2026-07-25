package ai_attendance_usecase

import (
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	aa "vozko/domain/ai_attendance"
	ce "vozko/domain/conversation_event"
)

// SessionService manages AI attendant sessions without affecting product UX.
type SessionService struct {
	repo   aa.Repository
	events ce.Logger
}

func NewSessionService(repo aa.Repository, events ce.Logger) *SessionService {
	return &SessionService{repo: repo, events: events}
}

// EnsureOpen returns an existing open session or starts a new one.
func (s *SessionService) EnsureOpen(in aa.StartInput) *aa.Session {
	if s == nil || s.repo == nil {
		return nil
	}
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.EntryID = strings.TrimSpace(in.EntryID)
	in.EntryType = strings.TrimSpace(in.EntryType)
	in.AgentID = strings.TrimSpace(in.AgentID)
	if in.WorkspaceID == "" || in.EntryID == "" || in.EntryType == "" || in.AgentID == "" {
		return nil
	}
	existing, err := s.repo.FindOpenByEntry(in.WorkspaceID, in.EntryID, in.EntryType)
	if err != nil {
		log.Printf("[AIAttendance] FindOpenByEntry: %v", err)
		return nil
	}
	if existing != nil {
		return existing
	}
	now := time.Now().UTC()
	sess := &aa.Session{
		ID:          uuid.New().String(),
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		AgentID:     in.AgentID,
		Channel:     in.Channel,
		CallID:      in.CallID,
		CampaignID:  in.CampaignID,
		Model:       in.Model,
		StartedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(sess); err != nil {
		log.Printf("[AIAttendance] Create session: %v", err)
		return nil
	}
	if s.events != nil {
		s.events.Log(ce.New(in.WorkspaceID, in.EntryID, in.EntryType, ce.EventAISessionStarted).
			WithActorAI(in.AgentID).
			WithChannel(in.Channel).
			WithCorrelation(sess.ID).
			WithDetails(map[string]string{
				"session_id": sess.ID,
				"agent_id":   in.AgentID,
			}).
			Build())
	}
	return sess
}

// RecordAIReply increments AI message count and ensures a session exists.
func (s *SessionService) RecordAIReply(in aa.StartInput, messageID string) {
	sess := s.EnsureOpen(in)
	if sess == nil {
		return
	}
	sess.AIMessageCount++
	sess.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(sess); err != nil {
		log.Printf("[AIAttendance] Update after AI reply: %v", err)
	}
	if s.events != nil {
		s.events.Log(ce.New(in.WorkspaceID, in.EntryID, in.EntryType, ce.EventAIReplied).
			WithActorAI(in.AgentID).
			WithChannel(in.Channel).
			WithCorrelation(messageID).
			WithDetails(map[string]string{
				"message_id": messageID,
				"session_id": sess.ID,
				"agent_id":   in.AgentID,
			}).
			Build())
	}
}

// EndOpen ends the open session for an entry (idempotent if none).
// outcome may be a domain Outcome string or raw string for hub adapters.
func (s *SessionService) EndOpen(workspaceID, entryID, entryType string, outcome aa.Outcome, reason, handoffUserID string) {
	s.endOpen(workspaceID, entryID, entryType, "", outcome, reason, handoffUserID)
}

// EndOpenWithCallID is EndOpen with optional call_id fallback (voice handoff / complete).
func (s *SessionService) EndOpenWithCallID(workspaceID, entryID, entryType, callID string, outcome aa.Outcome, reason, handoffUserID string) {
	s.endOpen(workspaceID, entryID, entryType, callID, outcome, reason, handoffUserID)
}

// EndOpenRaw accepts string outcome for delivery-layer adapters without importing domain outcomes.
func (s *SessionService) EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string) {
	s.endOpen(workspaceID, entryID, entryType, "", aa.Outcome(outcome), reason, handoffUserID)
}

// EndOpenRawWithCall ends open session by entry or call_id.
func (s *SessionService) EndOpenRawWithCall(workspaceID, entryID, entryType, callID, outcome, reason, handoffUserID string) {
	s.endOpen(workspaceID, entryID, entryType, callID, aa.Outcome(outcome), reason, handoffUserID)
}

func (s *SessionService) endOpen(workspaceID, entryID, entryType, callID string, outcome aa.Outcome, reason, handoffUserID string) {
	if s == nil || s.repo == nil {
		return
	}
	workspaceID = strings.TrimSpace(workspaceID)
	entryID = strings.TrimSpace(entryID)
	entryType = strings.TrimSpace(entryType)
	callID = strings.TrimSpace(callID)

	var sess *aa.Session
	var err error
	if entryID != "" && entryType != "" {
		sess, err = s.repo.FindOpenByEntry(workspaceID, entryID, entryType)
		if err != nil {
			log.Printf("[AIAttendance] FindOpenByEntry: %v", err)
			return
		}
	}
	// Handoff / complete often only has call_id while session was opened on campaign entry.
	if sess == nil && callID != "" {
		sess, err = s.repo.FindOpenByCallID(workspaceID, callID)
		if err != nil {
			log.Printf("[AIAttendance] FindOpenByCallID: %v", err)
			return
		}
	}
	// Last resort: treat entryID as call_id (emit fallback entry=callID).
	if sess == nil && entryID != "" && entryID != callID {
		sess, err = s.repo.FindOpenByCallID(workspaceID, entryID)
		if err != nil || sess == nil {
			return
		}
	}
	if sess == nil {
		return
	}
	now := time.Now().UTC()
	if err := sess.End(outcome, reason, handoffUserID, now); err != nil {
		return
	}
	if err := s.repo.Update(sess); err != nil {
		log.Printf("[AIAttendance] End session: %v", err)
		return
	}
	logEntryID := entryID
	if logEntryID == "" {
		logEntryID = sess.EntryID
	}
	logEntryType := entryType
	if logEntryType == "" {
		logEntryType = sess.EntryType
	}
	if s.events != nil {
		s.events.Log(ce.New(workspaceID, logEntryID, logEntryType, ce.EventAISessionEnded).
			WithActorAI(sess.AgentID).
			WithChannel(sess.Channel).
			WithCorrelation(sess.ID).
			WithDetails(map[string]string{
				"session_id": sess.ID,
				"outcome":    string(outcome),
				"reason":     reason,
				"handoff_to": handoffUserID,
				"call_id":    callID,
			}).
			Build())
	}
}

// TouchInbound increments inbound count on open session.
func (s *SessionService) TouchInbound(workspaceID, entryID, entryType string) {
	if s == nil || s.repo == nil {
		return
	}
	sess, err := s.repo.FindOpenByEntry(workspaceID, entryID, entryType)
	if err != nil || sess == nil {
		return
	}
	sess.InboundMessageCount++
	sess.UpdatedAt = time.Now().UTC()
	_ = s.repo.Update(sess)
}
