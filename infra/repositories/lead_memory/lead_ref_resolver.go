package lead_memory_repository

import (
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LeadRefResolver translates the id the conversation UI has on hand into the
// CRM lead id memories key on. For Instagram, Telegram and unofficial WhatsApp
// the inbox projects the channel CONTACT id into the lead slot (see
// entry_sources.go), so the operator panel addresses memories by that id; the
// contact row carries the bridged CRM lead. A real lead id resolves to itself.
type LeadRefResolver struct {
	db *gorm.DB
}

func NewLeadRefResolver(db *gorm.DB) *LeadRefResolver {
	return &LeadRefResolver{db: db}
}

// ResolveLeadRef returns the CRM lead id behind ref, or "" when ref is neither
// a lead in the workspace nor a channel contact bridged to one.
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

	// The join back to leads keeps a stale bridge from resurfacing a lead that
	// no longer exists; the FK on lead_memories would reject it anyway.
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
