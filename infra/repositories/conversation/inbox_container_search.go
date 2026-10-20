package conversation_repository

import (
	"fmt"
	"time"

	"github.com/lib/pq"

	"vozko/domain/conversation"
	"vozko/infra/database/sqlexpr"
)

func (r *repository) SearchEntriesWithMessages(input conversation.SearchEntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {
	if input.WorkspaceID != "" && input.CampaignID == "" {
		return r.searchEntriesByWorkspace(input)
	}

	var filteredIDs []string
	if input.CampaignID == "" {
		for _, id := range input.EntryIDs {
			if id != "" {
				filteredIDs = append(filteredIDs, id)
			}
		}
		if len(filteredIDs) == 0 {
			return nil, 0, nil
		}
	}

	ch, ok := channelQueryFor(input.EntryType)
	if !ok {
		return nil, 0, fmt.Errorf("invalid entry type: %s", input.EntryType)
	}

	pageSize, offset := inboxPage(input.Page, input.PageSize)
	queries := containerInboxQueries(input, ch, filteredIDs, pageSize, offset)

	var totalCount int64
	if err := r.db.Raw(queries.countSQL, queries.countArgs...).Scan(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("error counting search results: %w", err)
	}
	if totalCount == 0 {
		return nil, 0, nil
	}

	type idResult struct {
		EntryID string `gorm:"column:entry_id"`
	}
	var matchedIDs []idResult
	if err := r.db.Raw(queries.pageSQL, queries.pageArgs...).Scan(&matchedIDs).Error; err != nil {
		return nil, 0, fmt.Errorf("error getting search results: %w", err)
	}
	if len(matchedIDs) == 0 {
		return nil, totalCount, nil
	}

	resultIDs := make([]string, len(matchedIDs))
	refs := make([]entryRef, len(matchedIDs))
	for i, m := range matchedIDs {
		resultIDs[i] = m.EntryID
		refs[i] = entryRef{ID: m.EntryID, Type: input.EntryType}
	}

	entries, err := r.hydrateInOrder(refs)
	if err != nil {
		return nil, 0, fmt.Errorf("error getting entry details: %w", err)
	}

	searchTerm := input.MessageSearch
	if searchTerm == "" {
		searchTerm = input.Query
	}
	if searchTerm != "" {
		r.attachMatchedMessages(entries, resultIDs, string(input.EntryType), searchTerm)
	}

	return entries, totalCount, nil
}

func containerInboxQueries(input conversation.SearchEntriesInput, ch channelQuery, filteredIDs []string, pageSize, offset int) inboxQueries {
	et := string(input.EntryType)
	useCampaignFilter := input.CampaignID != ""
	entryTypeLiteral := "'" + string(ch.EntryType) + "'::text"

	var cteConditions []string
	var cteArgs []interface{}

	if useCampaignFilter {
		campaignFilter, departmentArgs := ch.filterForKind(input.ContainerKind,
			func(column, entryCol string) (string, []interface{}) {
				return departmentScopeClause(column, entryCol,
					input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
			})
		cteConditions = []string{campaignFilter, "cm.entry_type = ?", "cm.entry_id IS NOT NULL", "cm.deleted_at IS NULL"}
		cteArgs = append(append([]interface{}{input.CampaignID}, departmentArgs...), et)
	} else {
		cteConditions = []string{"cm.entry_id = ANY(?::uuid[])", "cm.entry_type = ?", "cm.entry_id IS NOT NULL", "cm.deleted_at IS NULL"}
		cteArgs = []interface{}{pq.Array(filteredIDs), et}
	}

	if len(input.StageIDs) > 0 && input.StageWorkspaceID != "" && !useCampaignFilter {
		if len(input.StageIDs) == 1 && input.StageIDs[0] == "__unstaged__" {
			cteConditions = append(cteConditions, "cm.entry_id NOT IN (SELECT entry_id FROM entry_stages WHERE workspace_id = ? AND deleted_at IS NULL)")
			cteArgs = append(cteArgs, input.StageWorkspaceID)
		} else {
			cteConditions = append(cteConditions, "cm.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)")
			cteArgs = append(cteArgs, pq.Array(input.StageIDs), input.StageWorkspaceID)
		}
	}

	if input.Channel != "" {
		cteConditions = append(cteConditions, "cm.channel = ?")
		cteArgs = append(cteArgs, input.Channel)
	}
	if input.DateFrom != nil {
		cteConditions = append(cteConditions, "cm.created_at >= ?")
		cteArgs = append(cteArgs, *input.DateFrom)
	}
	if input.DateTo != nil {
		cteConditions = append(cteConditions, "cm.created_at <= ?")
		cteArgs = append(cteArgs, *input.DateTo)
	}

	if input.AssignedUserID != "" {
		cteConditions = append(cteConditions, "(NOT EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = cm.entry_id) OR EXISTS (SELECT 1 FROM inbox_assignments ia2 WHERE ia2.entry_id = cm.entry_id AND ia2.assigned_user_id = ?))")
		cteArgs = append(cteArgs, input.AssignedUserID)
	}

	if input.ResponsibleUnassigned {
		cteConditions = append(cteConditions, "NOT EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = cm.entry_id)")
	} else if input.ResponsibleKind != "" {
		cteConditions = append(cteConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = cm.entry_id AND iaf.assignee_kind = ?)")
		cteArgs = append(cteArgs, string(input.ResponsibleKind))
	} else if input.ResponsibleUserID != "" {
		cteConditions = append(cteConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = cm.entry_id AND iaf.assigned_user_id = ?)")
		cteArgs = append(cteArgs, input.ResponsibleUserID)
	}

	personal := personalScopeOf(input)
	marksJoin, marksArgs := personal.join("entries.entry_id", entryTypeLiteral)
	selected := "entries.entry_id, " +
		sqlexpr.WaitingSince(ch.MessageClockTable) + " AS ws_at"
	order := inboxOrderColumns{LastMessage: "lm.created_at", WaitingSince: "matching.ws_at"}
	if pinned := personal.pinnedColumn(); pinned != "" {
		selected += ", " + pinned
		order.Pinned = "matching.pinned"
	}

	baseQuery := fmt.Sprintf(`
		WITH entries AS (
			SELECT DISTINCT entry_id
			FROM conversation_messages cm
			WHERE %s
		)
		SELECT %s
		FROM entries
		%s
		%s
		%s
	`, joinConditions(cteConditions), selected, ch.entryJoinOn("entries.entry_id"), ch.ContactJoin, marksJoin)

	whereConditions, whereArgs := containerInboxConditions(input, ch, filteredIDs, useCampaignFilter)
	if condition := personal.condition(); condition != "" {
		whereConditions = append(whereConditions, condition)
	}
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + joinConditions(whereConditions)
	}

	args := append(append(append([]interface{}{}, cteArgs...), marksArgs...), whereArgs...)
	matching := baseQuery + " " + whereClause
	return inboxQueries{
		countSQL:  "SELECT COUNT(*) FROM (" + matching + ") AS matching_entries",
		countArgs: args,
		pageSQL: fmt.Sprintf(`
			SELECT matching.entry_id FROM (%s) AS matching
			JOIN LATERAL (
				SELECT cm_l.created_at
				FROM conversation_messages cm_l
				WHERE cm_l.entry_id = matching.entry_id AND cm_l.entry_type = ? AND cm_l.deleted_at IS NULL
				ORDER BY cm_l.created_at DESC LIMIT 1
			) AS lm ON true
			%s
			LIMIT ? OFFSET ?
		`, matching, inboxOrderBySQL(input.SortOrder, order)),
		pageArgs: append(append([]interface{}{}, args...), et, pageSize, offset),
	}
}

func containerInboxConditions(input conversation.SearchEntriesInput, ch channelQuery, filteredIDs []string, useCampaignFilter bool) ([]string, []interface{}) {
	et := string(input.EntryType)
	whereConditions := []string{}
	whereArgs := []interface{}{}

	if statusCol := ch.StatusColumn; statusCol != "" {
		if input.ConversationStatus == "" {
			whereConditions = append(whereConditions, statusCol+" IS DISTINCT FROM 'finished'")
		} else {
			whereConditions = append(whereConditions, statusCol+" = ?")
			whereArgs = append(whereArgs, string(input.ConversationStatus))
		}
	}

	if useCampaignFilter && len(input.StageIDs) > 0 && input.StageWorkspaceID != "" {
		if len(input.StageIDs) == 1 && input.StageIDs[0] == "__unstaged__" {
			whereConditions = append(whereConditions, "NOT EXISTS (SELECT 1 FROM entry_stages et WHERE et.entry_id = entries.entry_id AND et.workspace_id = ? AND et.deleted_at IS NULL)")
			whereArgs = append(whereArgs, input.StageWorkspaceID)
		} else {
			whereConditions = append(whereConditions, "entries.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)")
			whereArgs = append(whereArgs, pq.Array(input.StageIDs), input.StageWorkspaceID)
		}
	}

	if input.LabelID != "" {
		condition, args := labelCondition(input.LabelID, "entries.entry_id", "'"+string(ch.EntryType)+"'::text")
		whereConditions = append(whereConditions, condition)
		whereArgs = append(whereArgs, args...)
	}

	if input.Query != "" {
		queryPattern := "%%" + input.Query + "%%"
		if input.MessageSearch == "" {
			if useCampaignFilter {
				whereConditions = append(whereConditions, `(LOWER(l.name) LIKE LOWER(?) OR l.number LIKE ? OR entries.entry_id IN (
					SELECT DISTINCT cm_q.entry_id
					FROM conversation_messages cm_q
					WHERE cm_q.entry_type = ?
					  AND `+sqlexpr.MessageTextMatches("cm_q")+`
				))`)
				whereArgs = append(whereArgs, queryPattern, queryPattern, et, queryPattern)
			} else {
				whereConditions = append(whereConditions, `(LOWER(l.name) LIKE LOWER(?) OR l.number LIKE ? OR entries.entry_id IN (
					SELECT DISTINCT cm_q.entry_id
					FROM conversation_messages cm_q
					WHERE cm_q.entry_id = ANY(?::uuid[]) AND cm_q.entry_type = ?
					  AND `+sqlexpr.MessageTextMatches("cm_q")+`
				))`)
				whereArgs = append(whereArgs, queryPattern, queryPattern, pq.Array(filteredIDs), et, queryPattern)
			}
		} else {
			whereConditions = append(whereConditions, "(LOWER(l.name) LIKE LOWER(?) OR l.number LIKE ?)")
			whereArgs = append(whereArgs, queryPattern, queryPattern)
		}
	}

	if input.MessageSearch != "" {
		if useCampaignFilter {
			whereConditions = append(whereConditions, `entries.entry_id IN (
				SELECT DISTINCT cm2.entry_id
				FROM conversation_messages cm2
				WHERE cm2.entry_type = ?
				  AND `+sqlexpr.MessageTextMatches("cm2")+`
			)`)
			whereArgs = append(whereArgs, et, "%%"+input.MessageSearch+"%%")
		} else {
			whereConditions = append(whereConditions, `entries.entry_id IN (
				SELECT DISTINCT cm2.entry_id
				FROM conversation_messages cm2
				WHERE cm2.entry_id = ANY(?::uuid[]) AND cm2.entry_type = ?
				  AND `+sqlexpr.MessageTextMatches("cm2")+`
			)`)
			whereArgs = append(whereArgs, pq.Array(filteredIDs), et, "%%"+input.MessageSearch+"%%")
		}
	}

	if input.MinMessageCount != nil || input.MaxMessageCount != nil {
		countSubquery := `(SELECT COUNT(*) FROM conversation_messages cm3 WHERE cm3.entry_id = entries.entry_id AND cm3.entry_type = ? AND cm3.deleted_at IS NULL)`
		if input.MinMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s >= ?", countSubquery))
			whereArgs = append(whereArgs, et, *input.MinMessageCount)
		}
		if input.MaxMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s <= ?", countSubquery))
			whereArgs = append(whereArgs, et, *input.MaxMessageCount)
		}
	}

	if input.HasUnread != nil {
		unreadSubquery := `(SELECT COUNT(*) FROM conversation_messages cm4 WHERE cm4.entry_id = entries.entry_id AND cm4.entry_type = ? AND cm4.read = false AND ` + sqlexpr.SentByContact("cm4") + ` AND cm4.deleted_at IS NULL)`
		if *input.HasUnread {
			whereConditions = append(whereConditions, fmt.Sprintf("%s > 0", unreadSubquery))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("%s = 0", unreadSubquery))
		}
		whereArgs = append(whereArgs, et)
	}

	if input.WindowOpen != nil {
		open := ch.windowOpenOn("entries.entry_id")
		if !*input.WindowOpen {
			open = "NOT " + open
		}
		whereConditions = append(whereConditions, open)
	}

	if input.IsGroup != nil {
		whereConditions = append(whereConditions, ch.groupCondition(*input.IsGroup, "entries.entry_id"))
	}

	return whereConditions, whereArgs
}

func (r *repository) attachMatchedMessages(entries []conversation.EntryWithLastMessage, resultIDs []string, entryType, searchTerm string) {
	type matchResult struct {
		MessageID  string    `gorm:"column:message_id"`
		EntryID    string    `gorm:"column:entry_id"`
		Text       string    `gorm:"column:text"`
		Transcript string    `gorm:"column:transcript"`
		From       string    `gorm:"column:from_participant"`
		MsgType    string    `gorm:"column:message_type"`
		Channel    string    `gorm:"column:channel"`
		CreatedAt  time.Time `gorm:"column:created_at"`
		Position   int64     `gorm:"column:position"`
		MatchRank  int       `gorm:"column:match_rank"`
		TotalMatch int       `gorm:"column:total_matches"`
	}

	var matches []matchResult
	matchQuery := `
		WITH ranked AS (
			SELECT
				cm.id AS message_id,
				cm.entry_id,
				cm.text,
				cm.transcript,
				cm.from_participant,
				cm.message_type,
				cm.channel,
				cm.created_at,
				(SELECT COUNT(*) FROM conversation_messages cm2
				 WHERE cm2.entry_id = cm.entry_id AND cm2.entry_type = cm.entry_type
				   AND cm2.deleted_at IS NULL AND cm2.created_at > cm.created_at) AS position,
				ROW_NUMBER() OVER (PARTITION BY cm.entry_id ORDER BY cm.created_at DESC) AS match_rank,
				COUNT(*) OVER (PARTITION BY cm.entry_id) AS total_matches
			FROM conversation_messages cm
			WHERE cm.entry_id = ANY(?::uuid[]) AND cm.entry_type = ?
			  AND ` + sqlexpr.MessageTextMatches("cm") + `
		)
		SELECT message_id, entry_id, text, transcript, from_participant, message_type, channel, created_at,
		       position, match_rank, total_matches
		FROM ranked
		WHERE match_rank <= 3
		ORDER BY entry_id, created_at DESC
	`
	r.db.Raw(matchQuery, pq.Array(resultIDs), entryType, "%%"+searchTerm+"%%").Scan(&matches)

	matchMap := make(map[string][]conversation.MatchedMessageResult)
	totalMap := make(map[string]int)
	for _, m := range matches {
		matchMap[m.EntryID] = append(matchMap[m.EntryID], conversation.MatchedMessageResult{
			MessageID:    m.MessageID,
			EntryID:      m.EntryID,
			Text:         (&conversation.Message{Text: m.Text, Transcript: m.Transcript}).ReadableText(),
			From:         m.From,
			MsgType:      conversation.MessageType(m.MsgType),
			Channel:      conversation.MessageChannel(m.Channel),
			CreatedAt:    m.CreatedAt,
			Position:     m.Position,
			TotalMatches: m.TotalMatch,
		})
		totalMap[m.EntryID] = m.TotalMatch
	}

	for i := range entries {
		if mm, ok := matchMap[entries[i].EntryID]; ok {
			entries[i].MatchedMessages = mm
			entries[i].TotalMatches = totalMap[entries[i].EntryID]
		}
	}
}
