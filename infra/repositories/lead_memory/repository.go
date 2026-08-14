package lead_memory_repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	leadmemory "vozko/domain/lead_memory"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) leadmemory.Repository {
	return &repository{db: db}
}

func (r *repository) Create(m *leadmemory.LeadMemory) error {
	row := fromDomain(m)
	if err := r.db.Create(&row).Error; err != nil {
		if isDuplicateKey(err) {
			// The partial unique index on (workspace_id, lead_id, content_norm)
			// fired: an active memory with this content already exists. The use
			// case resolves the race by re-reading.
			return leadmemory.ErrDuplicate
		}
		return err
	}
	m.ID = row.ID
	m.CreatedAt = row.CreatedAt
	m.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) FindByID(workspaceID, id string) (*leadmemory.LeadMemory, error) {
	var row schema.LeadMemory
	err := r.db.Where("workspace_id = ? AND id = ?", workspaceID, id).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, leadmemory.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) FindByIDPrefix(workspaceID, leadID, prefix string) (*leadmemory.LeadMemory, error) {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if len(prefix) < leadmemory.MinIDPrefixLen {
		return nil, leadmemory.ErrNotFound
	}

	// LIMIT 2: one row is a resolution, two is an ambiguity, and we never need
	// to know how many more there were.
	var rows []schema.LeadMemory
	err := r.db.
		Where("workspace_id = ? AND lead_id = ? AND id::text LIKE ?", workspaceID, leadID, prefix+"%").
		Limit(2).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		return nil, leadmemory.ErrNotFound
	case 1:
		return toDomain(&rows[0]), nil
	default:
		return nil, leadmemory.ErrAmbiguousID
	}
}

func (r *repository) ListByLead(workspaceID, leadID string, q leadmemory.ListQuery) ([]*leadmemory.LeadMemory, int64, error) {
	base := r.db.Model(&schema.LeadMemory{}).
		Where("workspace_id = ? AND lead_id = ?", workspaceID, leadID)
	if q.Category != nil {
		base = base.Where("category = ?", string(*q.Category))
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := q.Limit
	if limit < 1 || limit > leadmemory.MaxListLimit {
		limit = leadmemory.DefaultListLimit
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	var rows []schema.LeadMemory
	err := base.Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return toDomainSlice(rows), total, nil
}

func (r *repository) CountByLead(workspaceID, leadID string) (int64, error) {
	var total int64
	err := r.db.Model(&schema.LeadMemory{}).
		Where("workspace_id = ? AND lead_id = ?", workspaceID, leadID).
		Count(&total).Error
	return total, err
}

func (r *repository) FindByNormalizedContent(workspaceID, leadID, contentNorm string) (*leadmemory.LeadMemory, error) {
	var row schema.LeadMemory
	err := r.db.
		Where("workspace_id = ? AND lead_id = ? AND content_norm = ?", workspaceID, leadID, contentNorm).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, leadmemory.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) Update(m *leadmemory.LeadMemory) error {
	// Column-explicit update guarded on workspace: only the fields an edit may
	// change. The actor columns become the last writer: original authorship
	// lives on the created timeline event, not on the row.
	result := r.db.Model(&schema.LeadMemory{}).
		Where("workspace_id = ? AND id = ?", m.WorkspaceID, m.ID).
		Updates(map[string]interface{}{
			"category":     string(m.Category),
			"content":      m.Content,
			"content_norm": leadmemory.NormalizeContent(m.Content),
			"actor_kind":   string(m.ActorKind),
			"actor_id":     m.ActorID,
		})
	if result.Error != nil {
		if isDuplicateKey(result.Error) {
			return leadmemory.ErrDuplicate
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return leadmemory.ErrNotFound
	}
	return nil
}

func (r *repository) SoftDelete(workspaceID, id string) error {
	result := r.db.
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Delete(&schema.LeadMemory{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return leadmemory.ErrNotFound
	}
	return nil
}

func isDuplicateKey(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}

var _ leadmemory.Repository = (*repository)(nil)
