package conversation_event_repository

import (
	"gorm.io/gorm"

	"vozko/domain/actor"
	ce "vozko/domain/conversation_event"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) ce.Repository {
	return &repository{db: db}
}

func (r *repository) Create(event *ce.ConversationEvent) error {
	if event != nil {
		event.Normalize()
	}
	rec := toSchema(event)
	return r.db.Create(&rec).Error
}

func (r *repository) ListByEntry(workspaceID, entryID, entryType string, limit, offset int) ([]*ce.ConversationEvent, int64, error) {
	return r.ListByEntryFiltered(workspaceID, entryID, entryType, ce.ListFilter{
		Limit:  limit,
		Offset: offset,
	})
}

func (r *repository) ListByEntryFiltered(workspaceID, entryID, entryType string, filter ce.ListFilter) ([]*ce.ConversationEvent, int64, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	q := r.db.Model(&schema.ConversationEvent{}).
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType)
	if filter.ActorKind.Valid() {
		q = q.Where("actor_kind = ?", string(filter.ActorKind))
	}
	if filter.EventType.Valid() {
		q = q.Where("event_type = ?", string(filter.EventType))
	}
	if filter.Since != nil {
		q = q.Where("created_at >= ?", *filter.Since)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var recs []schema.ConversationEvent
	err := q.Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&recs).Error
	if err != nil {
		return nil, 0, err
	}

	result := make([]*ce.ConversationEvent, len(recs))
	for i := range recs {
		result[i] = toDomain(&recs[i])
	}
	return result, total, nil
}

func toDomain(rec *schema.ConversationEvent) *ce.ConversationEvent {
	kind := actor.Kind(rec.ActorKind)
	if !kind.Valid() {
		kind = actor.KindOf(rec.ActorID)
	}
	return &ce.ConversationEvent{
		ID:            rec.ID,
		WorkspaceID:   rec.WorkspaceID,
		EntryID:       rec.EntryID,
		EntryType:     rec.EntryType,
		EventType:     ce.EventType(rec.EventType),
		ActorID:       rec.ActorID,
		ActorKind:     kind,
		Channel:       rec.Channel,
		CorrelationID: rec.CorrelationID,
		Details:       rec.Details,
		CreatedAt:     rec.CreatedAt,
	}
}

func toSchema(e *ce.ConversationEvent) *schema.ConversationEvent {
	kind := string(e.ActorKind)
	if kind == "" {
		kind = string(actor.KindOf(e.ActorID))
	}
	return &schema.ConversationEvent{
		ID:            e.ID,
		WorkspaceID:   e.WorkspaceID,
		EntryID:       e.EntryID,
		EntryType:     e.EntryType,
		EventType:     string(e.EventType),
		ActorID:       e.ActorID,
		ActorKind:     kind,
		Channel:       e.Channel,
		CorrelationID: e.CorrelationID,
		Details:       e.Details,
	}
}
