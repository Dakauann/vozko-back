package call_roulette

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/call_roulette"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) call_roulette.Repository {
	return &repository{db: db}
}

func (r *repository) GetState(workspaceID, sipTrunkID, departmentID string) (*call_roulette.RouletteState, error) {
	var row struct {
		ID                 string
		WorkspaceID        string
		SIPTrunkID         string
		DepartmentID       string
		LastAssignedUserID string
	}
	err := r.db.Table("call_roulette_state").
		Select("id, workspace_id, sip_trunk_id, department_id, last_assigned_user_id").
		Where("workspace_id = ? AND sip_trunk_id = ? AND department_id = ?", workspaceID, sipTrunkID, departmentID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &call_roulette.RouletteState{
		ID:                 row.ID,
		WorkspaceID:        row.WorkspaceID,
		SIPTrunkID:         row.SIPTrunkID,
		DepartmentID:       row.DepartmentID,
		LastAssignedUserID: row.LastAssignedUserID,
	}, nil
}

func (r *repository) SaveState(state *call_roulette.RouletteState) error {
	if state == nil {
		return errors.New("call roulette: state is nil")
	}
	if state.ID == "" {
		state.ID = uuid.New().String()
	}
	return r.db.Exec(`
		INSERT INTO call_roulette_state (id, workspace_id, sip_trunk_id, department_id, last_assigned_user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (workspace_id, sip_trunk_id, department_id)
		DO UPDATE SET last_assigned_user_id = EXCLUDED.last_assigned_user_id, updated_at = NOW()
	`, state.ID, state.WorkspaceID, state.SIPTrunkID, state.DepartmentID, state.LastAssignedUserID).Error
}

func (r *repository) Assign(entryID string, entryType call_roulette.EntryType, workspaceID, sipTrunkID, departmentID, assignedUserID string) error {
	entryTypeStr := string(entryType)
	if entryTypeStr == "" {
		entryTypeStr = "voice"
	}

	return r.db.Exec(`
		INSERT INTO call_roulette_assignments (id, workspace_id, sip_trunk_id, entry_id, entry_type, assigned_user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (entry_id)
		DO UPDATE SET assigned_user_id = EXCLUDED.assigned_user_id, workspace_id = EXCLUDED.workspace_id,
		              sip_trunk_id = EXCLUDED.sip_trunk_id, updated_at = NOW()
	`, uuid.New().String(), workspaceID, sipTrunkID, entryID, entryTypeStr, assignedUserID).Error
}

func (r *repository) FindByEntry(workspaceID, entryID string, entryType call_roulette.EntryType) (*call_roulette.CallAssignment, error) {
	var row struct {
		ID             string
		WorkspaceID    string
		SIPTrunkID     string
		EntryID        string
		EntryType      string
		AssignedUserID string
		CreatedAt      time.Time
		UpdatedAt      time.Time
	}
	err := r.db.Table("call_roulette_assignments").
		Select("id, workspace_id, sip_trunk_id, entry_id, entry_type, assigned_user_id, created_at, updated_at").
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, string(entryType)).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &call_roulette.CallAssignment{
		ID:             row.ID,
		WorkspaceID:    row.WorkspaceID,
		SIPTrunkID:     row.SIPTrunkID,
		EntryID:        row.EntryID,
		EntryType:      call_roulette.EntryType(row.EntryType),
		AssignedUserID: row.AssignedUserID,
		CreatedAt:      row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (r *repository) Unassign(workspaceID, entryID string, entryType call_roulette.EntryType) error {
	return r.db.Table("call_roulette_assignments").
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, string(entryType)).
		Delete(nil).Error
}

func (r *repository) ListByUser(workspaceID, userID string) ([]*call_roulette.CallAssignment, error) {
	type row struct {
		ID             string
		WorkspaceID    string
		SIPTrunkID     string
		EntryID        string
		EntryType      string
		AssignedUserID string
		CreatedAt      time.Time
		UpdatedAt      time.Time
	}
	var rows []row
	err := r.db.Table("call_roulette_assignments").
		Select("id, workspace_id, sip_trunk_id, entry_id, entry_type, assigned_user_id, created_at, updated_at").
		Where("workspace_id = ? AND assigned_user_id = ?", workspaceID, userID).
		Order("created_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]*call_roulette.CallAssignment, len(rows))
	for i, row := range rows {
		result[i] = &call_roulette.CallAssignment{
			ID:             row.ID,
			WorkspaceID:    row.WorkspaceID,
			SIPTrunkID:     row.SIPTrunkID,
			EntryID:        row.EntryID,
			EntryType:      call_roulette.EntryType(strings.TrimSpace(row.EntryType)),
			AssignedUserID: row.AssignedUserID,
			CreatedAt:      row.CreatedAt.Format(time.RFC3339),
			UpdatedAt:      row.UpdatedAt.Format(time.RFC3339),
		}
	}
	return result, nil
}
