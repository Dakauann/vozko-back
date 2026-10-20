package conversation_repository

import (
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
)

type inboxOrderColumns struct {
	LastMessage  string
	WaitingSince string
	Pinned       string
}

func inboxOrderBySQL(sortOrder string, cols inboxOrderColumns) string {
	keys := make([]string, 0, 4)
	if cols.Pinned != "" {
		keys = append(keys, cols.Pinned+" DESC")
	}
	switch sortOrder {
	case conversation.SortOrderWaiting:
		keys = append(keys, cols.WaitingSince+" ASC NULLS LAST", cols.LastMessage+" DESC")
	case "asc":
		keys = append(keys, cols.LastMessage+" ASC")
	default:
		keys = append(keys, cols.LastMessage+" DESC")
	}
	return "ORDER BY " + strings.Join(keys, ", ")
}

type personalScope struct {
	view   personalinbox.View
	userID string
}

func personalScopeOf(input conversation.SearchEntriesInput) personalScope {
	return personalScope{view: input.Personal, userID: strings.TrimSpace(input.PersonalUserID)}
}

func (s personalScope) active() bool {
	return s.view.Applies() && s.userID != ""
}

func (s personalScope) join(entryIDColumn, entryTypeExpr string) (string, []interface{}) {
	if !s.active() {
		return "", nil
	}
	return fmt.Sprintf(" LEFT JOIN conversation_personal_marks pm ON pm.user_id = ? AND pm.entry_type = %s AND pm.entry_id = %s",
		entryTypeExpr, entryIDColumn), []interface{}{s.userID}
}

func (s personalScope) pinnedColumn() string {
	if !s.active() {
		return ""
	}
	return "(pm.pinned_at IS NOT NULL) AS pinned"
}

func (s personalScope) condition() string {
	if s.view != personalinbox.ViewPinned {
		return ""
	}
	if !s.active() {
		return "1 = 0"
	}
	return "pm.pinned_at IS NOT NULL"
}

func labelCondition(labelID, entryIDColumn, entryTypeExpr string) (string, []interface{}) {
	return fmt.Sprintf("EXISTS (SELECT 1 FROM entry_labels el WHERE el.label_id = ? AND el.entry_id = %s AND el.entry_type = %s AND el.deleted_at IS NULL)",
		entryIDColumn, entryTypeExpr), []interface{}{labelID}
}
