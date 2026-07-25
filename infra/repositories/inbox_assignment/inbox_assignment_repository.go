package inbox_assignment_repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	ia "vozko/domain/inbox_assignment"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) ia.Repository {
	return &repository{db: db}
}

func (r *repository) FindByEntry(workspaceID, entryID, entryType string) (*ia.InboxAssignment, error) {
	var rec schema.InboxAssignment
	err := r.db.Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType).First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(&rec), nil
}

func (r *repository) FindByEntries(workspaceID string, entryIDs []string) ([]*ia.InboxAssignment, error) {
	if len(entryIDs) == 0 {
		return nil, nil
	}
	var recs []schema.InboxAssignment
	err := r.db.Where("workspace_id = ? AND entry_id IN ?", workspaceID, entryIDs).Find(&recs).Error
	if err != nil {
		return nil, err
	}
	result := make([]*ia.InboxAssignment, len(recs))
	for i := range recs {
		result[i] = toDomain(&recs[i])
	}
	return result, nil
}

func (r *repository) FindByEntryAndUser(workspaceID, entryID, entryType, userID string) (*ia.InboxAssignment, error) {
	var rec schema.InboxAssignment
	err := r.db.Where("workspace_id = ? AND entry_id = ? AND entry_type = ? AND assigned_user_id = ?", workspaceID, entryID, entryType, userID).First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(&rec), nil
}

func (r *repository) Assign(assignment *ia.InboxAssignment) error {
	rec := toSchema(assignment)

	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "entry_id"}, {Name: "entry_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"assigned_user_id", "workspace_id", "updated_at"}),
	}).Create(&rec).Error
}

func (r *repository) Unassign(workspaceID, entryID, entryType string) error {
	return r.db.Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType).
		Delete(&schema.InboxAssignment{}).Error
}

func (r *repository) ListByUser(workspaceID, userID, entryType string) ([]string, error) {
	q := r.db.Model(&schema.InboxAssignment{}).
		Select("entry_id").
		Where("workspace_id = ? AND assigned_user_id = ?", workspaceID, userID)
	if entryType != "" {
		q = q.Where("entry_type = ?", entryType)
	}
	var ids []string
	if err := q.Find(&ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *repository) IsAssignedToUser(workspaceID, entryID, entryType, userID string) (bool, error) {
	var count int64
	err := r.db.Model(&schema.InboxAssignment{}).
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ? AND assigned_user_id = ?", workspaceID, entryID, entryType, userID).
		Count(&count).Error
	return count > 0, err
}

func (r *repository) GetRoundRobinState(workspaceID, businessPhoneID, departmentID string) (*ia.RoundRobinState, error) {
	var rec schema.InboxRoundRobinState
	err := r.db.Where("workspace_id = ? AND business_phone_id = ? AND department_id = ?", workspaceID, businessPhoneID, departmentID).First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ia.RoundRobinState{
		ID:                 rec.ID,
		WorkspaceID:        rec.WorkspaceID,
		BusinessPhoneID:    rec.BusinessPhoneID,
		DepartmentID:       rec.DepartmentID,
		LastAssignedUserID: rec.LastAssignedUserID,
		UpdatedAt:          rec.UpdatedAt,
	}, nil
}

func (r *repository) SaveRoundRobinState(state *ia.RoundRobinState) error {
	rec := schema.InboxRoundRobinState{
		ID:                 state.ID,
		WorkspaceID:        state.WorkspaceID,
		BusinessPhoneID:    state.BusinessPhoneID,
		DepartmentID:       state.DepartmentID,
		LastAssignedUserID: state.LastAssignedUserID,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "business_phone_id"}, {Name: "department_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_assigned_user_id", "updated_at"}),
	}).Create(&rec).Error
}

func toDomain(rec *schema.InboxAssignment) *ia.InboxAssignment {
	businessPhoneID := ""
	if rec.BusinessPhoneID != nil {
		businessPhoneID = *rec.BusinessPhoneID
	}
	return &ia.InboxAssignment{
		ID:              rec.ID,
		WorkspaceID:     rec.WorkspaceID,
		BusinessPhoneID: businessPhoneID,
		EntryID:         rec.EntryID,
		EntryType:       rec.EntryType,
		AssignedUserID:  rec.AssignedUserID,
		CreatedAt:       rec.CreatedAt,
		UpdatedAt:       rec.UpdatedAt,
	}
}

func toSchema(a *ia.InboxAssignment) *schema.InboxAssignment {
	rec := &schema.InboxAssignment{
		ID:             a.ID,
		WorkspaceID:    a.WorkspaceID,
		EntryID:        a.EntryID,
		EntryType:      a.EntryType,
		AssignedUserID: a.AssignedUserID,
	}
	// Empty → NULL: an empty string is not a valid uuid for business_phone_id.
	if a.BusinessPhoneID != "" {
		bp := a.BusinessPhoneID
		rec.BusinessPhoneID = &bp
	}
	return rec
}
