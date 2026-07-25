package workspace_addon_repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	"vozko/infra/database/schema"
)

type addonDefinitionRepository struct {
	db *gorm.DB
}

func NewAddonDefinitionRepository(db *gorm.DB) workspace_addon.AddonDefinitionRepository {
	return &addonDefinitionRepository{db: db}
}

func (r *addonDefinitionRepository) Create(def *workspace_addon.AddonDefinition) error {
	if def == nil {
		return workspace_addon.ErrAddonNotFound
	}
	return r.db.Create(definitionToSchema(def)).Error
}

func (r *addonDefinitionRepository) Update(def *workspace_addon.AddonDefinition) error {
	if def == nil {
		return workspace_addon.ErrAddonNotFound
	}
	result := r.db.Model(&schema.WorkspaceAddonDefinition{}).
		Where("id = ?", def.ID).
		Select("*").
		Updates(definitionToSchema(def))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_addon.ErrAddonNotFound
	}
	return nil
}

func (r *addonDefinitionRepository) Archive(id string, archivedAt time.Time) error {
	result := r.db.Model(&schema.WorkspaceAddonDefinition{}).
		Where("id = ?", id).
		Update("archived_at", archivedAt)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_addon.ErrAddonNotFound
	}
	return nil
}

func (r *addonDefinitionRepository) GetByID(id string) (*workspace_addon.AddonDefinition, error) {
	var row schema.WorkspaceAddonDefinition
	if err := r.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_addon.ErrAddonNotFound
		}
		return nil, err
	}
	return definitionFromSchema(&row), nil
}

func (r *addonDefinitionRepository) GetByKey(key string) (*workspace_addon.AddonDefinition, error) {
	var row schema.WorkspaceAddonDefinition
	if err := r.db.Where("key = ? AND archived_at IS NULL", key).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_addon.ErrAddonNotFound
		}
		return nil, err
	}
	return definitionFromSchema(&row), nil
}

func (r *addonDefinitionRepository) List(includeArchived bool) ([]*workspace_addon.AddonDefinition, error) {
	query := r.db.Model(&schema.WorkspaceAddonDefinition{}).Order("created_at asc")
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var rows []schema.WorkspaceAddonDefinition
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*workspace_addon.AddonDefinition, 0, len(rows))
	for i := range rows {
		out = append(out, definitionFromSchema(&rows[i]))
	}
	return out, nil
}

func (r *addonDefinitionRepository) ListActiveVisible(workspaceID string) ([]*workspace_addon.AddonDefinition, error) {
	var rows []schema.WorkspaceAddonDefinition
	if err := r.db.Where("archived_at IS NULL AND is_active = ? AND is_globally_visible = ?", true, true).
		Order("created_at asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*workspace_addon.AddonDefinition, 0, len(rows))
	for i := range rows {
		out = append(out, definitionFromSchema(&rows[i]))
	}
	return out, nil
}

func definitionToSchema(def *workspace_addon.AddonDefinition) *schema.WorkspaceAddonDefinition {
	return &schema.WorkspaceAddonDefinition{
		ID:                 def.ID,
		Key:                def.Key,
		Name:               def.Name,
		Description:        def.Description,
		EntitlementKind:    string(def.EntitlementKind),
		UnitsPerQuantity:   def.UnitsPerQuantity,
		MonthlyPriceMicros: def.MonthlyPriceMicros,
		AnnualPriceMicros:  def.AnnualPriceMicros,
		MonthlyCostMicros:  def.MonthlyCostMicros,
		AnnualCostMicros:   def.AnnualCostMicros,
		IsActive:           def.IsActive,
		IsGloballyVisible:  def.IsGloballyVisible,
		ArchivedAt:         def.ArchivedAt,
		CreatedAt:          def.CreatedAt,
		UpdatedAt:          def.UpdatedAt,
	}
}

func definitionFromSchema(row *schema.WorkspaceAddonDefinition) *workspace_addon.AddonDefinition {
	return &workspace_addon.AddonDefinition{
		ID:                 row.ID,
		Key:                row.Key,
		Name:               row.Name,
		Description:        row.Description,
		EntitlementKind:    workspace_addon.EntitlementKind(row.EntitlementKind),
		UnitsPerQuantity:   row.UnitsPerQuantity,
		MonthlyPriceMicros: row.MonthlyPriceMicros,
		AnnualPriceMicros:  row.AnnualPriceMicros,
		MonthlyCostMicros:  row.MonthlyCostMicros,
		AnnualCostMicros:   row.AnnualCostMicros,
		IsActive:           row.IsActive,
		IsGloballyVisible:  row.IsGloballyVisible,
		ArchivedAt:         row.ArchivedAt,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}
