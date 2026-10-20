package whatsappform_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

const publicationColumns = "p.id::text AS id, p.workspace_id::text AS workspace_id, p.form_id::text AS form_id, p.waba_id::text AS waba_id," +
	" p.meta_waba_id, p.meta_flow_id, p.meta_name, p.version_id::text AS version_id, p.status, p.problems::text AS problems," +
	" p.last_error, p.preview_url, p.preview_expires_at, p.published_at, p.created_at, p.updated_at"

const (
	insertPublicationSQL = "INSERT INTO whatsapp_form_publications (id, workspace_id, form_id, waba_id, meta_waba_id, meta_flow_id, meta_name, version_id, status," +
		" problems, last_error, preview_url, preview_expires_at, published_at, created_at, updated_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?)"

	savePublicationSQL = "UPDATE whatsapp_form_publications SET waba_id = ?, meta_flow_id = ?, meta_name = ?, version_id = ?, status = ?, problems = ?::jsonb," +
		" last_error = ?, preview_url = ?, preview_expires_at = ?, published_at = ?, updated_at = ? WHERE workspace_id = ? AND id = ?"

	publicationsFromSQL = "SELECT " + publicationColumns + " FROM whatsapp_form_publications p WHERE "

	listPublicationsSQL = publicationsFromSQL + "p.workspace_id = ? AND p.form_id = ? ORDER BY p.created_at, p.id"

	findPublicationSQL = publicationsFromSQL + "p.workspace_id = ? AND p.form_id = ? AND p.meta_waba_id = ?"

	findPublicationByFlowSQL = publicationsFromSQL + "p.meta_waba_id = ? AND p.meta_flow_id = ?"
)

var listStuckPublicationsSQL = publicationsFromSQL + "p.status = " + sqlexpr.Literal(string(whatsappform.PublicationCreating)) +
	" AND p.updated_at < ? ORDER BY p.updated_at, p.id LIMIT ?"

type PublicationStore struct {
	db *gorm.DB
}

var _ whatsappform.PublicationRepository = (*PublicationStore)(nil)

func NewPublicationStore(db *gorm.DB) *PublicationStore {
	return &PublicationStore{db: db}
}

type publicationRow struct {
	ID               string
	WorkspaceID      string
	FormID           string
	WABAID           schema.OptionalText `gorm:"column:waba_id"`
	MetaWABAID       string              `gorm:"column:meta_waba_id"`
	MetaFlowID       schema.OptionalText
	MetaName         string
	VersionID        string
	Status           string
	Problems         string
	LastError        string
	PreviewURL       string
	PreviewExpiresAt *time.Time
	PublishedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (r publicationRow) toDomain() (whatsappform.Publication, error) {
	p := whatsappform.Publication{
		ID: r.ID, WorkspaceID: r.WorkspaceID, FormID: r.FormID, WABAID: string(r.WABAID), MetaWABAID: r.MetaWABAID,
		MetaFlowID: string(r.MetaFlowID), MetaName: r.MetaName, VersionID: r.VersionID, Status: whatsappform.PublicationStatus(r.Status),
		LastError: r.LastError, PreviewURL: r.PreviewURL, PreviewExpiresAt: r.PreviewExpiresAt, PublishedAt: r.PublishedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if err := decodeJSON(r.Problems, &p.Problems); err != nil {
		return whatsappform.Publication{}, err
	}
	if len(p.Problems) == 0 {
		p.Problems = nil
	}
	return p, nil
}

func (s *PublicationStore) Insert(ctx context.Context, p whatsappform.Publication) error {
	if !database.AllUUIDs(p.ID, p.WorkspaceID, p.FormID, p.VersionID) {
		return errIDsInvalid
	}
	problems, err := jsonText(p.Problems, "[]")
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Exec(insertPublicationSQL, p.ID, p.WorkspaceID, p.FormID, schema.OptionalText(p.WABAID), p.MetaWABAID,
		schema.OptionalText(p.MetaFlowID), p.MetaName, p.VersionID, string(p.Status), problems, p.LastError, p.PreviewURL,
		p.PreviewExpiresAt, p.PublishedAt, p.CreatedAt, p.UpdatedAt).Error
}

func (s *PublicationStore) Save(ctx context.Context, p whatsappform.Publication) error {
	if !database.AllUUIDs(p.ID, p.WorkspaceID, p.VersionID) {
		return whatsappform.ErrPublicationNotFound
	}
	problems, err := jsonText(p.Problems, "[]")
	if err != nil {
		return err
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(savePublicationSQL, schema.OptionalText(p.WABAID), schema.OptionalText(p.MetaFlowID), p.MetaName,
		p.VersionID, string(p.Status), problems, p.LastError, p.PreviewURL, p.PreviewExpiresAt, p.PublishedAt, p.UpdatedAt,
		p.WorkspaceID, p.ID), whatsappform.ErrPublicationNotFound)
}

func (s *PublicationStore) ListForForm(ctx context.Context, workspaceID, formID string) ([]whatsappform.Publication, error) {
	if !database.AllUUIDs(workspaceID, formID) {
		return []whatsappform.Publication{}, nil
	}
	return s.publications(ctx, listPublicationsSQL, workspaceID, formID)
}

func (s *PublicationStore) Find(ctx context.Context, workspaceID, formID, metaWABAID string) (*whatsappform.Publication, error) {
	if !database.AllUUIDs(workspaceID, formID) || strings.TrimSpace(metaWABAID) == "" {
		return nil, whatsappform.ErrPublicationNotFound
	}
	return s.one(ctx, findPublicationSQL, workspaceID, formID, metaWABAID)
}

func (s *PublicationStore) FindByMetaFlow(ctx context.Context, metaWABAID, metaFlowID string) (*whatsappform.Publication, error) {
	if strings.TrimSpace(metaWABAID) == "" || strings.TrimSpace(metaFlowID) == "" {
		return nil, whatsappform.ErrPublicationNotFound
	}
	return s.one(ctx, findPublicationByFlowSQL, metaWABAID, metaFlowID)
}

func (s *PublicationStore) ListStuck(ctx context.Context, before time.Time, limit int) ([]whatsappform.Publication, error) {
	if limit <= 0 {
		return []whatsappform.Publication{}, nil
	}
	return s.publications(ctx, listStuckPublicationsSQL, before, limit)
}

func (s *PublicationStore) one(ctx context.Context, sql string, args ...interface{}) (*whatsappform.Publication, error) {
	found, err := s.publications(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, whatsappform.ErrPublicationNotFound
	}
	return &found[0], nil
}

func (s *PublicationStore) publications(ctx context.Context, sql string, args ...interface{}) ([]whatsappform.Publication, error) {
	var rows []publicationRow
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	publications := make([]whatsappform.Publication, 0, len(rows))
	for _, row := range rows {
		p, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		publications = append(publications, p)
	}
	return publications, nil
}
