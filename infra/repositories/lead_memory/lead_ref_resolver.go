package lead_memory_repository

import (
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/shared"
)

var leadRefIsTheLead = map[shared.EntryType]struct{}{
	shared.EntryTypeWhatsApp: {},
}

var leadLinkedContactTables = map[shared.EntryType]string{
	shared.EntryTypeUnofficialWhatsApp: "unofficial_whatsapp_contacts",
	shared.EntryTypeTelegram:           "telegram_contacts",
	shared.EntryTypeInstagram:          "instagram_contacts",
	shared.EntryTypeFacebook:           "facebook_contacts",
	shared.EntryTypeWebchat:            "webchat_visitors",
	shared.EntryTypeMercadoLivre:       "mercadolivre_contacts",
	shared.EntryTypeTikTok:             "tiktok_contacts",
	shared.EntryTypeEmail:              "email_contacts",
	shared.EntryTypeRCS:                "carrier_contacts",
	shared.EntryTypeSMS:                "carrier_contacts",
}

var leadViaContactsQuery = buildLeadViaContactsSQL()

func sortedContactTables() []string {
	seen := make(map[string]bool, len(leadLinkedContactTables))
	tables := make([]string, 0, len(leadLinkedContactTables))
	for _, table := range leadLinkedContactTables {
		if !seen[table] {
			seen[table] = true
			tables = append(tables, table)
		}
	}
	sort.Strings(tables)
	return tables
}

func buildLeadViaContactsSQL() string {
	tables := sortedContactTables()
	branches := make([]string, 0, len(tables))
	for _, table := range tables {
		branches = append(branches, "SELECT c.lead_id FROM "+table+" c WHERE c.id = ?::uuid AND c.workspace_id = ?::uuid AND c.lead_id IS NOT NULL")
	}
	return "SELECT l.id FROM leads l WHERE l.workspace_id = ?::uuid AND l.deleted_at IS NULL AND l.id IN (" +
		strings.Join(branches, " UNION ALL ") +
		") LIMIT 1"
}

func leadViaContactsSQL() string {
	return leadViaContactsQuery
}

func leadViaContactsArgs(workspaceID, ref string) []any {
	tables := sortedContactTables()
	args := make([]any, 0, 1+2*len(tables))
	args = append(args, workspaceID)
	for range tables {
		args = append(args, ref, workspaceID)
	}
	return args
}

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

	r.db.Raw(leadViaContactsSQL(), leadViaContactsArgs(workspaceID, ref)...).Scan(&leadID)
	return leadID
}
