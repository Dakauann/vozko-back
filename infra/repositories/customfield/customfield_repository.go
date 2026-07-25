// Package customfield_repository is the GORM implementation of the domain
// customfield.Repository (custom field definitions). Schema rows live in
// infra/database/schema and are hand-mapped here; the select/multiselect Options
// list is marshaled through datatypes.JSON. The constructor returns the domain
// interface.
package customfield_repository

import (
	"encoding/json"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/customfield"
	"vozko/infra/database/schema"
)

// ErrNotFound is returned when a custom field definition does not exist in the
// given workspace. The domain customfield package defines no not-found sentinel,
// so it lives here; the HTTP handler maps it to 404.
var ErrNotFound = errors.New("customfield: not found")

type repository struct {
	db *gorm.DB
}

// NewRepository returns the domain customfield.Repository backed by GORM.
func NewRepository(db *gorm.DB) customfield.Repository {
	return &repository{db: db}
}

func (r *repository) Create(d *customfield.Definition) error {
	row, err := mapToSchema(d)
	if err != nil {
		return err
	}
	if err := r.db.Create(row).Error; err != nil {
		return err
	}
	d.ID = row.ID
	d.CreatedAt = row.CreatedAt
	d.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) Update(d *customfield.Definition) error {
	optionsJSON, err := marshalOptions(d.Options)
	if err != nil {
		return err
	}
	update := map[string]interface{}{
		"label":    d.Label,
		"type":     string(d.Type),
		"options":  optionsJSON,
		"required": d.Required,
		"position": d.Position,
	}
	res := r.db.Model(&schema.CustomFieldDefinition{}).
		Where("id = ? AND workspace_id = ?", d.ID, d.WorkspaceID).
		Updates(update)
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

func (r *repository) ListByObject(workspaceID, objectType string) ([]*customfield.Definition, error) {
	var rows []schema.CustomFieldDefinition
	if err := r.db.
		Where("workspace_id = ? AND object_type = ?", workspaceID, objectType).
		Order("position ASC, created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
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
	optionsJSON, err := marshalOptions(d.Options)
	if err != nil {
		return nil, err
	}
	return &schema.CustomFieldDefinition{
		ID:          d.ID,
		WorkspaceID: d.WorkspaceID,
		ObjectType:  d.ObjectType,
		Key:         d.Key,
		Label:       d.Label,
		Type:        string(d.Type),
		Options:     optionsJSON,
		Required:    d.Required,
		Position:    d.Position,
	}, nil
}

func mapToDomain(row *schema.CustomFieldDefinition) (*customfield.Definition, error) {
	options, err := unmarshalOptions(row.Options)
	if err != nil {
		return nil, err
	}
	return &customfield.Definition{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		ObjectType:  row.ObjectType,
		Key:         row.Key,
		Label:       row.Label,
		Type:        customfield.FieldType(row.Type),
		Options:     options,
		Required:    row.Required,
		Position:    row.Position,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func marshalOptions(opts []string) (datatypes.JSON, error) {
	if len(opts) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func unmarshalOptions(raw datatypes.JSON) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var opts []string
	if err := json.Unmarshal(raw, &opts); err != nil {
		return nil, err
	}
	return opts, nil
}
