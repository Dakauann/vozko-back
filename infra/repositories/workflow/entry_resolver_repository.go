package workflow_repository

import (
	"strings"

	"vozko/domain/lead"
	"vozko/domain/workflow"

	"gorm.io/gorm"
)

type entryResolverRepository struct {
	db *gorm.DB
}

func NewEntryResolverRepository(db *gorm.DB) workflow.EntryResolver {
	return &entryResolverRepository{db: db}
}

func (r *entryResolverRepository) ResolveByPhone(workspaceID, phone string) (string, string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	canonical := lead.NormalizeRawNumber(phone)
	if workspaceID == "" || canonical == "" {
		return "", "", nil
	}

	numbers := []string{canonical}
	if alt := lead.GetAlternatePhoneFormat(canonical); alt != "" && alt != canonical {
		numbers = append(numbers, alt)
	}

	var ids []string
	err := r.db.
		Table("whatsapp_campaign_entries AS e").
		Joins("JOIN whatsapp_campaigns c ON c.id = e.campaign_id").
		Joins("JOIN leads l ON l.id = e.lead_id").
		Where("c.workspace_id = ? AND e.deleted_at IS NULL AND l.number IN ?", workspaceID, numbers).
		Order("e.last_message_at DESC NULLS LAST").
		Order("e.created_at DESC").
		Limit(1).
		Pluck("e.id", &ids).Error
	if err != nil {
		return "", "", err
	}
	if len(ids) == 0 {
		return "", "", nil
	}
	return ids[0], "whatsapp", nil
}
