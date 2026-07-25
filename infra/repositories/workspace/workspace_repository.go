package workspace_repository

import (
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/workspace"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) workspace.Repository {
	return &repository{db: db}
}

func (r *repository) WithTx(tx interface{}) workspace.Repository {
	return &repository{db: tx.(*gorm.DB)}
}

func (r *repository) CreateWorkspace(ws *workspace.Workspace) error {
	dbWs := schema.Workspace{
		ID:        ws.ID,
		OwnerID:   ws.OwnerID,
		Name:      ws.Name,
		IsDefault: ws.IsDefault,
	}
	if err := r.db.Create(&dbWs).Error; err != nil {
		return err
	}
	ws.CreatedAt = dbWs.CreatedAt
	ws.UpdatedAt = dbWs.UpdatedAt
	return nil
}

func (r *repository) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	var dbWs schema.Workspace
	if err := r.db.Where("id = ?", id).First(&dbWs).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace.ErrWorkspaceNotFound
		}
		return nil, err
	}
	return mapWorkspaceToDomain(&dbWs), nil
}

func (r *repository) GetDefaultWorkspace(ownerID string) (*workspace.Workspace, error) {
	var dbWs schema.Workspace
	if err := r.db.Where("owner_id = ? AND is_default = ?", ownerID, true).First(&dbWs).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapWorkspaceToDomain(&dbWs), nil
}

func (r *repository) ListWorkspacesByUser(userID, search, memberEmail string) ([]*workspace.Workspace, error) {
	query := r.db.Model(&schema.Workspace{}).
		Select("workspaces.*, wm.role AS current_user_role").
		Joins("JOIN workspace_members wm ON wm.workspace_id = workspaces.id AND wm.user_id = ?", userID)

	if memberEmail != "" {
		like := "%" + memberEmail + "%"
		query = query.Where(
			"workspaces.id IN (SELECT workspace_id FROM workspace_members JOIN users ON users.id = workspace_members.user_id WHERE users.email ILIKE ?)",
			like,
		)
	}

	if search != "" {
		like := "%" + search + "%"
		query = query.Where("workspaces.name ILIKE ?", like)
	}

	type wsWithRole struct {
		schema.Workspace
		CurrentUserRole string `gorm:"column:current_user_role"`
	}
	var rows []wsWithRole
	if err := query.Order("workspaces.created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.Workspace, 0, len(rows))
	for i := range rows {
		ws := mapWorkspaceToDomain(&rows[i].Workspace)
		ws.CurrentUserRole = workspace.Role(rows[i].CurrentUserRole)
		result = append(result, ws)
	}
	return result, nil
}

func (r *repository) ListAllWorkspaceIDs() ([]string, error) {
	var ids []string
	err := r.db.Model(&schema.Workspace{}).Pluck("id", &ids).Error
	return ids, err
}

func (r *repository) ListAllWorkspaces(search, memberEmail string, page, pageSize int) ([]*workspace.Workspace, int64, error) {
	query := r.db.Model(&schema.Workspace{})

	if memberEmail != "" {
		like := "%" + memberEmail + "%"
		query = query.Where(
			"workspaces.id IN (SELECT workspace_id FROM workspace_members JOIN users ON users.id = workspace_members.user_id WHERE users.email ILIKE ?)",
			like,
		)
	}

	if search != "" {
		like := "%" + search + "%"
		query = query.Where("name ILIKE ? OR id::text ILIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	var dbWorkspaces []schema.Workspace
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&dbWorkspaces).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*workspace.Workspace, 0, len(dbWorkspaces))
	for i := range dbWorkspaces {
		result = append(result, mapWorkspaceToDomain(&dbWorkspaces[i]))
	}
	return result, total, nil
}

func (r *repository) CountMembersByWorkspaceIDs(workspaceIDs []string) (map[string]int, error) {
	if len(workspaceIDs) == 0 {
		return make(map[string]int), nil
	}

	type countRow struct {
		WorkspaceID string
		Count       int
	}
	var rows []countRow
	err := r.db.Table("workspace_members").
		Select("workspace_id, COUNT(*) as count").
		Where("workspace_id IN ?", workspaceIDs).
		Group("workspace_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]int, len(rows))
	for _, row := range rows {
		result[row.WorkspaceID] = row.Count
	}
	return result, nil
}

func (r *repository) ListMembersPaginated(workspaceID string, page, pageSize int) ([]*workspace.Member, int64, error) {
	query := r.db.Where("workspace_id = ?", workspaceID)

	var total int64
	if err := query.Model(&schema.WorkspaceMember{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	var dbMembers []schema.WorkspaceMember
	if err := query.Preload("User").Preload("CustomRole").Order("created_at ASC").Offset(offset).Limit(pageSize).Find(&dbMembers).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*workspace.Member, 0, len(dbMembers))
	for i := range dbMembers {
		result = append(result, mapMemberToDomain(&dbMembers[i]))
	}
	return result, total, nil
}

func (r *repository) UpdateWorkspace(ws *workspace.Workspace) error {
	return r.db.Model(&schema.Workspace{}).Where("id = ?", ws.ID).Updates(map[string]interface{}{
		"name": ws.Name,
	}).Error
}

// TransferOwnership reassigns workspaces.owner_id to newOwnerID. An empty
// newOwnerID orphans the workspace by setting owner_id to NULL (used when a
// deleting user solely owns it and there is no admin to inherit).
func (r *repository) TransferOwnership(workspaceID, newOwnerID string) error {
	if newOwnerID == "" {
		return r.db.Model(&schema.Workspace{}).
			Where("id = ?", workspaceID).
			Update("owner_id", nil).Error
	}
	updates := map[string]interface{}{"owner_id": newOwnerID}
	// The (owner_id, is_default) unique index forbids a user owning two default
	// workspaces. If the new owner already has a default, demote the transferred
	// workspace to non-default so the update cannot collide.
	var existingDefault int64
	if err := r.db.Model(&schema.Workspace{}).
		Where("owner_id = ? AND is_default = ?", newOwnerID, true).
		Count(&existingDefault).Error; err != nil {
		return err
	}
	if existingDefault > 0 {
		updates["is_default"] = false
	}
	return r.db.Model(&schema.Workspace{}).Where("id = ?", workspaceID).Updates(updates).Error
}

func (r *repository) DetachUserAuthoredRefs(userID string) error {
	if err := r.db.Where("inviter_id = ?", userID).Delete(&schema.WorkspaceInvite{}).Error; err != nil {
		return err
	}
	if err := r.db.Where("granted_by = ?", userID).Delete(&schema.WorkspacePhoneAccess{}).Error; err != nil {
		return err
	}
	if err := r.db.Where("granted_by = ?", userID).Delete(&schema.WorkspaceTemplateAccess{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *repository) AddMember(member *workspace.Member) error {
	dbMember := schema.WorkspaceMember{
		ID:          member.ID,
		WorkspaceID: member.WorkspaceID,
		UserID:      member.UserID,
		Role:        string(member.Role),
		RoleID:      nilIfEmpty(member.RoleID),
	}
	if err := r.db.Create(&dbMember).Error; err != nil {
		return err
	}
	member.CreatedAt = dbMember.CreatedAt
	member.UpdatedAt = dbMember.UpdatedAt
	return nil
}

func (r *repository) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	var dbMember schema.WorkspaceMember
	if err := r.db.Preload("User").Preload("CustomRole").Where("workspace_id = ? AND user_id = ?", workspaceID, userID).First(&dbMember).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapMemberToDomain(&dbMember), nil
}

func (r *repository) GetMemberByID(memberID string) (*workspace.Member, error) {
	var dbMember schema.WorkspaceMember
	if err := r.db.Preload("User").Preload("CustomRole").Where("id = ?", memberID).First(&dbMember).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace.ErrMemberNotFound
		}
		return nil, err
	}
	return mapMemberToDomain(&dbMember), nil
}

func (r *repository) ListMembers(workspaceID string) ([]*workspace.Member, error) {
	var dbMembers []schema.WorkspaceMember
	if err := r.db.Preload("User").Preload("CustomRole").Where("workspace_id = ?", workspaceID).Find(&dbMembers).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.Member, len(dbMembers))
	for i := range dbMembers {
		result[i] = mapMemberToDomain(&dbMembers[i])
	}
	return result, nil
}

// ListAssignableMembers returns members visible to the caller, filtered by a
// pre-resolved scope (it makes no policy decisions). When restrict is false all
// workspace members are returned; when restrict is true only members in
// departmentIDs (plus selfUserID) are returned, and an empty departmentIDs
// yields just the caller. Results are searched by username/email and paginated.
func (r *repository) ListAssignableMembers(workspaceID, search string, restrict bool, departmentIDs []string, includeAdmins bool, selfUserID string, page, pageSize int) ([]*workspace.Member, int64, error) {
	build := func() *gorm.DB {
		q := r.db.Model(&schema.WorkspaceMember{}).
			Joins("JOIN users u ON u.id = workspace_members.user_id").
			Where("workspace_members.workspace_id = ?", workspaceID)

		if search != "" {
			like := "%" + strings.ToLower(search) + "%"
			q = q.Where("LOWER(u.username) LIKE ? OR LOWER(u.email) LIKE ?", like, like)
		}

		if restrict {
			// Build an OR of the visible sets: the caller's department members,
			// optionally owners/admins (roulette), and always the caller.
			clauses := make([]string, 0, 3)
			args := make([]interface{}, 0, 3)
			if len(departmentIDs) > 0 {
				memberSub := r.db.Table("workspace_department_members").
					Select("member_id").
					Where("department_id IN ?", departmentIDs)
				clauses = append(clauses, "workspace_members.id IN (?)")
				args = append(args, memberSub)
			}
			if includeAdmins {
				clauses = append(clauses, "workspace_members.role IN ?")
				args = append(args, []string{string(workspace.RoleOwner), string(workspace.RoleAdmin)})
			}
			clauses = append(clauses, "workspace_members.user_id = ?")
			args = append(args, selfUserID)
			q = q.Where(strings.Join(clauses, " OR "), args...)
		}
		return q
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize

	var dbMembers []schema.WorkspaceMember
	if err := build().
		Select("workspace_members.*").
		Preload("User").Preload("CustomRole").
		Order("u.username ASC").
		Offset(offset).Limit(pageSize).
		Find(&dbMembers).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*workspace.Member, 0, len(dbMembers))
	for i := range dbMembers {
		result = append(result, mapMemberToDomain(&dbMembers[i]))
	}
	return result, total, nil
}

func (r *repository) ListMemberDepartments(workspaceID string, memberIDs []string, restrictToDeptIDs []string) (map[string][]workspace.DepartmentRef, error) {
	result := make(map[string][]workspace.DepartmentRef)
	if len(memberIDs) == 0 {
		return result, nil
	}

	q := r.db.Table("workspace_department_members dm").
		Select("dm.member_id AS member_id, d.id AS dept_id, d.name AS dept_name").
		Joins("JOIN workspace_departments d ON d.id = dm.department_id AND d.deleted_at IS NULL").
		Where("d.workspace_id = ? AND dm.member_id IN ?", workspaceID, memberIDs)
	if len(restrictToDeptIDs) > 0 {
		q = q.Where("d.id IN ?", restrictToDeptIDs)
	}

	var rows []struct {
		MemberID string `gorm:"column:member_id"`
		DeptID   string `gorm:"column:dept_id"`
		DeptName string `gorm:"column:dept_name"`
	}
	if err := q.Order("d.name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.MemberID] = append(result[row.MemberID], workspace.DepartmentRef{ID: row.DeptID, Name: row.DeptName})
	}
	return result, nil
}

func (r *repository) UpdateMemberRole(memberID string, role workspace.Role) error {
	return r.db.Model(&schema.WorkspaceMember{}).Where("id = ?", memberID).Updates(map[string]interface{}{
		"role":    string(role),
		"role_id": nil,
	}).Error
}

func (r *repository) UpdateMemberRingChannels(memberID string, channels []workspace.RingChannel) error {
	return r.db.Model(&schema.WorkspaceMember{}).Where("id = ?", memberID).
		Update("ring_channels", serializeRingChannels(channels)).Error
}

// parseRingChannels turns the stored comma-separated column into the domain set,
// dropping unknown tokens so a bad/legacy value can never inject an invalid channel.
func parseRingChannels(s string) []workspace.RingChannel {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]workspace.RingChannel, 0, len(parts))
	for _, p := range parts {
		c := workspace.RingChannel(strings.TrimSpace(p))
		if workspace.IsValidRingChannel(c) {
			out = append(out, c)
		}
	}
	return out
}

// serializeRingChannels renders the domain set to the stored comma-separated form,
// keeping only valid channels.
func serializeRingChannels(channels []workspace.RingChannel) string {
	parts := make([]string, 0, len(channels))
	for _, c := range channels {
		if workspace.IsValidRingChannel(c) {
			parts = append(parts, string(c))
		}
	}
	return strings.Join(parts, ",")
}

func (r *repository) UpdateMemberRoleID(memberID string, roleID string) error {
	return r.db.Model(&schema.WorkspaceMember{}).Where("id = ?", memberID).Updates(map[string]interface{}{
		"role":    "",
		"role_id": nilIfEmpty(roleID),
	}).Error
}

func (r *repository) RemoveMember(memberID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Clear the member's permission grants.
		if err := tx.Where("member_id = ?", memberID).Delete(&schema.WorkspaceMemberPermission{}).Error; err != nil {
			return err
		}
		// Unlink the member from every department they belong to. These rows carry a
		// RESTRICT foreign key to the member, so leaving them would block the member
		// deletion below (this is what forced users to unlink departments by hand).
		if err := tx.Where("member_id = ?", memberID).Delete(&schema.WorkspaceDepartmentMember{}).Error; err != nil {
			return err
		}
		// Drop any resource assignments held by the member (same RESTRICT concern).
		if err := tx.Where("member_id = ?", memberID).Delete(&schema.WorkspaceResourceAssignment{}).Error; err != nil {
			return err
		}
		return tx.Delete(&schema.WorkspaceMember{}, "id = ?", memberID).Error
	})
}

func (r *repository) AddPermission(perm *workspace.Permission) error {
	dbPerm := schema.WorkspaceMemberPermission{
		ID:       perm.ID,
		MemberID: perm.MemberID,
		Resource: string(perm.Resource),
		Action:   string(perm.Action),
	}
	return r.db.Create(&dbPerm).Error
}

func (r *repository) RemovePermission(memberID string, resource workspace.Resource, action workspace.Action) error {
	result := r.db.Where("member_id = ? AND resource = ? AND action = ?", memberID, string(resource), string(action)).
		Delete(&schema.WorkspaceMemberPermission{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace.ErrPermissionNotFound
	}
	return nil
}

func (r *repository) GetPermissions(memberID string) ([]*workspace.Permission, error) {
	var dbPerms []schema.WorkspaceMemberPermission
	if err := r.db.Where("member_id = ?", memberID).Find(&dbPerms).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.Permission, len(dbPerms))
	for i := range dbPerms {
		result[i] = mapPermissionToDomain(&dbPerms[i])
	}
	return result, nil
}

func (r *repository) HasPermission(memberID string, resource workspace.Resource, action workspace.Action) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.WorkspaceMemberPermission{}).
		Where("member_id = ? AND resource = ? AND action = ?", memberID, string(resource), string(action)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) SetPermissions(memberID string, permissions []*workspace.Permission) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("member_id = ?", memberID).Delete(&schema.WorkspaceMemberPermission{}).Error; err != nil {
			return err
		}
		for _, p := range permissions {
			dbPerm := schema.WorkspaceMemberPermission{
				ID:       p.ID,
				MemberID: p.MemberID,
				Resource: string(p.Resource),
				Action:   string(p.Action),
			}
			if err := tx.Create(&dbPerm).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *repository) CreateInvite(invite *workspace.Invite) error {
	var permissionsJSON string
	if len(invite.Permissions) > 0 {
		b, err := json.Marshal(invite.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(b)
	}
	var departmentIDsJSON string
	if len(invite.DepartmentIDs) > 0 {
		b, err := json.Marshal(invite.DepartmentIDs)
		if err != nil {
			return err
		}
		departmentIDsJSON = string(b)
	}
	dbInvite := schema.WorkspaceInvite{
		ID:            invite.ID,
		WorkspaceID:   invite.WorkspaceID,
		InviterID:     invite.InviterID,
		Email:         invite.Email,
		Role:          string(invite.Role),
		RoleID:        nilIfEmpty(invite.RoleID),
		Status:        string(invite.Status),
		Token:         invite.Token,
		Permissions:   permissionsJSON,
		DepartmentIDs: departmentIDsJSON,
		ExpiresAt:     invite.ExpiresAt,
	}
	if err := r.db.Create(&dbInvite).Error; err != nil {
		return err
	}
	invite.CreatedAt = dbInvite.CreatedAt
	invite.UpdatedAt = dbInvite.UpdatedAt
	return nil
}

func (r *repository) GetInviteByID(id string) (*workspace.Invite, error) {
	var dbInvite schema.WorkspaceInvite
	if err := r.db.Preload("Workspace").Preload("Inviter").Where("id = ?", id).First(&dbInvite).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace.ErrInviteNotFound
		}
		return nil, err
	}
	return mapInviteToDomain(&dbInvite), nil
}

func (r *repository) GetInviteByToken(token string) (*workspace.Invite, error) {
	var dbInvite schema.WorkspaceInvite
	if err := r.db.Preload("Workspace").Preload("Inviter").Where("token = ?", token).First(&dbInvite).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace.ErrInviteNotFound
		}
		return nil, err
	}
	return mapInviteToDomain(&dbInvite), nil
}

func (r *repository) ListInvitesByWorkspace(workspaceID string) ([]*workspace.Invite, error) {
	var dbInvites []schema.WorkspaceInvite
	if err := r.db.Preload("Inviter").Where("workspace_id = ?", workspaceID).Order("created_at DESC").Find(&dbInvites).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.Invite, len(dbInvites))
	for i := range dbInvites {
		result[i] = mapInviteToDomain(&dbInvites[i])
	}
	return result, nil
}

func (r *repository) ListInvitesByEmail(email string) ([]*workspace.Invite, error) {
	var dbInvites []schema.WorkspaceInvite
	if err := r.db.Preload("Workspace").Preload("Inviter").
		Where("email = ? AND status = ?", email, string(workspace.InviteStatusPending)).
		Order("created_at DESC").Find(&dbInvites).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.Invite, len(dbInvites))
	for i := range dbInvites {
		result[i] = mapInviteToDomain(&dbInvites[i])
	}
	return result, nil
}

func (r *repository) UpdateInviteStatus(inviteID string, status workspace.InviteStatus) error {
	return r.db.Model(&schema.WorkspaceInvite{}).Where("id = ?", inviteID).Update("status", string(status)).Error
}

func (r *repository) PendingInviteExists(workspaceID, email string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.WorkspaceInvite{}).
		Where("workspace_id = ? AND email = ? AND status = ?", workspaceID, email, string(workspace.InviteStatusPending)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

var allowedWorkspaceResourceTables = map[string]struct{}{
	"agents":                   {},
	"campaigns":                {},
	"whatsapp_campaigns":       {},
	"whatsapp_templates":       {},
	"whatsapp_business_phones": {},
	"sip_trunks":               {},
	"support_inboxes":          {},
	"knowledge_bases":          {},
	"workflows":                {},
	"stages":                   {},
	"labels":                   {},
	"departments":              {},
	"message_shortcuts":        {},
}

func (r *repository) GetWorkspaceIDForResource(resourceTable, resourceID string) (string, error) {
	if _, ok := allowedWorkspaceResourceTables[resourceTable]; !ok {
		return "", workspace.ErrWorkspaceNotFound
	}
	var wsID string

	err := r.db.Raw("SELECT workspace_id FROM "+resourceTable+" WHERE id = ?", resourceID).Scan(&wsID).Error
	if err != nil {
		return "", err
	}
	if wsID == "" {
		return "", workspace.ErrWorkspaceNotFound
	}
	return wsID, nil
}

func (r *repository) AssignResource(assignment *workspace.ResourceAssignment) error {
	dbAssignment := schema.WorkspaceResourceAssignment{
		ID:           assignment.ID,
		WorkspaceID:  assignment.WorkspaceID,
		ResourceType: string(assignment.ResourceType),
		ResourceID:   assignment.ResourceID,
		MemberID:     assignment.MemberID,
	}
	if err := r.db.Create(&dbAssignment).Error; err != nil {
		return err
	}
	assignment.CreatedAt = dbAssignment.CreatedAt
	return nil
}

func (r *repository) UnassignResource(workspaceID string, resourceType workspace.Resource, resourceID, memberID string) error {
	result := r.db.Where(
		"workspace_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ?",
		workspaceID, string(resourceType), resourceID, memberID,
	).Delete(&schema.WorkspaceResourceAssignment{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace.ErrResourceAssignmentNotFound
	}
	return nil
}

func (r *repository) ListAssignmentsByResource(workspaceID string, resourceType workspace.Resource, resourceID string) ([]*workspace.ResourceAssignment, error) {
	var dbAssignments []schema.WorkspaceResourceAssignment
	if err := r.db.Preload("Member").Preload("Member.User").
		Where("workspace_id = ? AND resource_type = ? AND resource_id = ?", workspaceID, string(resourceType), resourceID).
		Find(&dbAssignments).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.ResourceAssignment, len(dbAssignments))
	for i := range dbAssignments {
		result[i] = mapAssignmentToDomain(&dbAssignments[i])
	}
	return result, nil
}

func (r *repository) ListAssignmentsByMember(memberID string, resourceType workspace.Resource) ([]*workspace.ResourceAssignment, error) {
	var dbAssignments []schema.WorkspaceResourceAssignment
	if err := r.db.Where("member_id = ? AND resource_type = ?", memberID, string(resourceType)).
		Find(&dbAssignments).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.ResourceAssignment, len(dbAssignments))
	for i := range dbAssignments {
		result[i] = mapAssignmentToDomain(&dbAssignments[i])
	}
	return result, nil
}

func (r *repository) IsResourceAssignedToMember(workspaceID string, resourceType workspace.Resource, resourceID, memberID string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.WorkspaceResourceAssignment{}).
		Where("workspace_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ?",
			workspaceID, string(resourceType), resourceID, memberID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) HasAnyAssignments(workspaceID string, resourceType workspace.Resource, resourceID string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.WorkspaceResourceAssignment{}).
		Where("workspace_id = ? AND resource_type = ? AND resource_id = ?",
			workspaceID, string(resourceType), resourceID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func mapWorkspaceToDomain(dbWs *schema.Workspace) *workspace.Workspace {
	return &workspace.Workspace{
		ID:        dbWs.ID,
		OwnerID:   dbWs.OwnerID,
		Name:      dbWs.Name,
		IsDefault: dbWs.IsDefault,
		CreatedAt: dbWs.CreatedAt,
		UpdatedAt: dbWs.UpdatedAt,
	}
}

func mapMemberToDomain(dbMember *schema.WorkspaceMember) *workspace.Member {
	m := &workspace.Member{
		ID:           dbMember.ID,
		WorkspaceID:  dbMember.WorkspaceID,
		UserID:       dbMember.UserID,
		Role:         workspace.Role(dbMember.Role),
		RoleID:       derefString(dbMember.RoleID),
		RingChannels: parseRingChannels(dbMember.RingChannels),
		CreatedAt:    dbMember.CreatedAt,
		UpdatedAt:    dbMember.UpdatedAt,
	}
	if dbMember.User.ID != "" {
		m.Email = dbMember.User.Email
		m.Username = dbMember.User.Username
	}
	if dbMember.CustomRole != nil && dbMember.CustomRole.ID != "" {
		m.RoleName = dbMember.CustomRole.Name
	}
	return m
}

func mapPermissionToDomain(dbPerm *schema.WorkspaceMemberPermission) *workspace.Permission {
	return &workspace.Permission{
		ID:        dbPerm.ID,
		MemberID:  dbPerm.MemberID,
		Resource:  workspace.Resource(dbPerm.Resource),
		Action:    workspace.Action(dbPerm.Action),
		CreatedAt: dbPerm.CreatedAt,
	}
}

func mapInviteToDomain(dbInvite *schema.WorkspaceInvite) *workspace.Invite {
	inv := &workspace.Invite{
		ID:          dbInvite.ID,
		WorkspaceID: dbInvite.WorkspaceID,
		InviterID:   dbInvite.InviterID,
		Email:       dbInvite.Email,
		Role:        workspace.Role(dbInvite.Role),
		RoleID:      derefString(dbInvite.RoleID),
		Status:      workspace.InviteStatus(dbInvite.Status),
		Token:       dbInvite.Token,
		ExpiresAt:   dbInvite.ExpiresAt,
		CreatedAt:   dbInvite.CreatedAt,
		UpdatedAt:   dbInvite.UpdatedAt,
	}
	if dbInvite.Permissions != "" {
		_ = json.Unmarshal([]byte(dbInvite.Permissions), &inv.Permissions)
	}
	if dbInvite.DepartmentIDs != "" {
		_ = json.Unmarshal([]byte(dbInvite.DepartmentIDs), &inv.DepartmentIDs)
	}
	if dbInvite.Workspace.ID != "" {
		inv.WorkspaceName = dbInvite.Workspace.Name
	}
	if dbInvite.Inviter.ID != "" {
		inv.InviterEmail = dbInvite.Inviter.Email
	}
	return inv
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func mapAssignmentToDomain(dbAssignment *schema.WorkspaceResourceAssignment) *workspace.ResourceAssignment {
	a := &workspace.ResourceAssignment{
		ID:           dbAssignment.ID,
		WorkspaceID:  dbAssignment.WorkspaceID,
		ResourceType: workspace.Resource(dbAssignment.ResourceType),
		ResourceID:   dbAssignment.ResourceID,
		MemberID:     dbAssignment.MemberID,
		CreatedAt:    dbAssignment.CreatedAt,
	}
	if dbAssignment.Member.User.ID != "" {
		a.MemberEmail = dbAssignment.Member.User.Email
		a.MemberUsername = dbAssignment.Member.User.Username
	}
	return a
}
