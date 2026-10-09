package conversation_repository

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"vozko/domain/conversation"
	"vozko/domain/selection"
	"vozko/domain/shared"
	crmfiltersql "vozko/infra/repositories/crmfilter"
)

var errFilterWorkspaceRequired = errors.New("filtered entries: workspace id is required")

type entryCondition struct {
	sql  string
	args []interface{}
}

func filteredEntriesCTE(input conversation.SearchByFilterInput, extra ...entryCondition) (string, []interface{}, error) {
	wsID := input.WorkspaceID
	if wsID == "" {
		return "", nil, errFilterWorkspaceRequired
	}

	desc := crmfiltersql.NewConversationDescriptor()
	desc.WorkspaceID = wsID
	desc.LastActivityExpr = "ae.lm_created_at"
	whereSQL, whereArgs, err := crmfiltersql.Compile(input.Filter, desc, 1)
	if err != nil {
		return "", nil, err
	}

	entryCTE, entryArgs := buildEntryUnion(entrySourceScope{
		WhatsAppCampaignType:   input.WhatsAppCampaignType,
		DepartmentIDs:          input.DepartmentIDs,
		RestrictDepartments:    input.RestrictDepartments,
		AssigneeOverrideUserID: input.AssigneeOverrideUserID,
		AssignedUserID:         input.AssignedUserID,
	}, wsID, entrySource.boardSelect)

	conditions := make([]string, 0, 2+len(extra))
	if strings.TrimSpace(whereSQL) != "" {
		conditions = append(conditions, "("+whereSQL+")")
	}
	if len(input.ExcludeEntryIDs) > 0 {
		conditions = append(conditions, "NOT (ae.entry_id::text = ANY(?))")
		whereArgs = append(whereArgs, pq.Array(input.ExcludeEntryIDs))
	}
	for _, c := range extra {
		conditions = append(conditions, c.sql)
		whereArgs = append(whereArgs, c.args...)
	}
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	cte := fmt.Sprintf(`
		WITH all_entries AS (%s),
		entries_with_msg AS (
			SELECT ae.entry_id, ae.entry_type, ae.lead_id, ae.business_phone_id,
			       ae.created_at AS created_at,
			       COALESCE(l.name, '') AS lead_name, COALESCE(l.number, '') AS lead_number,
			       ae.lm_created_at AS lm_created_at
			FROM all_entries ae
			LEFT JOIN leads l ON l.id = ae.lead_id AND l.deleted_at IS NULL
			%s
		)
	`, entryCTE, whereClause)

	return cte, append(append([]interface{}{}, entryArgs...), whereArgs...), nil
}

func (r *repository) countFilteredEntries(cte string, args []interface{}) (int64, error) {
	var total int64
	if err := r.db.Raw(cte+" SELECT COUNT(*) FROM entries_with_msg", args...).Scan(&total).Error; err != nil {
		return 0, fmt.Errorf("error counting filtered board results: %w", err)
	}
	return total, nil
}

func (r *repository) CountEntriesByFilter(input conversation.SearchByFilterInput) (int64, error) {
	cte, args, err := filteredEntriesCTE(input)
	if err != nil {
		return 0, fmt.Errorf("CountEntriesByFilter: %w", err)
	}
	return r.countFilteredEntries(cte, args)
}

func entryRefPageQuery(input conversation.SearchByFilterInput, after string, limit int) (string, []interface{}, error) {
	if err := selection.ValidatePage(limit); err != nil {
		return "", nil, err
	}
	cte, args, err := filteredEntriesCTE(input, keysetAfter(after)...)
	if err != nil {
		return "", nil, err
	}
	page := cte + ` SELECT ewm.entry_id::text AS entry_id, ewm.entry_type FROM entries_with_msg ewm ORDER BY ewm.entry_id::text LIMIT ?`
	return page, append(args, limit), nil
}

func keysetAfter(after string) []entryCondition {
	if after == "" {
		return nil
	}
	return []entryCondition{{sql: "ae.entry_id::text > ?", args: []interface{}{after}}}
}

func (r *repository) ResolveEntryRefsByFilter(input conversation.SearchByFilterInput, after string, limit int) ([]shared.EntryRef, error) {
	page, args, err := entryRefPageQuery(input, after, limit)
	if err != nil {
		return nil, fmt.Errorf("ResolveEntryRefsByFilter: %w", err)
	}
	var rows []struct {
		EntryID   string `gorm:"column:entry_id"`
		EntryType string `gorm:"column:entry_type"`
	}
	if err := r.db.Raw(page, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("error resolving filtered board entries: %w", err)
	}
	refs := make([]shared.EntryRef, len(rows))
	for i, row := range rows {
		refs[i] = shared.EntryRef{EntryID: row.EntryID, EntryType: shared.EntryType(row.EntryType)}
	}
	return refs, nil
}
