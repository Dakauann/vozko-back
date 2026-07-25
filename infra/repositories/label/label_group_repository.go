package label_repository

import (
	"vozko/domain/label"
	"vozko/infra/database/schema"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type labelGroupRepository struct {
	db *gorm.DB
}

func NewLabelGroupRepository(db *gorm.DB) label.LabelGroupRepository {
	return &labelGroupRepository{db: db}
}

func (r *labelGroupRepository) Create(group *label.LabelGroup) error {
	if group.ID == "" {
		group.ID = uuid.New().String()
	}

	schemaGroup := schema.LabelGroup{
		ID:          group.ID,
		WorkspaceID: group.WorkspaceID,
		Name:        group.Name,
	}
	if err := r.db.Create(&schemaGroup).Error; err != nil {
		return err
	}

	for i, item := range group.Items {
		if item.ID == "" {
			item.ID = uuid.New().String()
			group.Items[i].ID = item.ID
		}
		schemaItem := schema.LabelGroupItem{
			ID:           item.ID,
			LabelGroupID: group.ID,
			Name:         item.Name,
			Color:        item.Color,
			Position:     item.Position,
		}
		if err := r.db.Create(&schemaItem).Error; err != nil {
			return err
		}
	}

	return nil
}

func (r *labelGroupRepository) Update(group *label.LabelGroup) error {
	return r.db.Model(&schema.LabelGroup{}).
		Where("id = ?", group.ID).
		Updates(map[string]interface{}{
			"name": group.Name,
		}).Error
}

func (r *labelGroupRepository) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&schema.LabelGroup{}).Error
}

func (r *labelGroupRepository) FindByID(id string) (*label.LabelGroup, error) {
	var sg schema.LabelGroup
	if err := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&sg, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return mapLabelGroupToDomain(&sg), nil
}

func (r *labelGroupRepository) ListByWorkspace(workspaceID string) ([]*label.LabelGroup, error) {
	var groups []schema.LabelGroup
	if err := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).Where("workspace_id = ?", workspaceID).Find(&groups).Error; err != nil {
		return nil, err
	}
	result := make([]*label.LabelGroup, len(groups))
	for i := range groups {
		result[i] = mapLabelGroupToDomain(&groups[i])
	}
	return result, nil
}

func (r *labelGroupRepository) AddItem(item *label.LabelGroupItem) error {
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	schemaItem := schema.LabelGroupItem{
		ID:           item.ID,
		LabelGroupID: item.LabelGroupID,
		Name:         item.Name,
		Color:        item.Color,
		Position:     item.Position,
	}
	return r.db.Create(&schemaItem).Error
}

func (r *labelGroupRepository) RemoveItem(itemID string) error {
	return r.db.Where("id = ?", itemID).Delete(&schema.LabelGroupItem{}).Error
}

func (r *labelGroupRepository) GetItems(labelGroupID string) ([]label.LabelGroupItem, error) {
	var items []schema.LabelGroupItem
	if err := r.db.Where("label_group_id = ?", labelGroupID).
		Order("position ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	result := make([]label.LabelGroupItem, len(items))
	for i, item := range items {
		result[i] = label.LabelGroupItem{
			ID:           item.ID,
			LabelGroupID: item.LabelGroupID,
			Name:         item.Name,
			Color:        item.Color,
			Position:     item.Position,
		}
	}
	return result, nil
}

func mapLabelGroupToDomain(sg *schema.LabelGroup) *label.LabelGroup {
	items := make([]label.LabelGroupItem, len(sg.Items))
	for i, item := range sg.Items {
		items[i] = label.LabelGroupItem{
			ID:           item.ID,
			LabelGroupID: item.LabelGroupID,
			Name:         item.Name,
			Color:        item.Color,
			Position:     item.Position,
		}
	}
	return &label.LabelGroup{
		ID:          sg.ID,
		WorkspaceID: sg.WorkspaceID,
		Name:        sg.Name,
		Items:       items,
	}
}
