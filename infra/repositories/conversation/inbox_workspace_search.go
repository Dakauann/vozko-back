package conversation_repository

import (
	"fmt"

	"github.com/lib/pq"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/sqlexpr"
)

type inboxQueries struct {
	countSQL  string
	countArgs []interface{}
	pageSQL   string
	pageArgs  []interface{}
}

func inboxPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return pageSize, (page - 1) * pageSize
}

func workspaceInboxQueries(input conversation.SearchEntriesInput, pageSize, offset int) (inboxQueries, bool) {
	entryCTE, entryArgs := buildEntryUnion(entrySourceScope{
		EntryType:              input.EntryType,
		WhatsAppCampaignType:   input.WhatsAppCampaignType,
		ConversationStatus:     string(input.ConversationStatus),
		ExcludeFinished:        true,
		DepartmentIDs:          input.DepartmentIDs,
		RestrictDepartments:    input.RestrictDepartments,
		AssigneeOverrideUserID: input.AssigneeOverrideUserID,
	}, input.WorkspaceID, entrySource.inboxSelect)
	if entryCTE == "" {
		return inboxQueries{}, false
	}

	whereConditions, whereArgs := workspaceInboxConditions(input)
	personal := personalScopeOf(input)
	if condition := personal.condition(); condition != "" {
		whereConditions = append(whereConditions, condition)
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + joinConditions(whereConditions)
	}

	leadsJoin := ""
	if input.Query != "" {
		leadsJoin = "LEFT JOIN leads l ON l.id = ae.lead_id AND l.deleted_at IS NULL"
	}
	marksJoin, marksArgs := personal.join("ae.entry_id", "ae.entry_type")

	selected := "ae.entry_id, ae.entry_type, ae.lead_id, ae.business_phone_id, ae.lm_created_at AS lm_created_at, ae.ws_at"
	order := inboxOrderColumns{LastMessage: "lm_created_at", WaitingSince: "ws_at"}
	if pinned := personal.pinnedColumn(); pinned != "" {
		selected += ", " + pinned
		order.Pinned = "pinned"
	}

	baseCTE := fmt.Sprintf(`
		WITH all_entries AS (%s),
		entries_with_msg AS (
			SELECT %s
			FROM all_entries ae
			%s
			%s
			%s
		)
	`, entryCTE, selected, leadsJoin, marksJoin, whereClause)

	args := append(append(append([]interface{}{}, entryArgs...), marksArgs...), whereArgs...)
	return inboxQueries{
		countSQL:  baseCTE + " SELECT COUNT(*) FROM entries_with_msg",
		countArgs: args,
		pageSQL: fmt.Sprintf(`%s
		SELECT entry_id, entry_type FROM entries_with_msg
		%s
		LIMIT ? OFFSET ?
	`, baseCTE, inboxOrderBySQL(input.SortOrder, order)),
		pageArgs: append(append([]interface{}{}, args...), pageSize, offset),
	}, true
}

func workspaceInboxConditions(input conversation.SearchEntriesInput) ([]string, []interface{}) {
	whereConditions := []string{}
	whereArgs := []interface{}{}

	if input.Query != "" {
		queryPattern := "%%" + input.Query + "%%"
		whereConditions = append(whereConditions, `(
			LOWER(COALESCE(l.name, '')) LIKE LOWER(?) OR COALESCE(l.number, '') LIKE ?
			OR ae.entry_id IN (
				SELECT DISTINCT cm_q.entry_id
				FROM conversation_messages cm_q
				WHERE cm_q.entry_id = ae.entry_id AND cm_q.entry_type = ae.entry_type
				  AND `+sqlexpr.MessageTextMatches("cm_q")+`
			)
		)`)
		whereArgs = append(whereArgs, queryPattern, queryPattern, queryPattern)
	}

	if len(input.StageIDs) > 0 && input.StageWorkspaceID != "" {
		if len(input.StageIDs) == 1 && input.StageIDs[0] == "__unstaged__" {
			whereConditions = append(whereConditions, "ae.entry_id NOT IN (SELECT entry_id FROM entry_stages WHERE workspace_id = ? AND deleted_at IS NULL)")
			whereArgs = append(whereArgs, input.StageWorkspaceID)
		} else {
			whereConditions = append(whereConditions, "ae.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)")
			whereArgs = append(whereArgs, pq.Array(input.StageIDs), input.StageWorkspaceID)
		}
	}

	if input.LabelID != "" {
		condition, args := labelCondition(input.LabelID, "ae.entry_id", "ae.entry_type")
		whereConditions = append(whereConditions, condition)
		whereArgs = append(whereArgs, args...)
	}

	if input.HasUnread != nil {
		unreadSub := `(SELECT COUNT(*) FROM conversation_messages cm4 WHERE cm4.entry_id = ae.entry_id AND cm4.entry_type = ae.entry_type AND cm4.read = false AND ` + sqlexpr.SentByContact("cm4") + ` AND cm4.deleted_at IS NULL)`
		if *input.HasUnread {
			whereConditions = append(whereConditions, fmt.Sprintf("%s > 0", unreadSub))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("%s = 0", unreadSub))
		}
	}

	if input.Channel != "" {
		whereConditions = append(whereConditions, `EXISTS (
			SELECT 1 FROM conversation_messages cm_ch
			WHERE cm_ch.entry_id = ae.entry_id AND cm_ch.entry_type = ae.entry_type
			  AND cm_ch.channel = ? AND cm_ch.deleted_at IS NULL
		)`)
		whereArgs = append(whereArgs, input.Channel)
	}

	if input.MessageSearch != "" {
		whereConditions = append(whereConditions, `EXISTS (
			SELECT 1 FROM conversation_messages cm_ms
			WHERE cm_ms.entry_id = ae.entry_id AND cm_ms.entry_type = ae.entry_type
			  AND `+sqlexpr.MessageTextMatches("cm_ms")+`
		)`)
		whereArgs = append(whereArgs, "%%"+input.MessageSearch+"%%")
	}

	if input.AssignedUserID != "" {
		whereConditions = append(whereConditions, "(NOT EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = ae.entry_id) OR EXISTS (SELECT 1 FROM inbox_assignments ia2 WHERE ia2.entry_id = ae.entry_id AND ia2.assigned_user_id = ?))")
		whereArgs = append(whereArgs, input.AssignedUserID)
	}

	if input.ResponsibleUnassigned {
		whereConditions = append(whereConditions, "NOT EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = ae.entry_id)")
	} else if input.ResponsibleKind != "" {
		whereConditions = append(whereConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = ae.entry_id AND iaf.assignee_kind = ?)")
		whereArgs = append(whereArgs, string(input.ResponsibleKind))
	} else if input.ResponsibleUserID != "" {
		whereConditions = append(whereConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = ae.entry_id AND iaf.assigned_user_id = ?)")
		whereArgs = append(whereArgs, input.ResponsibleUserID)
	}

	if input.WindowOpen != nil {
		whereConditions = append(whereConditions, windowFilterCondition(*input.WindowOpen, "ae.entry_type", "ae.entry_id", input.EntryType))
	}

	if input.IsGroup != nil {
		whereConditions = append(whereConditions, groupFilterCondition(*input.IsGroup, "ae.entry_type", "ae.entry_id", input.EntryType))
	}

	if input.MinMessageCount != nil || input.MaxMessageCount != nil {
		countSub := `(SELECT COUNT(*) FROM conversation_messages cm3 WHERE cm3.entry_id = ae.entry_id AND cm3.entry_type = ae.entry_type AND cm3.deleted_at IS NULL)`
		if input.MinMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s >= ?", countSub))
			whereArgs = append(whereArgs, *input.MinMessageCount)
		}
		if input.MaxMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s <= ?", countSub))
			whereArgs = append(whereArgs, *input.MaxMessageCount)
		}
	}

	if input.DateFrom != nil {
		whereConditions = append(whereConditions, "ae.lm_created_at >= ?")
		whereArgs = append(whereArgs, *input.DateFrom)
	}
	if input.DateTo != nil {
		whereConditions = append(whereConditions, "ae.lm_created_at <= ?")
		whereArgs = append(whereArgs, *input.DateTo)
	}

	return whereConditions, whereArgs
}

func (r *repository) searchEntriesByWorkspace(input conversation.SearchEntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {
	pageSize, offset := inboxPage(input.Page, input.PageSize)
	queries, ok := workspaceInboxQueries(input, pageSize, offset)
	if !ok {
		return nil, 0, nil
	}

	var totalCount int64
	if err := r.db.Raw(queries.countSQL, queries.countArgs...).Scan(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("error counting workspace search results: %w", err)
	}
	if totalCount == 0 {
		return nil, 0, nil
	}

	type idResult struct {
		EntryID   string `gorm:"column:entry_id"`
		EntryType string `gorm:"column:entry_type"`
	}
	var matchedIDs []idResult
	if err := r.db.Raw(queries.pageSQL, queries.pageArgs...).Scan(&matchedIDs).Error; err != nil {
		return nil, 0, fmt.Errorf("error searching workspace entries: %w", err)
	}
	if len(matchedIDs) == 0 {
		return nil, totalCount, nil
	}

	refs := make([]entryRef, 0, len(matchedIDs))
	for _, m := range matchedIDs {
		refs = append(refs, entryRef{ID: m.EntryID, Type: shared.EntryType(m.EntryType)})
	}
	entries, err := r.hydrateInOrder(refs)
	if err != nil {
		return nil, 0, fmt.Errorf("error hydrating workspace search results: %w", err)
	}
	return entries, totalCount, nil
}

func (r *repository) hydrateInOrder(refs []entryRef) ([]conversation.EntryWithLastMessage, error) {
	position := make(map[entryRef]int, len(refs))
	for i, ref := range refs {
		position[ref] = i
	}
	ordered := make([]conversation.EntryWithLastMessage, len(refs))
	found := make([]bool, len(refs))
	for _, batch := range hydrationBatches(refs) {
		entries, _, err := r.GetEntriesWithMessages("", batch.IDs, batch.EntryType, 1, len(batch.IDs), "")
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if i, ok := position[entryRef{ID: entry.EntryID, Type: batch.EntryType}]; ok {
				ordered[i], found[i] = entry, true
			}
		}
	}
	out := make([]conversation.EntryWithLastMessage, 0, len(refs))
	for i, entry := range ordered {
		if found[i] {
			out = append(out, entry)
		}
	}
	return out, nil
}
