package ai_attendance_usecase

import (
	"time"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/crm_telemetry"
)

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

func (s *AsyncSessionService) End(request aa.EndRequest) {
	if s == nil || s.pub == nil {
		return
	}
	_ = s.pub.Publish(crm_telemetry.KindAISession, crm_telemetry.AISessionPayload{
		Op:                  crm_telemetry.AISessionOpEndOpen,
		WorkspaceID:         request.WorkspaceID,
		EntryID:             request.EntryID,
		EntryType:           request.EntryType,
		CallID:              request.CallID,
		Outcome:             string(request.Outcome),
		Reason:              request.Reason,
		HandoffTargetUserID: request.HandoffTo,
		EndedBy:             request.EndedBy,
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

func (s *AsyncSessionService) EnsureOpen(in aa.StartInput) *aa.Session {
	_ = in
	return nil
}

var _ aa.SessionEnder = (*AsyncSessionService)(nil)

var _ = time.Time{}
