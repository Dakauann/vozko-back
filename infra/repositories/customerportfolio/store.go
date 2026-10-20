package customerportfolio_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	cp "vozko/domain/customerportfolio"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

const (
	entryColumns = "p.id::text AS id, p.workspace_id::text AS workspace_id, p.holder_id::text AS holder_id," +
		" p.lead_id::text AS lead_id, p.contact_type, p.contact_id::text AS contact_id, p.added_by::text AS added_by, p.created_at, p.updated_at"

	insertEntryHeadSQL = "INSERT INTO customer_portfolio_entries (id, workspace_id, holder_id, lead_id, contact_type, contact_id, added_by, created_at, updated_at)" +
		" SELECT ?, ?, ?, ?::uuid, ?, ?::uuid, ?::uuid, ?, ? WHERE EXISTS ("

	insertEntryTailSQL = ") ON CONFLICT DO NOTHING RETURNING id::text"

	leadInWorkspaceSQL = "SELECT 1 FROM leads WHERE id = ?::uuid AND workspace_id = ? AND deleted_at IS NULL"

	updateHolderSQL = "UPDATE customer_portfolio_entries SET holder_id = ?, updated_at = ? WHERE workspace_id = ? AND id = ?"

	deleteEntrySQL = "DELETE FROM customer_portfolio_entries WHERE workspace_id = ? AND id = ?"
)

type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

type entryRow struct {
	ID          string
	WorkspaceID string
	HolderID    string
	LeadID      schema.OptionalText
	ContactType schema.OptionalText
	ContactID   schema.OptionalText
	AddedBy     schema.OptionalText
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r entryRow) toDomain() cp.Entry {
	return cp.Entry{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		HolderID:    r.HolderID,
		Subject:     cp.Subject{LeadID: string(r.LeadID), ContactType: shared.EntryType(r.ContactType), ContactID: string(r.ContactID)},
		AddedBy:     string(r.AddedBy),
		AddedAt:     r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func (s *Store) Insert(ctx context.Context, e cp.Entry) (cp.Entry, bool, error) {
	exists, args, err := subjectInWorkspace(e.WorkspaceID, e.Subject)
	if err != nil {
		return cp.Entry{}, false, err
	}
	values := append([]interface{}{
		e.ID, e.WorkspaceID, e.HolderID,
		schema.OptionalText(e.Subject.LeadID), schema.OptionalText(e.Subject.ContactType), schema.OptionalText(e.Subject.ContactID),
		schema.OptionalText(e.AddedBy), e.AddedAt, e.UpdatedAt,
	}, args...)
	var ids []string
	if err := s.db.WithContext(ctx).Raw(insertEntryHeadSQL+exists+insertEntryTailSQL, values...).Scan(&ids).Error; err != nil {
		return cp.Entry{}, false, err
	}
	if len(ids) == 1 {
		return e, true, nil
	}
	existing, err := s.FindBySubject(ctx, e.WorkspaceID, e.Subject)
	if err != nil {
		return cp.Entry{}, false, err
	}
	if existing == nil {
		return cp.Entry{}, false, cp.ErrSubjectNotFound
	}
	return *existing, false, nil
}

func subjectInWorkspace(workspaceID string, s cp.Subject) (string, []interface{}, error) {
	if !validUUIDs(workspaceID) {
		return "", nil, cp.ErrSubjectNotFound
	}
	if s.Kind() == cp.SubjectLead {
		if !validUUIDs(s.LeadID) {
			return "", nil, cp.ErrSubjectNotFound
		}
		return leadInWorkspaceSQL, []interface{}{s.LeadID, workspaceID}, nil
	}
	channel, ok := infracrmfilter.ContactsOf(s.ContactType)
	if !ok || !validUUIDs(s.ContactID) {
		return "", nil, cp.ErrSubjectNotFound
	}
	return "SELECT 1 FROM " + channel.Contacts + " WHERE id = ?::uuid AND workspace_id = ? AND deleted_at IS NULL", []interface{}{s.ContactID, workspaceID}, nil
}

func (s *Store) Get(ctx context.Context, workspaceID, id string) (cp.Entry, error) {
	if !validUUIDs(workspaceID, id) {
		return cp.Entry{}, cp.ErrEntryNotFound
	}
	rows, err := s.entries(ctx, "p.workspace_id = ? AND p.id = ?", workspaceID, id)
	if err != nil {
		return cp.Entry{}, err
	}
	if len(rows) == 0 {
		return cp.Entry{}, cp.ErrEntryNotFound
	}
	return rows[0], nil
}

func (s *Store) FindBySubject(ctx context.Context, workspaceID string, subject cp.Subject) (*cp.Entry, error) {
	if !validUUIDs(workspaceID) {
		return nil, nil
	}
	var (
		rows []cp.Entry
		err  error
	)
	switch {
	case subject.Kind() == cp.SubjectLead && validUUIDs(subject.LeadID):
		rows, err = s.entries(ctx, "p.workspace_id = ? AND p.lead_id = ?", workspaceID, subject.LeadID)
	case subject.Kind() == cp.SubjectContact && validUUIDs(subject.ContactID):
		rows, err = s.entries(ctx, "p.workspace_id = ? AND p.contact_type = ? AND p.contact_id = ?", workspaceID, string(subject.ContactType), subject.ContactID)
	}
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (s *Store) entries(ctx context.Context, where string, args ...interface{}) ([]cp.Entry, error) {
	var rows []entryRow
	if err := s.db.WithContext(ctx).Raw("SELECT "+entryColumns+" FROM customer_portfolio_entries p WHERE "+where, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	entries := make([]cp.Entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, row.toDomain())
	}
	return entries, nil
}

func (s *Store) SaveHolder(ctx context.Context, e cp.Entry) error {
	if !validUUIDs(e.WorkspaceID, e.ID, e.HolderID) {
		return cp.ErrEntryNotFound
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(updateHolderSQL, e.HolderID, e.UpdatedAt, e.WorkspaceID, e.ID), cp.ErrEntryNotFound)
}

func (s *Store) Delete(ctx context.Context, workspaceID, id string) error {
	if !validUUIDs(workspaceID, id) {
		return cp.ErrEntryNotFound
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(deleteEntrySQL, workspaceID, id), cp.ErrEntryNotFound)
}

func (s *Store) ContactOfConversation(ctx context.Context, workspaceID string, ref shared.EntryRef) (cp.Subject, error) {
	channel, ok := infracrmfilter.ContactsOf(ref.EntryType)
	if !ok || !validUUIDs(workspaceID, ref.EntryID) {
		return cp.Subject{}, cp.ErrSubjectNotFound
	}
	var ids []string
	sql := "SELECT " + channel.ContactColumn + "::text FROM " + channel.Conversations + " WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL"
	if err := s.db.WithContext(ctx).Raw(sql, ref.EntryID, workspaceID).Scan(&ids).Error; err != nil {
		return cp.Subject{}, err
	}
	if len(ids) == 0 || ids[0] == "" {
		return cp.Subject{}, cp.ErrSubjectNotFound
	}
	return cp.ContactSubject(ref.EntryType, ids[0]), nil
}

func validUUIDs(ids ...string) bool {
	for _, id := range ids {
		if len(database.UUIDArray([]string{id})) == 0 {
			return false
		}
	}
	return true
}
