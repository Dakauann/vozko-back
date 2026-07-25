package label_repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/label"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) label.Repository {
	return &repository{db: db}
}

func (r *repository) Create(l *label.Label) error {
	dbLabel := schema.Label{
		ID:          l.ID,
		WorkspaceID: l.WorkspaceID,
		Name:        l.Name,
		Color:       l.Color,
		Position:    l.Position,
	}
	if err := r.db.Create(&dbLabel).Error; err != nil {
		return err
	}
	l.CreatedAt = dbLabel.CreatedAt
	l.UpdatedAt = dbLabel.UpdatedAt
	return nil
}

func (r *repository) Update(l *label.Label) error {
	update := map[string]interface{}{
		"name":     l.Name,
		"color":    l.Color,
		"position": l.Position,
	}
	return r.db.Model(&schema.Label{}).Where("id = ?", l.ID).Updates(update).Error
}

func (r *repository) Delete(id string) error {
	if err := r.db.Where("label_id = ?", id).Delete(&schema.EntryLabel{}).Error; err != nil {
		return err
	}
	return r.db.Delete(&schema.Label{}, "id = ?", id).Error
}

func (r *repository) FindByID(id string) (*label.Label, error) {
	var dbLabel schema.Label
	if err := r.db.Where("id = ?", id).First(&dbLabel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, label.ErrLabelNotFound
		}
		return nil, err
	}
	return mapLabelToDomain(&dbLabel), nil
}

func (r *repository) ListByWorkspace(userID string) ([]*label.Label, error) {
	var dbLabels []schema.Label
	if err := r.db.Where("workspace_id = ?", userID).Order("position ASC, created_at ASC").Find(&dbLabels).Error; err != nil {
		return nil, err
	}
	result := make([]*label.Label, len(dbLabels))
	for i := range dbLabels {
		result[i] = mapLabelToDomain(&dbLabels[i])
	}
	return result, nil
}

func (r *repository) NameExistsInWorkspace(workspaceID, name string, excludeID *string) (bool, error) {
	query := r.db.Model(&schema.Label{}).Where("workspace_id = ? AND LOWER(name) = ?", workspaceID, strings.ToLower(name))
	if excludeID != nil && *excludeID != "" {
		query = query.Where("id <> ?", *excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) AssignLabel(el *label.EntryLabel) error {
	var count int64
	if err := r.db.Model(&schema.EntryLabel{}).
		Where("label_id = ? AND entry_id = ? AND entry_type = ? AND workspace_id = ?", el.LabelID, el.EntryID, el.EntryType, el.WorkspaceID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return label.ErrEntryLabelExists
	}

	dbEL := schema.EntryLabel{
		ID:          el.ID,
		LabelID:     el.LabelID,
		EntryID:     el.EntryID,
		EntryType:   el.EntryType,
		WorkspaceID: el.WorkspaceID,
	}
	if err := r.db.Create(&dbEL).Error; err != nil {
		return err
	}
	el.CreatedAt = dbEL.CreatedAt
	return nil
}

func (r *repository) RemoveLabel(labelID, entryID, entryType, userID string) error {
	result := r.db.Where("label_id = ? AND entry_id = ? AND entry_type = ? AND workspace_id = ?", labelID, entryID, entryType, userID).
		Delete(&schema.EntryLabel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return label.ErrEntryLabelNotFound
	}
	return nil
}

func (r *repository) RemoveEntryLabels(entryID, entryType, userID string) error {
	result := r.db.Where("entry_id = ? AND entry_type = ? AND workspace_id = ?", entryID, entryType, userID).
		Delete(&schema.EntryLabel{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *repository) GetEntryLabels(entryID, entryType, userID string) ([]*label.EntryLabel, error) {
	var dbEntryLabels []schema.EntryLabel
	if err := r.db.Preload("Label").
		Where("entry_id = ? AND entry_type = ? AND workspace_id = ?", entryID, entryType, userID).
		Find(&dbEntryLabels).Error; err != nil {
		return nil, err
	}

	result := make([]*label.EntryLabel, len(dbEntryLabels))
	for i := range dbEntryLabels {
		result[i] = mapEntryLabelToDomain(&dbEntryLabels[i])
	}
	return result, nil
}

func (r *repository) GetBatchEntryLabels(entryIDs []string, entryType, userID string) (map[string][]*label.EntryLabel, error) {
	if len(entryIDs) == 0 {
		return make(map[string][]*label.EntryLabel), nil
	}

	var dbEntryLabels []schema.EntryLabel
	if err := r.db.Preload("Label").
		Where("entry_id IN ? AND entry_type = ? AND workspace_id = ?", entryIDs, entryType, userID).
		Find(&dbEntryLabels).Error; err != nil {
		return nil, err
	}

	result := make(map[string][]*label.EntryLabel, len(entryIDs))
	for i := range dbEntryLabels {
		el := mapEntryLabelToDomain(&dbEntryLabels[i])
		result[el.EntryID] = append(result[el.EntryID], el)
	}
	return result, nil
}

func (r *repository) GetEntriesByLabel(labelID, userID string) ([]*label.EntryLabel, error) {
	var dbEntryLabels []schema.EntryLabel
	if err := r.db.Preload("Label").
		Where("label_id = ? AND workspace_id = ?", labelID, userID).
		Find(&dbEntryLabels).Error; err != nil {
		return nil, err
	}

	result := make([]*label.EntryLabel, len(dbEntryLabels))
	for i := range dbEntryLabels {
		result[i] = mapEntryLabelToDomain(&dbEntryLabels[i])
	}
	return result, nil
}

func (r *repository) ReorderLabels(userID string, labelIDs []string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for i, labelID := range labelIDs {
			position := i + 1
			result := tx.Model(&schema.Label{}).
				Where("id = ? AND workspace_id = ?", labelID, userID).
				Update("position", position)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return label.ErrLabelNotFound
			}
		}
		return nil
	})
}

func mapLabelToDomain(dbLabel *schema.Label) *label.Label {
	return &label.Label{
		ID:          dbLabel.ID,
		WorkspaceID: dbLabel.WorkspaceID,
		Name:        dbLabel.Name,
		Color:       dbLabel.Color,
		Position:    dbLabel.Position,
		CreatedAt:   dbLabel.CreatedAt,
		UpdatedAt:   dbLabel.UpdatedAt,
	}
}

func mapEntryLabelToDomain(dbEL *schema.EntryLabel) *label.EntryLabel {
	el := &label.EntryLabel{
		ID:          dbEL.ID,
		LabelID:     dbEL.LabelID,
		EntryID:     dbEL.EntryID,
		EntryType:   dbEL.EntryType,
		WorkspaceID: dbEL.WorkspaceID,
		CreatedAt:   dbEL.CreatedAt,
	}
	if dbEL.Label.ID != "" {
		el.LabelName = dbEL.Label.Name
		el.LabelColor = dbEL.Label.Color
	}
	return el
}
