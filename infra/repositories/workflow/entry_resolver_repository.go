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

// ResolveByPhone maps an inbound phone number to the most recently active
// WhatsApp campaign entry in the workspace. The phone lives on the lead
// (whatsapp_campaign_entries.lead_id -> leads.number), so it normalizes the raw
// number with the shared lead helpers (BR country code plus the 9th-digit
// alternate) and matches either form. The campaign join keeps the lookup scoped
// to workspaceID, so it can never surface an entry from another workspace.
// "most recent activity" wins ties via last_message_at. Returns ("", "", nil)
// when the phone matches no entry.
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
