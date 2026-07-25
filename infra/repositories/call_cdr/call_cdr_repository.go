package call_cdr_repository

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/calls/cdr"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) cdr.Repository {
	return &repository{db: db}
}

func (r *repository) Create(call *cdr.Call) error {
	if call == nil {
		return errors.New("call is required")
	}
	if call.ID == "" {
		call.ID = uuid.New().String()
	}
	s := toSchema(call)
	if err := r.db.Create(s).Error; err != nil {
		return err
	}
	call.CreatedAt = s.CreatedAt
	call.UpdatedAt = s.UpdatedAt
	return nil
}

func (r *repository) GetByID(id string) (*cdr.Call, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, cdr.ErrCallNotFound
	}
	var s schema.Call
	if err := r.db.Where("id = ?", id).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, cdr.ErrCallNotFound
		}
		return nil, err
	}
	return toDomain(&s), nil
}

func (r *repository) GetByCallID(callID string) (*cdr.Call, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, cdr.ErrCallIDRequired
	}
	var s schema.Call
	if err := r.db.Where("call_id = ?", callID).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, cdr.ErrCallNotFound
		}
		return nil, err
	}
	return toDomain(&s), nil
}

func (r *repository) Update(call *cdr.Call) error {
	if call == nil {
		return errors.New("call is required")
	}
	if call.ID == "" {
		return errors.New("call id is required")
	}
	s := toSchema(call)
	return r.db.Save(s).Error
}

func (r *repository) MarkAnswered(callID string, answeredAt time.Time) error {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return cdr.ErrCallIDRequired
	}
	if answeredAt.IsZero() {
		answeredAt = time.Now().UTC()
	}

	res := r.db.Model(&schema.Call{}).
		Where("call_id = ? AND answered_at IS NULL", callID).
		Updates(map[string]any{"answered_at": answeredAt})
	if res.Error != nil {
		return res.Error
	}

	return nil
}

func (r *repository) Complete(input cdr.CompleteInput) error {
	callID := strings.TrimSpace(input.CallID)
	if callID == "" {
		return cdr.ErrCallIDRequired
	}
	updates := map[string]any{
		"status":       string(input.Status),
		"duration_sec": input.DurationSec,
	}
	if input.EndedAt != nil {
		updates["ended_at"] = *input.EndedAt
	}
	if input.EndReason != nil {
		updates["end_reason"] = *input.EndReason
	}
	res := r.db.Model(&schema.Call{}).Where("call_id = ?", callID).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return cdr.ErrCallNotFound
	}
	return nil
}

func (r *repository) List(filters cdr.ListFilters) (*shared.PaginatedResult[*cdr.Call], error) {
	pagination := shared.NormalizePagination(filters.Pagination)

	query := r.db.Model(&schema.Call{}).Where("workspace_id = ?", filters.WorkspaceID)
	if filters.Type != nil {
		query = query.Where("type = ?", string(*filters.Type))
	}
	if filters.Direction != nil {
		query = query.Where("direction = ?", string(*filters.Direction))
	}
	if filters.Source != nil {
		query = query.Where("source = ?", string(*filters.Source))
	}
	if filters.Status != nil {
		query = query.Where("status = ?", string(*filters.Status))
	}
	if filters.AgentID != nil {
		query = query.Where("agent_id = ?", *filters.AgentID)
	}
	if filters.LeadID != nil {
		query = query.Where("lead_id = ?", *filters.LeadID)
	}
	if filters.StartedFrom != nil {
		query = query.Where("started_at >= ?", *filters.StartedFrom)
	}
	if filters.StartedTo != nil {
		query = query.Where("started_at <= ?", *filters.StartedTo)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var rows []schema.Call
	if err := query.
		Order("started_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*cdr.Call, len(rows))
	for i := range rows {
		items[i] = toDomain(&rows[i])
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func toSchema(d *cdr.Call) *schema.Call {
	return &schema.Call{
		ID:             d.ID,
		CallID:         d.CallID,
		WorkspaceID:    d.WorkspaceID,
		Type:           string(d.Type),
		Direction:      string(d.Direction),
		Source:         string(d.Source),
		Status:         string(d.Status),
		AgentID:        d.AgentID,
		ConversationID: d.ConversationID,
		LeadID:         d.LeadID,
		PhoneFrom:      d.PhoneFrom,
		PhoneTo:        d.PhoneTo,
		TrunkID:        d.TrunkID,
		ParentCallID:   d.ParentCallID,
		EndReason:      d.EndReason,
		StartedAt:      d.StartedAt,
		AnsweredAt:     d.AnsweredAt,
		EndedAt:        d.EndedAt,
		DurationSec:    d.DurationSec,
	}
}

func toDomain(s *schema.Call) *cdr.Call {
	c := &cdr.Call{
		ID:             s.ID,
		CallID:         s.CallID,
		WorkspaceID:    s.WorkspaceID,
		Type:           cdr.CallType(s.Type),
		Direction:      cdr.Direction(s.Direction),
		Source:         cdr.Source(s.Source),
		Status:         cdr.Status(s.Status),
		AgentID:        s.AgentID,
		ConversationID: s.ConversationID,
		LeadID:         s.LeadID,
		PhoneFrom:      s.PhoneFrom,
		PhoneTo:        s.PhoneTo,
		TrunkID:        s.TrunkID,
		ParentCallID:   s.ParentCallID,
		EndReason:      s.EndReason,
		StartedAt:      s.StartedAt,
		AnsweredAt:     s.AnsweredAt,
		EndedAt:        s.EndedAt,
		DurationSec:    s.DurationSec,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
	return c
}
