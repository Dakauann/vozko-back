package inbox_assignment_repository

import (
	"gorm.io/gorm"

	"vozko/domain/actor"
	ia "vozko/domain/inbox_assignment"
	attendance_repository "vozko/infra/repositories/attendance"
)

var openConversationsSQL = `SELECT ia.assigned_user_id::text AS user_id, COUNT(*) AS open_count ` +
	`FROM inbox_assignments ia JOIN (` + attendance_repository.OpenEntryUnion() + `) e ON e.entry_id = ia.entry_id AND e.entry_type = ia.entry_type ` +
	`WHERE ia.workspace_id = ? AND ia.assignee_kind = ? AND ia.assigned_user_id IN ? GROUP BY ia.assigned_user_id`

type workloadRepository struct {
	db *gorm.DB
}

func NewWorkloadRepository(db *gorm.DB) ia.WorkloadReader {
	return &workloadRepository{db: db}
}

func (r *workloadRepository) OpenConversations(workspaceID string, userIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		UserID    string
		OpenCount int
	}
	if err := r.db.Raw(openConversationsSQL, workspaceID, string(actor.KindHuman), userIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.UserID] = row.OpenCount
	}
	return out, nil
}
