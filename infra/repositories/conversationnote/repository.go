package conversationnote_repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/conversationnote"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type Repository struct {
	db *gorm.DB
}

var _ conversationnote.Repository = (*Repository)(nil)

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, note conversationnote.Note) error {
	row := rowOf(note)
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *Repository) Find(ctx context.Context, workspaceID, id string) (conversationnote.Note, error) {
	if !readableIDs(workspaceID, id) {
		return conversationnote.Note{}, conversationnote.ErrNotFound
	}
	var row schema.ConversationNote
	err := r.live(ctx, workspaceID).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return conversationnote.Note{}, conversationnote.ErrNotFound
	}
	if err != nil {
		return conversationnote.Note{}, err
	}
	return noteOf(row), nil
}

func (r *Repository) ListByEntry(ctx context.Context, q conversationnote.PageQuery) ([]conversationnote.Note, error) {
	if !readableIDs(q.WorkspaceID, q.Entry.EntryID) {
		return []conversationnote.Note{}, nil
	}
	query := r.live(ctx, q.WorkspaceID).
		Where("entry_id = ? AND entry_type = ?", q.Entry.EntryID, string(q.Entry.EntryType))
	if q.Before != nil {
		query = query.Where("created_at < ?", q.Before.UTC())
	}
	var rows []schema.ConversationNote
	if err := query.Order("created_at DESC, id DESC").Limit(q.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	notes := make([]conversationnote.Note, len(rows))
	for i, row := range rows {
		notes[i] = noteOf(row)
	}
	return notes, nil
}

func (r *Repository) MarkDeleted(ctx context.Context, workspaceID, id string, at time.Time) error {
	if !readableIDs(workspaceID, id) {
		return conversationnote.ErrNotFound
	}
	result := r.live(ctx, workspaceID).Model(&schema.ConversationNote{}).
		Where("id = ?", id).
		Update("deleted_at", at.UTC())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return conversationnote.ErrNotFound
	}
	return nil
}

func (r *Repository) live(ctx context.Context, workspaceID string) *gorm.DB {
	return r.db.WithContext(ctx).Where("workspace_id = ? AND deleted_at IS NULL", workspaceID)
}

func readableIDs(ids ...string) bool {
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return false
		}
	}
	return true
}

func uuidArray(ids []string) pq.StringArray {
	if ids == nil {
		return pq.StringArray{}
	}
	return pq.StringArray(ids)
}

func idList(ids pq.StringArray) []string {
	if ids == nil {
		return []string{}
	}
	return []string(ids)
}

func rowOf(note conversationnote.Note) schema.ConversationNote {
	return schema.ConversationNote{
		ID:          note.ID,
		WorkspaceID: note.WorkspaceID,
		EntryID:     note.EntryID,
		EntryType:   string(note.EntryType),
		AuthorID:    note.AuthorID,
		Body:        note.Body,
		Mentions:    uuidArray(note.Mentions),
		ImageIDs:    uuidArray(note.ImageIDs),
		CreatedAt:   note.CreatedAt.UTC(),
		DeletedAt:   note.DeletedAt,
	}
}

func noteOf(row schema.ConversationNote) conversationnote.Note {
	var deletedAt *time.Time
	if row.DeletedAt != nil {
		at := row.DeletedAt.UTC()
		deletedAt = &at
	}
	return conversationnote.Note{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		EntryType:   shared.EntryType(row.EntryType),
		EntryID:     row.EntryID,
		AuthorID:    row.AuthorID,
		Body:        row.Body,
		Mentions:    idList(row.Mentions),
		ImageIDs:    idList(row.ImageIDs),
		CreatedAt:   row.CreatedAt.UTC(),
		DeletedAt:   deletedAt,
	}
}
