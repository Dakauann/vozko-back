package workflow_repository

import (
	"vozko/domain/shared"
	"vozko/domain/workflow"

	"gorm.io/gorm"
)

// ownershipQueries answers "does this workspace own this entry?" per channel.
//
// Each query takes (entry_id, workspace_id) in that order and yields one
// boolean. The check is what lets a webhook-triggered workflow act on a
// conversation, so a channel missing from here is not merely unsupported, its
// workflows are rejected SILENTLY, with no error and no log line, because
// OwnsEntry answers `false, nil`. That is what happened to Instagram, and it is
// why this is a registry rather than a switch with a `default: return false`.
var ownershipQueries = map[shared.EntryType]string{
	shared.EntryTypeWhatsApp: `SELECT EXISTS (
		SELECT 1 FROM whatsapp_campaign_entries e
		JOIN whatsapp_campaigns c ON c.id = e.campaign_id
		WHERE e.id = ? AND c.workspace_id = ? AND e.deleted_at IS NULL
	)`,
	// Instagram and Telegram carry the workspace on the conversation row itself,
	// so ownership needs no join through the container.
	shared.EntryTypeInstagram: `SELECT EXISTS (
		SELECT 1 FROM instagram_conversations c
		WHERE c.id = ?::uuid AND c.workspace_id = ?::uuid AND c.deleted_at IS NULL
	)`,
	shared.EntryTypeTelegram: `SELECT EXISTS (
		SELECT 1 FROM telegram_conversations c
		WHERE c.id = ?::uuid AND c.workspace_id = ?::uuid AND c.deleted_at IS NULL
	)`,
	shared.EntryTypeUnofficialWhatsApp: `SELECT EXISTS (
		SELECT 1 FROM unofficial_whatsapp_conversations c
		WHERE c.id = ?::uuid AND c.workspace_id = ?::uuid AND c.deleted_at IS NULL
	)`,
	shared.EntryTypeSupport: `SELECT EXISTS (
		SELECT 1 FROM support_entries e
		JOIN support_inboxes i ON i.id = e.inbox_id
		WHERE e.id = ?::uuid AND i.workspace_id = ?::uuid AND e.deleted_at IS NULL
	)`,
}

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

	query, ok := ownershipQueries[shared.EntryType(entryType)]
	if !ok {
		return false, nil
	}

	var owns bool
	if err := r.db.Raw(query, entryID, workspaceID).Scan(&owns).Error; err != nil {
		return false, err
	}
	return owns, nil
}
