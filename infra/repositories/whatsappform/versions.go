package whatsappform_repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const versionColumns = "v.id::text AS id, v.workspace_id::text AS workspace_id, v.form_id::text AS form_id, v.number," +
	" v.document::text AS document, v.flow_json::text AS flow_json, v.flow_version, v.compiler_version, v.hash," +
	" v.problems::text AS problems, v.created_by::text AS created_by, v.created_at"

const (
	getVersionSQL = "SELECT " + versionColumns + " FROM whatsapp_form_versions v WHERE v.workspace_id = ? AND v.id = ?"

	listVersionsSQL = "SELECT " + versionColumns + " FROM whatsapp_form_versions v WHERE v.workspace_id = ? AND v.form_id = ? ORDER BY v.number DESC"
)

type VersionStore struct {
	db *gorm.DB
}

var _ whatsappform.VersionRepository = (*VersionStore)(nil)

func NewVersionStore(db *gorm.DB) *VersionStore {
	return &VersionStore{db: db}
}

type versionRow struct {
	ID              string
	WorkspaceID     string
	FormID          string
	Number          int
	Document        string
	FlowJSON        schema.OptionalText
	FlowVersion     string
	CompilerVersion string
	Hash            string
	Problems        string
	CreatedBy       schema.OptionalText
	CreatedAt       time.Time
}

func (r versionRow) toDomain() (whatsappform.Version, error) {
	v := whatsappform.Version{
		ID: r.ID, WorkspaceID: r.WorkspaceID, FormID: r.FormID, Number: r.Number, FlowVersion: r.FlowVersion,
		CompilerVersion: r.CompilerVersion, Hash: r.Hash, CreatedBy: string(r.CreatedBy), CreatedAt: r.CreatedAt,
	}
	if r.FlowJSON != "" {
		v.FlowJSON = json.RawMessage(r.FlowJSON)
	}
	if err := decodeJSON(r.Document, &v.Document); err != nil {
		return whatsappform.Version{}, err
	}
	if err := decodeJSON(r.Problems, &v.Problems); err != nil {
		return whatsappform.Version{}, err
	}
	if len(v.Problems) == 0 {
		v.Problems = nil
	}
	return v, nil
}

func (s *VersionStore) Get(ctx context.Context, workspaceID, versionID string) (*whatsappform.Version, error) {
	if !database.AllUUIDs(workspaceID, versionID) {
		return nil, whatsappform.ErrVersionNotFound
	}
	versions, err := s.versions(ctx, getVersionSQL, workspaceID, versionID)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, whatsappform.ErrVersionNotFound
	}
	return &versions[0], nil
}

func (s *VersionStore) List(ctx context.Context, workspaceID, formID string) ([]whatsappform.Version, error) {
	if !database.AllUUIDs(workspaceID, formID) {
		return []whatsappform.Version{}, nil
	}
	return s.versions(ctx, listVersionsSQL, workspaceID, formID)
}

func (s *VersionStore) versions(ctx context.Context, sql string, args ...interface{}) ([]whatsappform.Version, error) {
	var rows []versionRow
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	versions := make([]whatsappform.Version, 0, len(rows))
	for _, row := range rows {
		v, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, nil
}
