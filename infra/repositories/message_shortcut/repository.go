package message_shortcut_repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/message_shortcut"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) message_shortcut.Repository {
	return &repository{db: db}
}

func (r *repository) Create(s *message_shortcut.MessageShortcut) error {
	row := fromDomain(s)
	if err := r.db.Create(&row).Error; err != nil {
		return err
	}
	s.ID = row.ID
	s.CreatedAt = row.CreatedAt
	s.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) Update(s *message_shortcut.MessageShortcut) error {
	update := map[string]interface{}{
		"name":         s.Name,
		"shortcut":     s.Shortcut,
		"message_type": string(s.MessageType),
		"content":      s.Content,
	}
	result := r.db.Model(&schema.MessageShortcut{}).Where("id = ? AND deleted_at IS NULL", s.ID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return message_shortcut.ErrNotFound
	}
	return nil
}

func (r *repository) Delete(id string) error {
	result := r.db.Where("id = ?", id).Delete(&schema.MessageShortcut{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return message_shortcut.ErrNotFound
	}
	return nil
}

func (r *repository) FindByID(id string) (*message_shortcut.MessageShortcut, error) {
	var row schema.MessageShortcut
	if err := r.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, message_shortcut.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) FindByShortcut(workspaceID, shortcut string) (*message_shortcut.MessageShortcut, error) {
	var row schema.MessageShortcut
	if err := r.db.Where("workspace_id = ? AND shortcut = ?", workspaceID, strings.ToLower(shortcut)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, message_shortcut.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) ListByWorkspace(workspaceID string) ([]*message_shortcut.MessageShortcut, error) {
	var rows []schema.MessageShortcut
	if err := r.db.Where("workspace_id = ?", workspaceID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*message_shortcut.MessageShortcut, len(rows))
	for i := range rows {
		result[i] = toDomain(&rows[i])
	}
	return result, nil
}

func (r *repository) ShortcutExistsInWorkspace(workspaceID, shortcut string, excludeID *string) (bool, error) {
	query := r.db.Model(&schema.MessageShortcut{}).Where("workspace_id = ? AND shortcut = ?", workspaceID, strings.ToLower(shortcut))
	if excludeID != nil && *excludeID != "" {
		query = query.Where("id <> ?", *excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func toDomain(row *schema.MessageShortcut) *message_shortcut.MessageShortcut {
	return &message_shortcut.MessageShortcut{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Shortcut:    row.Shortcut,
		Name:        row.Name,
		MessageType: message_shortcut.MessageType(row.MessageType),
		Content:     row.Content,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func fromDomain(s *message_shortcut.MessageShortcut) *schema.MessageShortcut {
	return &schema.MessageShortcut{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		Shortcut:    s.Shortcut,
		Name:        s.Name,
		MessageType: string(s.MessageType),
		Content:     s.Content,
	}
}
