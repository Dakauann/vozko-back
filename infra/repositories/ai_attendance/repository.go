package ai_attendance_repository

import (
	"errors"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	aa "vozko/domain/ai_attendance"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) aa.Repository {
	return &repository{db: db}
}

func (r *repository) Create(s *aa.Session) error {
	rec := toSchema(s)
	if err := r.db.Create(rec).Error; err != nil {
		return err
	}
	s.ID = rec.ID
	s.CreatedAt = rec.CreatedAt
	s.UpdatedAt = rec.UpdatedAt
	return nil
}

func (r *repository) Update(s *aa.Session) error {
	rec := toSchema(s)
	return r.db.Save(rec).Error
}

func (r *repository) FindOpenByEntry(workspaceID, entryID, entryType string) (*aa.Session, error) {
	var rec schema.AIAttendanceSession
	err := r.db.Where("workspace_id = ? AND entry_id = ? AND entry_type = ? AND ended_at IS NULL", workspaceID, entryID, entryType).
		Order("started_at DESC").
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(&rec), nil
}

func (r *repository) FindOpenByCallID(workspaceID, callID string) (*aa.Session, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, nil
	}
	var rec schema.AIAttendanceSession
	err := r.db.Where("workspace_id = ? AND call_id = ? AND ended_at IS NULL", workspaceID, callID).
		Order("started_at DESC").
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(&rec), nil
}

func (r *repository) ExistsByCallID(workspaceID, callID string) (bool, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return false, nil
	}
	var n int64
	q := r.db.Model(&schema.AIAttendanceSession{}).Where("call_id = ?", callID)
	if strings.TrimSpace(workspaceID) != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if err := q.Limit(1).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *repository) FindByID(id string) (*aa.Session, error) {
	var rec schema.AIAttendanceSession
	err := r.db.Where("id = ?", id).First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, aa.ErrSessionNotFound
		}
		return nil, err
	}
	return toDomain(&rec), nil
}

func (r *repository) ListByEntry(workspaceID, entryID, entryType string, limit, offset int) ([]*aa.Session, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.db.Model(&schema.AIAttendanceSession{}).
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var recs []schema.AIAttendanceSession
	if err := q.Order("started_at DESC").Limit(limit).Offset(offset).Find(&recs).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*aa.Session, len(recs))
	for i := range recs {
		out[i] = toDomain(&recs[i])
	}
	return out, total, nil
}

func (r *repository) Stats(workspaceID string, from, to *time.Time) (*aa.Stats, error) {
	type row struct {
		Outcome string
		Cnt     int64
	}
	q := r.db.Model(&schema.AIAttendanceSession{}).
		Select("outcome, COUNT(*) AS cnt").
		Where("workspace_id = ? AND ended_at IS NOT NULL", workspaceID)
	if from != nil {
		q = q.Where("started_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("started_at <= ?", *to)
	}
	var rows []row
	if err := q.Group("outcome").Scan(&rows).Error; err != nil {
		return nil, err
	}
	st := &aa.Stats{}
	for _, rw := range rows {
		st.Total += rw.Cnt
		switch aa.Outcome(rw.Outcome) {
		case aa.OutcomeContained:
			st.Contained = rw.Cnt
		case aa.OutcomeHandedOff:
			st.HandedOff = rw.Cnt
		case aa.OutcomeAbandoned:
			st.Abandoned = rw.Cnt
		case aa.OutcomeError:
			st.Error = rw.Cnt
		case aa.OutcomeSuppressed:
			st.Suppressed = rw.Cnt
		}
	}
	if st.Total > 0 {
		st.ContainmentRate = math.Round(float64(st.Contained)/float64(st.Total)*10000) / 100
		st.HandoffRate = math.Round(float64(st.HandedOff)/float64(st.Total)*10000) / 100
	}
	return st, nil
}

func toDomain(rec *schema.AIAttendanceSession) *aa.Session {
	return &aa.Session{
		ID:                  rec.ID,
		WorkspaceID:         rec.WorkspaceID,
		EntryID:             rec.EntryID,
		EntryType:           rec.EntryType,
		AgentID:             rec.AgentID,
		Channel:             rec.Channel,
		CallID:              rec.CallID,
		CampaignID:          rec.CampaignID,
		StartedAt:           rec.StartedAt,
		EndedAt:             rec.EndedAt,
		Outcome:             aa.Outcome(rec.Outcome),
		HandoffTargetUserID: rec.HandoffTargetUserID,
		EndReason:           rec.EndReason,
		InboundMessageCount: rec.InboundMessageCount,
		AIMessageCount:      rec.AIMessageCount,
		ToolCallCount:       rec.ToolCallCount,
		Model:               rec.Model,
		CreatedAt:           rec.CreatedAt,
		UpdatedAt:           rec.UpdatedAt,
	}
}

func toSchema(s *aa.Session) *schema.AIAttendanceSession {
	return &schema.AIAttendanceSession{
		ID:                  s.ID,
		WorkspaceID:         s.WorkspaceID,
		EntryID:             s.EntryID,
		EntryType:           s.EntryType,
		AgentID:             s.AgentID,
		Channel:             s.Channel,
		CallID:              s.CallID,
		CampaignID:          s.CampaignID,
		StartedAt:           s.StartedAt,
		EndedAt:             s.EndedAt,
		Outcome:             string(s.Outcome),
		HandoffTargetUserID: s.HandoffTargetUserID,
		EndReason:           s.EndReason,
		InboundMessageCount: s.InboundMessageCount,
		AIMessageCount:      s.AIMessageCount,
		ToolCallCount:       s.ToolCallCount,
		Model:               s.Model,
	}
}
