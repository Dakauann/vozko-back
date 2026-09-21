package lead_memory_repository

import (
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LeadRefResolver struct {
	db *gorm.DB
}

func NewLeadRefResolver(db *gorm.DB) *LeadRefResolver {
	return &LeadRefResolver{db: db}
}

func (r *LeadRefResolver) ResolveLeadRef(workspaceID, ref string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	ref = strings.TrimSpace(ref)
	if _, err := uuid.Parse(ref); err != nil {
		return ""
	}
	if _, err := uuid.Parse(workspaceID); err != nil {
		return ""
	}

	var leadID string
	r.db.Raw(
		`SELECT id FROM leads WHERE workspace_id = ?::uuid AND id = ?::uuid AND deleted_at IS NULL`,
		workspaceID, ref,
	).Scan(&leadID)
	if leadID != "" {
		return leadID
	}

	r.db.Raw(`
		SELECT l.id FROM leads l
		WHERE l.workspace_id = ?::uuid AND l.deleted_at IS NULL AND l.id IN (
			SELECT c.lead_id FROM unofficial_whatsapp_contacts c WHERE c.id = ?::uuid AND c.workspace_id = ?::uuid AND c.lead_id IS NOT NULL
			UNION ALL
			SELECT c.lead_id FROM telegram_contacts c WHERE c.id = ?::uuid AND c.workspace_id = ?::uuid AND c.lead_id IS NOT NULL
			UNION ALL
			SELECT c.lead_id FROM instagram_contacts c WHERE c.id = ?::uuid AND c.workspace_id = ?::uuid AND c.lead_id IS NOT NULL
		)
		LIMIT 1`,
		workspaceID, ref, workspaceID, ref, workspaceID, ref, workspaceID,
	).Scan(&leadID)
	return leadID
}
