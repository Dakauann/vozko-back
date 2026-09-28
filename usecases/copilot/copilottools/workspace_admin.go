package copilottools

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/user"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type WorkspaceAdminDeps struct {
	Members           workspace.ListMembersUseCase
	Invite            workspace.InviteMemberUseCase
	Invites           workspace.ListWorkspaceInvitesUseCase
	CancelInvite      workspace.CancelInviteUseCase
	RemoveMember      workspace.RemoveMemberUseCase
	ChangeRole        workspace.UpdateMemberRoleUseCase
	Permissions       workspace.GetMemberPermissionsUseCase
	SetPermissions    workspace.SetMemberPermissionsUseCase
	Catalog           workspace.ListResourcePermissionsUseCase
	Roles             workspace.ListCustomRolesUseCase
	CreateRole        workspace.CreateCustomRoleUseCase
	UpdateRole        workspace.UpdateCustomRoleUseCase
	DeleteRole        workspace.DeleteCustomRoleUseCase
	AssignRole        workspace.AssignCustomRoleUseCase
	CreateDepartment  wd.CreateDepartmentUseCase
	ScopedDepartments wd.ScopedDepartmentsUseCase
}

var errUnknownWorkspaceMember = fmt.Errorf("%w: membro não encontrado neste workspace; use o user_id exato de list_workspace_members", errInvalidArgs)

func platformRole(cc copilot.Context) string {
	if cc.SystemAdmin {
		return string(user.RoleAdmin)
	}
	return string(user.RoleUser)
}

func (d WorkspaceAdminDeps) members(cc copilot.Context) ([]*workspace.Member, error) {
	return d.Members.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc))
}

func (d WorkspaceAdminDeps) member(cc copilot.Context, userID string) (*workspace.Member, error) {
	members, err := d.members(cc)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(userID)
	for _, m := range members {
		if m != nil && m.UserID == id {
			return m, nil
		}
	}
	return nil, errUnknownWorkspaceMember
}

func (d WorkspaceAdminDeps) actor(cc copilot.Context) (workspace.Actor, error) {
	if cc.SystemAdmin {
		return workspace.Actor{PlatformAdmin: true}, nil
	}
	self, err := d.member(cc, cc.UserID)
	if err != nil {
		return workspace.Actor{}, workspace.ErrUnauthorized
	}
	return workspace.Actor{UserID: self.UserID, Role: self.Role}, nil
}

func (d WorkspaceAdminDeps) role(cc copilot.Context, roleID string) (*workspace.CustomRole, error) {
	roles, err := d.Roles.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc))
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(roleID)
	for _, r := range roles {
		if r != nil && r.ID == id {
			return r, nil
		}
	}
	return nil, fmt.Errorf("%w: função desconhecida; use o role_id exato de list_roles", errInvalidArgs)
}

func (d WorkspaceAdminDeps) department(cc copilot.Context, departmentID string) (*wd.Department, error) {
	dept, err := d.ScopedDepartments.Get(cc.WorkspaceID, strings.TrimSpace(departmentID))
	if err != nil {
		return nil, fmt.Errorf("%w: departamento desconhecido; use o id exato de list_departments", errInvalidArgs)
	}
	return dept, nil
}

func memberLabel(m *workspace.Member) string {
	name := strings.TrimSpace(m.Username)
	email := strings.TrimSpace(m.Email)
	switch {
	case name != "" && email != "":
		return name + " (" + email + ")"
	case name != "":
		return name
	}
	return email
}

func roleLabel(m *workspace.Member) string {
	if m.RoleName != "" {
		return m.RoleName
	}
	return string(m.Role)
}

func parsePermissions(raw []string) ([]workspace.PermissionEntry, error) {
	entries, err := workspace.ParsePermissionEntries(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v; use as permissões exatas de list_permission_catalog", errInvalidArgs, err)
	}
	return entries, nil
}

func requireCompletePermissions(set []workspace.PermissionEntry) error {
	unmet := workspace.UnmetRequirements(set)
	if len(unmet) == 0 {
		return nil
	}
	parts := make([]string, 0, len(unmet))
	for p, needs := range unmet {
		parts = append(parts, fmt.Sprintf("%s precisa também de %s", p.Key(), permissionKeys(needs)))
	}
	sort.Strings(parts)
	return fmt.Errorf("%w: %s", errInvalidArgs, strings.Join(parts, "; "))
}

func permissionKeys(entries []workspace.PermissionEntry) string {
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.Key())
	}
	return strings.Join(keys, ", ")
}

const adminRiskSummary = "Acesso administrativo completo ao workspace, incluindo gestão de membros e permissões, consumo de saldo, envio de mensagens a clientes e exclusão permanente de dados."

func riskSummary(entries []workspace.PermissionEntry) string {
	flagged := workspace.RiskyPermissions(entries)
	lines := make([]string, 0, len(flagged))
	for _, f := range flagged {
		reasons := make([]string, 0, len(f.Risks))
		for _, r := range f.Risks {
			reasons = append(reasons, r.Description)
		}
		lines = append(lines, workspace.DescribePermission(f.Permission)+": "+strings.Join(reasons, " "))
	}
	return strings.Join(lines, "\n")
}

func (d WorkspaceAdminDeps) grantedRisks(cc copilot.Context, role workspace.Role, roleID string) string {
	if role == workspace.RoleAdmin {
		return adminRiskSummary
	}
	if strings.TrimSpace(roleID) == "" {
		return ""
	}
	if r, err := d.role(cc, roleID); err == nil {
		return riskSummary(r.Permissions)
	}
	return ""
}

func withRisks(fields []copilot.Field, summary string) []copilot.Field {
	if summary == "" {
		return fields
	}
	return append(fields, copilot.Field{Key: "risks", Value: summary})
}

func riskKinds(entry workspace.PermissionEntry) []workspace.RiskKind {
	risks := workspace.RisksOf(entry)
	kinds := make([]workspace.RiskKind, 0, len(risks))
	for _, r := range risks {
		kinds = append(kinds, r.Kind)
	}
	return kinds
}

func permissionLabels(entries []workspace.PermissionEntry) string {
	labels := make([]string, 0, len(entries))
	for _, e := range entries {
		labels = append(labels, workspace.DescribePermission(e))
	}
	return strings.Join(labels, "; ")
}

func entryKeys(entries []workspace.PermissionEntry) []string {
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.Key())
	}
	return keys
}

func workspaceAdminFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, errInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, workspace.ErrUnauthorized), errors.Is(err, workspace.ErrInsufficientPermissions):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não pode fazer isso neste workspace (só o dono ou um administrador)"}
	case errors.Is(err, workspace.ErrCannotRemoveOwner):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o dono do workspace não pode ser removido"}
	case errors.Is(err, workspace.ErrCannotChangeOwnerRole):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o dono e os administradores têm todas as permissões; não dá para mudar a função ou as permissões deles assim"}
	case errors.Is(err, workspace.ErrCannotModifySelf):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não pode alterar as próprias permissões"}
	case errors.Is(err, workspace.ErrMemberNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: errUnknownWorkspaceMember.Error()}
	case errors.Is(err, workspace.ErrMemberAlreadyExists):
		return copilot.Result{Status: copilot.StatusError, Message: "essa pessoa já faz parte do workspace"}
	case errors.Is(err, workspace.ErrInviteAlreadyExists):
		return copilot.Result{Status: copilot.StatusError, Message: "já existe um convite pendente para esse e-mail"}
	case errors.Is(err, workspace.ErrCannotInviteSelf):
		return copilot.Result{Status: copilot.StatusError, Message: "o usuário não pode convidar a si mesmo"}
	case errors.Is(err, workspace.ErrInviteNotFound), errors.Is(err, workspace.ErrInviteAlreadyProcessed):
		return copilot.Result{Status: copilot.StatusError, Message: "convite desconhecido ou já respondido; use list_workspace_invites"}
	case errors.Is(err, workspace.ErrInvalidRole):
		return copilot.Result{Status: copilot.StatusError, Message: "função inválida: use admin, member ou uma função de list_roles"}
	case errors.Is(err, workspace.ErrRoleNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "função desconhecida; use o role_id exato de list_roles"}
	case errors.Is(err, workspace.ErrRoleNameRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "a função precisa de um nome"}
	case errors.Is(err, workspace.ErrRoleInUse):
		return copilot.Result{Status: copilot.StatusError, Message: "essa função ainda está atribuída a membros; troque a função deles antes de excluir"}
	case errors.Is(err, workspace.ErrInvalidDepartment), errors.Is(err, wd.ErrDepartmentNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "departamento desconhecido; use o id exato de list_departments"}
	case errors.Is(err, wd.ErrDepartmentNameRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "o departamento precisa de um nome"}
	case errors.Is(err, wd.ErrDepartmentMemberExists):
		return copilot.Result{Status: copilot.StatusError, Message: "essa pessoa já está nesse departamento"}
	case errors.Is(err, wd.ErrDepartmentMemberNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "essa pessoa não está nesse departamento"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao alterar o workspace"}
}
