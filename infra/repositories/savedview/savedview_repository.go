package savedview_repository

import (
	"encoding/json"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/savedview"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

// NewRepository returns the domain savedview.Repository backed by GORM.
func NewRepository(db *gorm.DB) savedview.Repository {
	return &repository{db: db}
}

func (r *repository) Create(v *savedview.SavedView) error {
	row, err := mapToSchema(v)
	if err != nil {
		return err
	}
	if err := r.db.Create(row).Error; err != nil {
		return err
	}
	v.ID = row.ID
	v.CreatedAt = row.CreatedAt
	v.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) Update(v *savedview.SavedView) error {
	filterJSON, err := marshalFilter(v.Filter)
	if err != nil {
		return err
	}
	columnsJSON, err := marshalColumns(v.Columns)
	if err != nil {
		return err
	}
	update := map[string]interface{}{
		"object_type":  string(v.ObjectType),
		"pipeline_id":  nullableUUID(v.PipelineID),
		"name":         v.Name,
		"filter":       filterJSON,
		"group_by":     string(v.GroupBy),
		"group_by_key": v.GroupByKey,
		"sort_field":   v.SortField,
		"sort_dir":     string(v.SortDir),
		"columns":      columnsJSON,
		"visibility":   string(v.Visibility),
		"is_default":   v.IsDefault,
		"position":     v.Position,
	}
	res := r.db.Model(&schema.SavedView{}).
		Where("id = ? AND workspace_id = ?", v.ID, v.WorkspaceID).
		Updates(update)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return savedview.ErrNotFound
	}
	return nil
}

func (r *repository) Delete(workspaceID, id string) error {
	res := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.SavedView{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return savedview.ErrNotFound
	}
	return nil
}

func (r *repository) GetByID(workspaceID, id string) (*savedview.SavedView, error) {
	var row schema.SavedView
	if err := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, savedview.ErrNotFound
		}
		return nil, err
	}
	return mapToDomain(&row)
}

// ListForUser returns the user's private views plus every shared view in the
// workspace for the given object type, ordered by Position.
func (r *repository) ListForUser(workspaceID, userID string, objectType savedview.ObjectType) ([]*savedview.SavedView, error) {
	var rows []schema.SavedView
	if err := r.db.
		Where("workspace_id = ? AND object_type = ?", workspaceID, string(objectType)).
		Where("visibility = ? OR owner_id = ?", string(savedview.VisibilityShared), userID).
		Order("position ASC, created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*savedview.SavedView, 0, len(rows))
	for i := range rows {
		v, err := mapToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ClearDefault unsets IsDefault on the user's views for an object type so a
// newly-set default stays exclusive.
func (r *repository) ClearDefault(workspaceID, userID string, objectType savedview.ObjectType) error {
	return r.db.Model(&schema.SavedView{}).
		Where("workspace_id = ? AND owner_id = ? AND object_type = ? AND is_default = ?",
			workspaceID, userID, string(objectType), true).
		Update("is_default", false).Error
}

func mapToSchema(v *savedview.SavedView) (*schema.SavedView, error) {
	filterJSON, err := marshalFilter(v.Filter)
	if err != nil {
		return nil, err
	}
	columnsJSON, err := marshalColumns(v.Columns)
	if err != nil {
		return nil, err
	}
	return &schema.SavedView{
		ID:          v.ID,
		WorkspaceID: v.WorkspaceID,
		OwnerID:     v.OwnerID,
		ObjectType:  string(v.ObjectType),
		PipelineID:  v.PipelineID,
		Name:        v.Name,
		Filter:      filterJSON,
		GroupBy:     string(v.GroupBy),
		GroupByKey:  v.GroupByKey,
		SortField:   v.SortField,
		SortDir:     string(v.SortDir),
		Columns:     columnsJSON,
		Visibility:  string(v.Visibility),
		IsDefault:   v.IsDefault,
		Position:    v.Position,
	}, nil
}

func mapToDomain(row *schema.SavedView) (*savedview.SavedView, error) {
	filter, err := unmarshalFilter(row.Filter)
	if err != nil {
		return nil, err
	}
	columns, err := unmarshalColumns(row.Columns)
	if err != nil {
		return nil, err
	}
	return &savedview.SavedView{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		OwnerID:     row.OwnerID,
		ObjectType:  savedview.ObjectType(row.ObjectType),
		PipelineID:  row.PipelineID,
		Name:        row.Name,
		Filter:      filter,
		GroupBy:     savedview.GroupBy(row.GroupBy),
		GroupByKey:  row.GroupByKey,
		SortField:   row.SortField,
		SortDir:     savedview.SortDir(row.SortDir),
		Columns:     columns,
		Visibility:  savedview.Visibility(row.Visibility),
		IsDefault:   row.IsDefault,
		Position:    row.Position,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

// nullableUUID returns nil for an empty id so an Update writes SQL NULL instead
// of an empty string into a nullable uuid column (which Postgres would reject).
func nullableUUID(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func marshalFilter(f crmfilter.Filter) (datatypes.JSON, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func unmarshalFilter(raw datatypes.JSON) (crmfilter.Filter, error) {
	var f crmfilter.Filter
	if len(raw) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, err
	}
	return f, nil
}

func marshalColumns(cols []string) (datatypes.JSON, error) {
	if len(cols) == 0 {
		return datatypes.JSON([]byte("[]")), nil
	}
	b, err := json.Marshal(cols)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func unmarshalColumns(raw datatypes.JSON) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var cols []string
	if err := json.Unmarshal(raw, &cols); err != nil {
		return nil, err
	}
	return cols, nil
}
