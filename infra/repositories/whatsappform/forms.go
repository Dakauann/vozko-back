package whatsappform_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

var errVersionNotTheDraft = errors.New("whatsappform repository: the version is not the draft of this form")

var sentStatuses = sqlexpr.LiteralList([]string{string(whatsappform.SendSent), string(whatsappform.SendResponded)})

const formColumns = "f.id::text AS id, f.workspace_id::text AS workspace_id, f.name, f.description, f.kind," +
	" f.draft_version_id::text AS draft_version_id, f.published_version_id::text AS published_version_id, f.created_by::text AS created_by," +
	" f.created_at, f.updated_at, f.archived_at"

const (
	insertFormSQL = "INSERT INTO whatsapp_forms (id, workspace_id, name, description, kind, draft_version_id, published_version_id, created_by, created_at, updated_at, archived_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	insertVersionSQL = "INSERT INTO whatsapp_form_versions (id, workspace_id, form_id, number, document, flow_json, flow_version, compiler_version, hash, problems, created_by, created_at)" +
		" VALUES (?, ?, ?, ?, ?::jsonb, ?::json, ?, ?, ?, ?::jsonb, ?, ?)"

	getFormSQL = "SELECT " + formColumns + " FROM whatsapp_forms f WHERE f.workspace_id = ? AND f.id = ?"

	moveDraftSQL = "UPDATE whatsapp_forms SET draft_version_id = ?, updated_at = ? WHERE workspace_id = ? AND id = ? AND draft_version_id = ?"

	renameFormSQL = "UPDATE whatsapp_forms SET name = ?, description = ?, updated_at = ? WHERE workspace_id = ? AND id = ?"

	publishFormSQL = "UPDATE whatsapp_forms SET published_version_id = ?, updated_at = ? WHERE workspace_id = ? AND id = ?" +
		" AND EXISTS (SELECT 1 FROM whatsapp_form_versions v WHERE v.id = ? AND v.form_id = whatsapp_forms.id AND v.workspace_id = whatsapp_forms.workspace_id)"

	unpublishFormSQL = "UPDATE whatsapp_forms SET published_version_id = NULL, updated_at = ? WHERE workspace_id = ? AND id = ?"

	archiveFormSQL = "UPDATE whatsapp_forms SET archived_at = COALESCE(archived_at, ?), updated_at = ? WHERE workspace_id = ? AND id = ?"

	listActiveFormsSQL = " AND f.archived_at IS NULL"

	listFormsOrderSQL = " ORDER BY f.updated_at DESC, f.id DESC"
)

var listFormsSQL = "SELECT " + formColumns +
	", COALESCE(pb.publications, '[]') AS publications, sc.sent, rc.responses, rc.last_response_at" +
	" FROM whatsapp_forms f" +
	" LEFT JOIN LATERAL (SELECT json_agg(json_build_object('metaWabaId', p.meta_waba_id, 'status', p.status, 'versionId', p.version_id::text) ORDER BY p.created_at, p.id) AS publications" +
	" FROM whatsapp_form_publications p WHERE p.workspace_id = f.workspace_id AND p.form_id = f.id) pb ON true" +
	" LEFT JOIN LATERAL (SELECT COUNT(*) AS sent FROM whatsapp_form_sends s WHERE s.workspace_id = f.workspace_id AND s.form_id = f.id AND s.status IN (" + sentStatuses + ")) sc ON true" +
	" LEFT JOIN LATERAL (SELECT COUNT(*) AS responses, MAX(r.submitted_at) AS last_response_at" +
	" FROM whatsapp_form_responses r WHERE r.workspace_id = f.workspace_id AND r.form_id = f.id) rc ON true" +
	" WHERE f.workspace_id = ?"

const publishedWorkspacesSQL = "SELECT DISTINCT workspace_id::text AS workspace_id FROM whatsapp_forms WHERE published_version_id IS NOT NULL AND archived_at IS NULL"

var listFormsSearchSQL = " AND " + database.SearchFold("f.name") + " LIKE " + database.SearchFold("?")

type FormStore struct {
	db *gorm.DB
}

var _ whatsappform.FormRepository = (*FormStore)(nil)

func NewFormStore(db *gorm.DB) *FormStore {
	return &FormStore{db: db}
}

type formRow struct {
	ID                 string
	WorkspaceID        string
	Name               string
	Description        string
	Kind               string
	DraftVersionID     string
	PublishedVersionID schema.OptionalText
	CreatedBy          schema.OptionalText
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ArchivedAt         *time.Time
}

func (r formRow) toDomain() whatsappform.Form {
	return whatsappform.Form{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Name: r.Name, Description: r.Description, Kind: whatsappform.FormKind(r.Kind),
		DraftVersionID: r.DraftVersionID, PublishedVersionID: string(r.PublishedVersionID), CreatedBy: string(r.CreatedBy),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt,
	}
}

type summaryRow struct {
	Form           formRow `gorm:"embedded"`
	Publications   string
	Sent           int64
	Responses      int64
	LastResponseAt *time.Time
}

func (s *FormStore) Create(ctx context.Context, form whatsappform.Form, version whatsappform.Version) error {
	if !database.AllUUIDs(form.ID, form.WorkspaceID, form.DraftVersionID) {
		return errIDsInvalid
	}
	if version.ID != form.DraftVersionID || version.FormID != form.ID || version.WorkspaceID != form.WorkspaceID {
		return errVersionNotTheDraft
	}
	insertVersion, err := versionInsert(version)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Exec(insertFormSQL, form.ID, form.WorkspaceID, form.Name, form.Description, string(form.Kind), form.DraftVersionID,
			schema.OptionalText(form.PublishedVersionID), schema.OptionalText(form.CreatedBy), form.CreatedAt, form.UpdatedAt, form.ArchivedAt).Error
		if err != nil {
			return nameTaken(err)
		}
		return tx.Exec(insertVersionSQL, insertVersion...).Error
	})
}

func versionInsert(v whatsappform.Version) ([]interface{}, error) {
	document, err := jsonText(v.Document, "{}")
	if err != nil {
		return nil, err
	}
	problems, err := jsonText(v.Problems, "[]")
	if err != nil {
		return nil, err
	}
	return []interface{}{
		v.ID, v.WorkspaceID, v.FormID, v.Number, document, rawJSON(v.FlowJSON), v.FlowVersion, v.CompilerVersion, v.Hash, problems,
		schema.OptionalText(v.CreatedBy), v.CreatedAt,
	}, nil
}

func nameTaken(err error) error {
	if database.IsUniqueViolationOf(err, schema.WhatsAppFormLiveNameIndex) {
		return whatsappform.ErrNameTaken
	}
	return err
}

func (s *FormStore) Get(ctx context.Context, workspaceID, formID string) (*whatsappform.Form, error) {
	if !database.AllUUIDs(workspaceID, formID) {
		return nil, whatsappform.ErrFormNotFound
	}
	var rows []formRow
	if err := s.db.WithContext(ctx).Raw(getFormSQL, workspaceID, formID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, whatsappform.ErrFormNotFound
	}
	form := rows[0].toDomain()
	return &form, nil
}

func (s *FormStore) List(ctx context.Context, q whatsappform.FormQuery) ([]whatsappform.FormSummary, error) {
	if !database.AllUUIDs(q.WorkspaceID) {
		return []whatsappform.FormSummary{}, nil
	}
	filter := where(listFormsSQL, q.WorkspaceID)
	if !q.IncludeArchived {
		filter.and(listActiveFormsSQL)
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		filter.and(listFormsSearchSQL, sqlexpr.LikeContains(search))
	}
	filter.and(listFormsOrderSQL)
	var rows []summaryRow
	if err := s.db.WithContext(ctx).Raw(filter.String(), filter.args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	summaries := make([]whatsappform.FormSummary, 0, len(rows))
	for _, row := range rows {
		publications := []whatsappform.PublicationBrief{}
		if err := decodeJSON(row.Publications, &publications); err != nil {
			return nil, err
		}
		summaries = append(summaries, whatsappform.FormSummary{
			Form: row.Form.toDomain(), Publications: publications, Sent: row.Sent, Responses: row.Responses, LastResponseAt: row.LastResponseAt,
		})
	}
	return summaries, nil
}

func (s *FormStore) SaveDraft(ctx context.Context, workspaceID, formID, baseVersionID string, version whatsappform.Version, at time.Time) error {
	if !database.AllUUIDs(workspaceID, formID, baseVersionID, version.ID) {
		return whatsappform.ErrStaleDraft
	}
	if version.FormID != formID || version.WorkspaceID != workspaceID {
		return errVersionNotTheDraft
	}
	insertVersion, err := versionInsert(version)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := database.AffectedOne(tx.Exec(moveDraftSQL, version.ID, at, workspaceID, formID, baseVersionID), whatsappform.ErrStaleDraft); err != nil {
			return err
		}
		err := tx.Exec(insertVersionSQL, insertVersion...).Error
		if database.IsUniqueViolationOf(err, schema.WhatsAppFormVersionNumberIndex) {
			return whatsappform.ErrStaleDraft
		}
		return err
	})
}

func (s *FormStore) Rename(ctx context.Context, workspaceID, formID, name, description string, at time.Time) error {
	if !database.AllUUIDs(workspaceID, formID) {
		return whatsappform.ErrFormNotFound
	}
	result := s.db.WithContext(ctx).Exec(renameFormSQL, name, description, at, workspaceID, formID)
	if result.Error != nil {
		return nameTaken(result.Error)
	}
	return database.AffectedOne(result, whatsappform.ErrFormNotFound)
}

func (s *FormStore) SetPublished(ctx context.Context, workspaceID, formID, versionID string, at time.Time) error {
	if !database.AllUUIDs(workspaceID, formID) {
		return whatsappform.ErrFormNotFound
	}
	if versionID == "" {
		return database.AffectedOne(s.db.WithContext(ctx).Exec(unpublishFormSQL, at, workspaceID, formID), whatsappform.ErrFormNotFound)
	}
	if !database.AllUUIDs(versionID) {
		return whatsappform.ErrFormNotFound
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(publishFormSQL, versionID, at, workspaceID, formID, versionID), whatsappform.ErrFormNotFound)
}

func (s *FormStore) Archive(ctx context.Context, workspaceID, formID string, at time.Time) error {
	if !database.AllUUIDs(workspaceID, formID) {
		return whatsappform.ErrFormNotFound
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(archiveFormSQL, at, at, workspaceID, formID), whatsappform.ErrFormNotFound)
}

func (s *FormStore) PublishedWorkspaces(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).Raw(publishedWorkspacesSQL).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}
