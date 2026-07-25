package ai_attendance_usecase

import (
	"time"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/crm_telemetry"
)

// AsyncSessionService is the hot-path facade: publishes only, no DB.
// Consumer runs SessionService against Postgres.
type AsyncSessionService struct {
	pub crm_telemetry.Publisher
}

func NewAsyncSessionService(pub crm_telemetry.Publisher) *AsyncSessionService {
	return &AsyncSessionService{pub: pub}
}

func (s *AsyncSessionService) RecordAIReply(in aa.StartInput, messageID string) {
	if s == nil || s.pub == nil {
		return
	}
	_ = s.pub.Publish(crm_telemetry.KindAISession, crm_telemetry.AISessionPayload{
		Op:          crm_telemetry.AISessionOpRecordReply,
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		AgentID:     in.AgentID,
		Channel:     in.Channel,
		CallID:      in.CallID,
		CampaignID:  in.CampaignID,
		Model:       in.Model,
		MessageID:   messageID,
	})
}

func (s *AsyncSessionService) EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string) {
	if s == nil || s.pub == nil {
		return
	}
	_ = s.pub.Publish(crm_telemetry.KindAISession, crm_telemetry.AISessionPayload{
		Op:                  crm_telemetry.AISessionOpEndOpen,
		WorkspaceID:         workspaceID,
		EntryID:             entryID,
		EntryType:           entryType,
		Outcome:             outcome,
		Reason:              reason,
		HandoffTargetUserID: handoffUserID,
	})
}

func (s *AsyncSessionService) TouchInbound(workspaceID, entryID, entryType string) {
	if s == nil || s.pub == nil {
		return
	}
	_ = s.pub.Publish(crm_telemetry.KindAISession, crm_telemetry.AISessionPayload{
		Op:          crm_telemetry.AISessionOpTouchInbound,
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   entryType,
	})
}

// Ensure compatibility with SetAIAttendance(*SessionService) — WhatsApp use case
// should switch to interface. Provide adapter methods matching SessionService surface
// used on hot path only.

func (s *AsyncSessionService) EnsureOpen(in aa.StartInput) *aa.Session {
	// No DB: fire a record-reply-less open via publish is not enough for return value.
	// Hot path must not need the session row. Publish is done on RecordAIReply.
	_ = in
	return nil
}

func (s *AsyncSessionService) EndOpen(workspaceID, entryID, entryType string, outcome aa.Outcome, reason, handoffUserID string) {
	s.EndOpenRaw(workspaceID, entryID, entryType, string(outcome), reason, handoffUserID)
}

// idle is unused but keeps imports honest if we add timestamps later.
var _ = time.Time{}
