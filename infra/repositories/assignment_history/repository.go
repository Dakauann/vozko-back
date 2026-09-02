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

func (r *repository) ListOpenOlderThan(workspaceIDs []string, triggers []string, olderThan time.Time, limit int) ([]*ia.AssignmentHistory, error) {
	// An empty trigger set would render as IN (), which Postgres rejects, and
	// an empty workspace set has nothing to ask about. Both mean "no candidates"
	// rather than "everything".
	if len(workspaceIDs) == 0 || len(triggers) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var recs []schema.AssignmentHistory
	err := r.db.
		Where("workspace_id IN ? AND ended_at IS NULL AND trigger IN ? AND started_at < ?", workspaceIDs, triggers, olderThan).
		Order("started_at ASC").
		Limit(limit).
		Find(&recs).Error
	if err != nil {
		return nil, err
	}
	out := make([]*ia.AssignmentHistory, len(recs))
	for i := range recs {
		out[i] = toDomain(&recs[i])
	}
	return out, nil
}

// CountRescuesSinceHandout counts rescue hops since the roulette last handed
// this entry out.
//
// One query, not two. The rescue sweep calls this once per stalled
// conversation, so a second round-trip per candidate is a per-candidate cost on
// the one path that is already the most query-hungry in the feature.
//
// Reading the tail newest-first and stopping at the roulette hand-out is
// correct for any real chain — the hop cap is a single digit — and the LIMIT
// bounds an entry that has been reassigned by hand hundreds of times. An entry
// whose history holds no hand-out at all (it began as a manual assignment) has
// no rescue chain to bound, so it counts zero.
func (r *repository) CountRescuesSinceHandout(workspaceID, entryID, entryType string) (int, error) {
	type row struct {
		Trigger string `gorm:"column:trigger"`
	}
	var rows []row
	err := r.db.Model(&schema.AssignmentHistory{}).
		Select("trigger").
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType).
		Order("started_at DESC").
		Limit(rescueChainScanLimit).
		Scan(&rows).Error
	if err != nil {
		return 0, err
	}

	count := 0
	for _, item := range rows {
		switch item.Trigger {
		case ia.TriggerInboundRR:
			// The hand-out that opened the current chain: everything older
			// belongs to a previous one.
			return count, nil
		case ia.TriggerRescue:
			count++
		}
	}
	// No hand-out in the scanned tail: nothing to bound.
	return 0, nil
}

// rescueChainScanLimit bounds the tail read above. The hop cap is
// MaxRescueHops, so a real chain is at most a handful of rows; this only has to
// survive an entry somebody reassigned by hand many times.
const rescueChainScanLimit = 50
