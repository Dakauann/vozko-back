package conversation_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type delegationRepository struct {
	db *gorm.DB
}

func NewDelegationRepository(db *gorm.DB) conversation.DelegationRepository {
	return &delegationRepository{db: db}
}

func (r *delegationRepository) Find(ctx context.Context, entryID string, entryType shared.EntryType) (*conversation.Delegation, error) {
	var record schema.ConversationDelegation
	err := r.db.WithContext(ctx).
		Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	automation, err := conversation.NewAutomation(record.AutomationKind, record.AutomationID)
	if err != nil {
		return nil, err
	}
	return &conversation.Delegation{
		WorkspaceID: record.WorkspaceID,
		EntryID:     record.EntryID,
		EntryType:   shared.EntryType(record.EntryType),
		Automation:  automation,
		DelegatedBy: record.DelegatedBy,
		DelegatedAt: record.UpdatedAt,
	}, nil
}

func (r *delegationRepository) FindMany(ctx context.Context, entries []shared.EntryRef) (map[shared.EntryRef]conversation.Automation, error) {
	out := make(map[shared.EntryRef]conversation.Automation)
	if len(entries) == 0 {
		return out, nil
	}
	pairs := make([][]any, len(entries))
	for i, e := range entries {
		pairs[i] = []any{e.EntryID, string(e.EntryType)}
	}
	var records []schema.ConversationDelegation
	if err := r.db.WithContext(ctx).
		Where("(entry_id, entry_type) IN ?", pairs).
		Find(&records).Error; err != nil {
		return nil, err
	}
	for _, record := range records {
		automation, err := conversation.NewAutomation(record.AutomationKind, record.AutomationID)
		if err != nil {
			continue
		}
		out[shared.EntryRef{EntryID: record.EntryID, EntryType: shared.EntryType(record.EntryType)}] = automation
	}
	return out, nil
}

func (r *delegationRepository) Save(ctx context.Context, d conversation.Delegation) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "entry_id"}, {Name: "entry_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"workspace_id", "automation_kind", "automation_id", "delegated_by", "updated_at"}),
	}).Create(&schema.ConversationDelegation{
		EntryID:        d.EntryID,
		EntryType:      string(d.EntryType),
		WorkspaceID:    d.WorkspaceID,
		AutomationKind: string(d.Automation.Kind),
		AutomationID:   d.Automation.ID,
		DelegatedBy:    d.DelegatedBy,
	}).Error
}

func (r *delegationRepository) Delete(ctx context.Context, entryID string, entryType shared.EntryType) error {
	return r.db.WithContext(ctx).
		Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Delete(&schema.ConversationDelegation{}).Error
}
