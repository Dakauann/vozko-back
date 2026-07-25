package workflow_repository

import (
	"vozko/domain/workflow"

	"gorm.io/gorm"
)

type entryOwnershipRepository struct {
	db *gorm.DB
}

func NewEntryOwnershipRepository(db *gorm.DB) workflow.EntryOwnershipChecker {
	return &entryOwnershipRepository{db: db}
}

func (r *entryOwnershipRepository) OwnsEntry(workspaceID, entryID, entryType string) (bool, error) {
	if workspaceID == "" || entryID == "" {
		return false, nil
	}

	var query string
	switch entryType {
	case "whatsapp":
		query = `SELECT EXISTS (
			SELECT 1 FROM whatsapp_campaign_entries e
			JOIN whatsapp_campaigns c ON c.id = e.campaign_id
			WHERE e.id = ? AND c.workspace_id = ? AND e.deleted_at IS NULL
		)`
	default:
		return false, nil
	}

	var owns bool
	if err := r.db.Raw(query, entryID, workspaceID).Scan(&owns).Error; err != nil {
		return false, err
	}
	return owns, nil
}
