package assignment_history_repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	ia "vozko/domain/inbox_assignment"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) ia.HistoryRepository {
	return &repository{db: db}
}

func (r *repository) CloseOpen(workspaceID, entryID, entryType string, endedAt time.Time) error {
	return r.db.Model(&schema.AssignmentHistory{}).
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ? AND ended_at IS NULL", workspaceID, entryID, entryType).
		Update("ended_at", endedAt).Error
}

func (r *repository) Append(h *ia.AssignmentHistory) error {
	if h.StartedAt.IsZero() {
		h.StartedAt = time.Now().UTC()
	}
	rec := toSchema(h)
	if err := r.db.Create(rec).Error; err != nil {
		return err
	}
	h.ID = rec.ID
	h.CreatedAt = rec.CreatedAt
	return nil
}

func (r *repository) GetOpen(workspaceID, entryID, entryType string) (*ia.AssignmentHistory, error) {
	var rec schema.AssignmentHistory
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

func (r *repository) ListByEntry(workspaceID, entryID, entryType string, limit, offset int) ([]*ia.AssignmentHistory, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.db.Model(&schema.AssignmentHistory{}).
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var recs []schema.AssignmentHistory
	if err := q.Order("started_at DESC").Limit(limit).Offset(offset).Find(&recs).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*ia.AssignmentHistory, len(recs))
	for i := range recs {
		out[i] = toDomain(&recs[i])
	}
	return out, total, nil
}

func toDomain(rec *schema.AssignmentHistory) *ia.AssignmentHistory {
	bp := ""
	if rec.BusinessPhoneID != nil {
		bp = *rec.BusinessPhoneID
	}
	return &ia.AssignmentHistory{
		ID:                rec.ID,
		WorkspaceID:       rec.WorkspaceID,
		EntryID:           rec.EntryID,
		EntryType:         rec.EntryType,
		ActorKind:         rec.ActorKind,
		AssignedActorID:   rec.AssignedActorID,
		PreviousActorID:   rec.PreviousActorID,
		Trigger:           rec.Trigger,
		AssignedByActorID: rec.AssignedByActorID,
		BusinessPhoneID:   bp,
		SIPTrunkID:        rec.SIPTrunkID,
		DepartmentID:      rec.DepartmentID,
		StartedAt:         rec.StartedAt,
		EndedAt:           rec.EndedAt,
		CreatedAt:         rec.CreatedAt,
	}
}

func toSchema(h *ia.AssignmentHistory) *schema.AssignmentHistory {
	rec := &schema.AssignmentHistory{
		ID:                h.ID,
		WorkspaceID:       h.WorkspaceID,
		EntryID:           h.EntryID,
		EntryType:         h.EntryType,
		ActorKind:         h.ActorKind,
		AssignedActorID:   h.AssignedActorID,
		PreviousActorID:   h.PreviousActorID,
		Trigger:           h.Trigger,
		AssignedByActorID: h.AssignedByActorID,
		SIPTrunkID:        h.SIPTrunkID,
		DepartmentID:      h.DepartmentID,
		StartedAt:         h.StartedAt,
		EndedAt:           h.EndedAt,
	}
	if h.BusinessPhoneID != "" {
		bp := h.BusinessPhoneID
		rec.BusinessPhoneID = &bp
	}
	return rec
}
