package customerportfolio_repository

import (
	"vozko/domain/shared"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

var holderByLeadSQL = "SELECT p.holder_id::text AS holder_id, 0 AS rank FROM customer_portfolio_entries p" +
	" JOIN leads l ON l.id = p.lead_id AND l.workspace_id = p.workspace_id AND l.deleted_at IS NULL" +
	" WHERE p.workspace_id = ? AND p.lead_id IN (SELECT lead_entries.lead_id FROM " + infracrmfilter.LeadEntriesSource() +
	" WHERE lead_entries.entry_id = ? AND lead_entries.entry_type = ?)"

func holderByContactSQL(channel infracrmfilter.ChannelContacts) string {
	return " UNION ALL SELECT p.holder_id::text, 1 FROM customer_portfolio_entries p" +
		" JOIN " + channel.Conversations + " c ON c." + channel.ContactColumn + " = p.contact_id AND c.workspace_id = p.workspace_id AND c.deleted_at IS NULL" +
		" WHERE p.workspace_id = ? AND p.contact_type = ? AND c.id = ?"
}

type holderRow struct {
	HolderID string
	Rank     int
}

func (s *Store) HolderOfConversation(workspaceID, entryID, entryType string) (string, error) {
	if !validUUIDs(workspaceID, entryID) || !shared.EntryType(entryType).Valid() {
		return "", nil
	}
	sql, args := holderByLeadSQL, []interface{}{workspaceID, entryID, entryType}
	if channel, ok := infracrmfilter.ContactsOf(shared.EntryType(entryType)); ok {
		sql += holderByContactSQL(channel)
		args = append(args, workspaceID, entryType, entryID)
	}
	var rows []holderRow
	if err := s.db.Raw("SELECT holder_id, rank FROM ("+sql+") holders ORDER BY rank LIMIT 1", args...).Scan(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].HolderID, nil
}
