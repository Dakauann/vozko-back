package branch_repository

import (
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/branch"
	"vozko/domain/workspace"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) branch.Repository {
	return &repository{db: db}
}

func (r *repository) Create(b *branch.Branch) error {
	return r.db.Create(toSchema(b)).Error
}

func (r *repository) Update(b *branch.Branch) error {
	return r.db.Save(toSchema(b)).Error
}

func (r *repository) Delete(id string) error {
	return r.db.Delete(&schema.Branch{}, "id = ?", id).Error
}

func (r *repository) FindByID(id string) (*branch.Branch, error) {
	var s schema.Branch
	if err := r.db.Where("id = ?", id).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, branch.ErrBranchNotFound
		}
		return nil, err
	}
	return toDomain(&s), nil
}

func (r *repository) FindBySIPUser(workspaceID, sipUser string) (*branch.Branch, error) {
	var s schema.Branch
	if err := r.db.Where("workspace_id = ? AND sip_user = ?", workspaceID, sipUser).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, branch.ErrBranchNotFound
		}
		return nil, err
	}
	return toDomain(&s), nil
}

func (r *repository) FindByWorkspace(workspaceID string, page, pageSize int) ([]*branch.Branch, int64, error) {
	var rows []schema.Branch
	var total int64

	query := r.db.Model(&schema.Branch{}).Where("workspace_id = ?", workspaceID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page > 0 && pageSize > 0 {
		query = query.Offset((page - 1) * pageSize).Limit(pageSize)
	}

	if err := query.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*branch.Branch, len(rows))
	for i := range rows {
		result[i] = toDomain(&rows[i])
	}
	return result, total, nil
}

func (r *repository) FindByGlobalSIPUser(sipUser string) (*branch.Branch, error) {
	var rows []schema.Branch
	// Fetch up to 2 so an accidental duplicate is detected rather than silently
	// resolving to an arbitrary branch (fail safe: the registrar must not
	// authenticate the wrong tenant).
	if err := r.db.Where("sip_user = ?", sipUser).Limit(2).Find(&rows).Error; err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		return nil, branch.ErrBranchNotFound
	case 1:
		return toDomain(&rows[0]), nil
	default:
		return nil, branch.ErrBranchSIPUserTaken
	}
}

func (r *repository) FindByUser(workspaceID, userID string) ([]*branch.Branch, error) {
	var rows []schema.Branch
	if err := r.db.Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*branch.Branch, len(rows))
	for i := range rows {
		result[i] = toDomain(&rows[i])
	}
	return result, nil
}

func (r *repository) CountByWorkspace(workspaceID string) (int64, error) {
	var total int64
	if err := r.db.Model(&schema.Branch{}).
		Where("workspace_id = ?", workspaceID).
		Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *repository) UpdateRegistrationStatus(id string, status branch.RegistrationStatus) error {
	updates := map[string]any{"registration_status": string(status)}
	// Stamp the moment the phone last came online so the dashboard can show it.
	if status == branch.RegistrationStatusRegistered {
		updates["last_registered_at"] = time.Now()
	}
	return r.db.Model(&schema.Branch{}).Where("id = ?", id).Updates(updates).Error
}

func (r *repository) ResetLiveRegistrations() (int64, error) {
	// After a restart the in-process AOR bindings are gone, so any row still marked
	// `registered` is stale. Flip it to `expired` (it will return to `registered` on
	// the phone's next refresh REGISTER) so the dashboard never claims a branch is
	// online that the routing layer cannot reach.
	res := r.db.Model(&schema.Branch{}).
		Where("registration_status = ?", string(branch.RegistrationStatusRegistered)).
		Update("registration_status", string(branch.RegistrationStatusExpired))
	return res.RowsAffected, res.Error
}

func toSchema(b *branch.Branch) *schema.Branch {
	return &schema.Branch{
		ID:                 b.ID,
		WorkspaceID:        b.WorkspaceID,
		MemberID:           b.MemberID,
		UserID:             b.UserID,
		SIPUser:            b.SIPUser,
		DisplayName:        b.DisplayName,
		Realm:              b.Realm,
		SecretHA1:          piigorm.NewEncrypted(b.SecretHA1),
		Codecs:             marshalCodecs(b.Codecs),
		MaxContacts:        b.MaxContacts,
		DND:                b.DND,
		Enabled:            b.Enabled,
		RegistrationStatus: string(b.RegistrationStatus),
		LastRegisteredAt:   b.LastRegisteredAt,
		CreatedAt:          b.CreatedAt,
		UpdatedAt:          b.UpdatedAt,
	}
}

func toDomain(s *schema.Branch) *branch.Branch {
	return &branch.Branch{
		ID:                 s.ID,
		WorkspaceID:        s.WorkspaceID,
		MemberID:           s.MemberID,
		UserID:             s.UserID,
		SIPUser:            s.SIPUser,
		DisplayName:        s.DisplayName,
		Realm:              s.Realm,
		SecretHA1:          s.SecretHA1.String(),
		Codecs:             unmarshalCodecs(s.Codecs),
		MaxContacts:        s.MaxContacts,
		DND:                s.DND,
		Enabled:            s.Enabled,
		RegistrationStatus: branch.RegistrationStatus(s.RegistrationStatus),
		LastRegisteredAt:   s.LastRegisteredAt,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}
}

func marshalCodecs(v []branch.CodecID) datatypes.JSON {
	if len(v) == 0 {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return datatypes.JSON(b)
}

func unmarshalCodecs(b datatypes.JSON) []branch.CodecID {
	if len(b) == 0 {
		return nil
	}
	var out []branch.CodecID
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// memberDirectory adapts the workspace repository to branch.MemberDirectory so
// the branch use cases can resolve a (workspace, user) pair to a membership id
// without depending on the whole workspace domain surface.
type memberDirectory struct {
	ws workspace.Repository
}

// NewMemberDirectory wraps the workspace repository as a branch.MemberDirectory.
func NewMemberDirectory(ws workspace.Repository) branch.MemberDirectory {
	return &memberDirectory{ws: ws}
}

func (d *memberDirectory) ResolveMember(workspaceID, userID string) (string, error) {
	m, err := d.ws.GetMember(workspaceID, userID)
	if err != nil || m == nil {
		return "", branch.ErrBranchMemberNotFound
	}
	return m.ID, nil
}
