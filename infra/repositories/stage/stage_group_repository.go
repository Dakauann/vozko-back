package stage_repository

import (
	"vozko/domain/stage"
	"vozko/infra/database/schema"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StageGroupRepository struct {
	db *gorm.DB
}

func NewStageGroupRepository(db *gorm.DB) stage.StageGroupRepository {
	return &StageGroupRepository{db: db}
}

func (r *StageGroupRepository) Create(group *stage.StageGroup) error {
	if group.ID == "" {
		group.ID = uuid.New().String()
	}

	schemaGroup := schema.StageGroup{
		ID:          group.ID,
		WorkspaceID: group.WorkspaceID,
		Name:        group.Name,
	}
	if group.DepartmentID != "" {
		schemaGroup.DepartmentID = &group.DepartmentID
	}

	if err := r.db.Create(&schemaGroup).Error; err != nil {
		return err
	}

	for i, item := range group.Items {
		if item.ID == "" {
			item.ID = uuid.New().String()
			group.Items[i].ID = item.ID
		}
		schemaItem := schema.StageGroupItem{
			ID:          item.ID,
			StageGroupID:  group.ID,
			Name:        item.Name,
			Description: item.Description,
			Color:       item.Color,
			Position:    item.Position,
		}
		if err := r.db.Create(&schemaItem).Error; err != nil {
			return err
		}
	}

	return nil
}

func (r *StageGroupRepository) Update(group *stage.StageGroup) error {
	return r.db.Model(&schema.StageGroup{}).
		Where("id = ?", group.ID).
		Updates(map[string]interface{}{
			"name": group.Name,
		}).Error
}

func (r *StageGroupRepository) Delete(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tag_group_id = ?", id).Delete(&schema.StageGroupItem{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&schema.StageGroup{}).Error
	})
}

func (r *StageGroupRepository) FindByID(id string) (*stage.StageGroup, error) {
	var s schema.StageGroup
	if err := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).Where("id = ?", id).First(&s).Error; err != nil {
		return nil, err
	}
	return mapTagGroupToDomain(&s), nil
}

func (r *StageGroupRepository) ListByWorkspace(workspaceID string) ([]*stage.StageGroup, error) {
	var groups []schema.StageGroup
	if err := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).Where("workspace_id = ?", workspaceID).
		Order("created_at ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}

	result := make([]*stage.StageGroup, len(groups))
	for i := range groups {
		result[i] = mapTagGroupToDomain(&groups[i])
	}
	return result, nil
}

func (r *StageGroupRepository) ListByWorkspaceAndDepartments(workspaceID string, departmentIDs []string) ([]*stage.StageGroup, error) {
	var groups []schema.StageGroup
	q := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).Where("workspace_id = ?", workspaceID)

	if len(departmentIDs) > 0 {
		q = q.Where("department_id IN ?", departmentIDs)
	}

	if err := q.Order("created_at ASC").Find(&groups).Error; err != nil {
		return nil, err
	}

	result := make([]*stage.StageGroup, len(groups))
	for i := range groups {
		result[i] = mapTagGroupToDomain(&groups[i])
	}
	return result, nil
}

func (r *StageGroupRepository) AddItem(item *stage.StageGroupItem) error {
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	schemaItem := schema.StageGroupItem{
		ID:          item.ID,
		StageGroupID:  item.StageGroupID,
		Name:        item.Name,
		Description: item.Description,
		Color:       item.Color,
		Position:    item.Position,
	}
	return r.db.Create(&schemaItem).Error
}

func (r *StageGroupRepository) RemoveItem(itemID string) error {
	return r.db.Where("id = ?", itemID).Delete(&schema.StageGroupItem{}).Error
}

func (r *StageGroupRepository) GetItems(StageGroupID string) ([]stage.StageGroupItem, error) {
	var items []schema.StageGroupItem
	if err := r.db.Where("tag_group_id = ?", StageGroupID).
		Order("position ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	result := make([]stage.StageGroupItem, len(items))
	for i, item := range items {
		result[i] = stage.StageGroupItem{
			ID:          item.ID,
			StageGroupID:  item.StageGroupID,
			Name:        item.Name,
			Description: item.Description,
			Color:       item.Color,
			Position:    item.Position,
		}
	}
	return result, nil
}

func mapTagGroupToDomain(s *schema.StageGroup) *stage.StageGroup {
	group := &stage.StageGroup{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		Name:        s.Name,
		Items:       make([]stage.StageGroupItem, len(s.Items)),
	}
	if s.DepartmentID != nil {
		group.DepartmentID = *s.DepartmentID
	}
	for i, item := range s.Items {
		group.Items[i] = stage.StageGroupItem{
			ID:          item.ID,
			StageGroupID:  item.StageGroupID,
			Name:        item.Name,
			Description: item.Description,
			Color:       item.Color,
			Position:    item.Position,
		}
	}
	return group
}
