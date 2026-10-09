package customfield_repository

import (
	"encoding/json"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/customfield"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

var ErrNotFound = customfield.ErrNotFound

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) customfield.Store {
	return &repository{db: db}
}

func (r *repository) Create(d *customfield.Definition) error {
	row, err := mapToSchema(d)
	if err != nil {
		return err
	}
	if err := r.db.Create(row).Error; err != nil {
		if database.IsUniqueViolationOf(err, schema.CustomFieldLiveRoleIndex) {
			return customfield.ErrRoleTaken
		}
		if database.IsUniqueViolation(err) {
			return customfield.ErrKeyExists
		}
		return err
	}
	d.ID = row.ID
	d.CreatedAt = row.CreatedAt
	d.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) Update(d *customfield.Definition) error {
	row, err := mapToSchema(d)
	if err != nil {
		return err
	}
	update := map[string]interface{}{
		"label":        row.Label,
		"type":         row.Type,
		"options":      row.Options,
		"option_tones": row.OptionTones,
		"required":     row.Required,
		"sensitive":    row.Sensitive,
		"legal_basis":  row.LegalBasis,
		"role":         row.Role,
		"position":     row.Position,
	}
	res := r.db.Model(&schema.CustomFieldDefinition{}).
		Where("id = ? AND workspace_id = ?", d.ID, d.WorkspaceID).
		Updates(update)
	if database.IsUniqueViolationOf(res.Error, schema.CustomFieldLiveRoleIndex) {
		return customfield.ErrRoleTaken
	}
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) Delete(workspaceID, id string) error {
	res := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.CustomFieldDefinition{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) GetByID(workspaceID, id string) (*customfield.Definition, error) {
	var row schema.CustomFieldDefinition
	if err := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return mapToDomain(&row)
}

func (r *repository) RetiredByKey(workspaceID string, objectType customfield.ObjectType, key string) ([]*customfield.Definition, error) {
	var rows []schema.CustomFieldDefinition
	if err := r.db.Unscoped().
		Where("workspace_id = ? AND object_type = ? AND key = ? AND deleted_at IS NOT NULL", workspaceID, string(objectType), key).
		Order("deleted_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapAllToDomain(rows)
}

func (r *repository) ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	var rows []schema.CustomFieldDefinition
	if err := r.db.
		Where("workspace_id = ? AND object_type = ?", workspaceID, string(objectType)).
		Order("position ASC, created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapAllToDomain(rows)
}

func mapAllToDomain(rows []schema.CustomFieldDefinition) ([]*customfield.Definition, error) {
	out := make([]*customfield.Definition, 0, len(rows))
	for i := range rows {
		d, err := mapToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func mapToSchema(d *customfield.Definition) (*schema.CustomFieldDefinition, error) {
	optionsJSON, err := marshalJSON(d.Options, len(d.Options) == 0)
	if err != nil {
		return nil, err
	}
	tonesJSON, err := marshalJSON(d.OptionTones, len(d.OptionTones) == 0)
	if err != nil {
		return nil, err
	}
	return &schema.CustomFieldDefinition{
		ID:          d.ID,
		WorkspaceID: d.WorkspaceID,
		ObjectType:  string(d.ObjectType),
		Key:         d.Key,
		Label:       d.Label,
		Type:        string(d.Type),
		Options:     optionsJSON,
		OptionTones: tonesJSON,
		Required:    d.Required,
		Sensitive:   d.Sensitive,
		LegalBasis:  d.LegalBasis,
		Role:        string(d.Role),
		Position:    d.Position,
	}, nil
}

func mapToDomain(row *schema.CustomFieldDefinition) (*customfield.Definition, error) {
	var options []string
	if err := unmarshalJSON(row.Options, &options); err != nil {
		return nil, err
	}
	var tones map[string]customfield.Tone
	if err := unmarshalJSON(row.OptionTones, &tones); err != nil {
		return nil, err
	}
	return &customfield.Definition{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		ObjectType:  customfield.ObjectType(row.ObjectType),
		Key:         row.Key,
		Label:       row.Label,
		Type:        customfield.FieldType(row.Type),
		Options:     options,
		OptionTones: tones,
		Required:    row.Required,
		Sensitive:   row.Sensitive,
		LegalBasis:  row.LegalBasis,
		Role:        customfield.Role(row.Role),
		Position:    row.Position,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func marshalJSON(value any, empty bool) (datatypes.JSON, error) {
	if empty {
		return nil, nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func unmarshalJSON(raw datatypes.JSON, target any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}
