package workspace

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/affiliate"
	workspacedomain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type WorkspaceHandler struct {
	createWorkspace         workspacedomain.CreateWorkspaceUseCase
	getWorkspace            workspacedomain.GetWorkspaceUseCase
	listWorkspaces          workspacedomain.ListWorkspacesUseCase
	updateWorkspace         workspacedomain.UpdateWorkspaceUseCase
	inviteMember            workspacedomain.InviteMemberUseCase
	acceptInvite            workspacedomain.AcceptInviteUseCase
	declineInvite           workspacedomain.DeclineInviteUseCase
	cancelInvite            workspacedomain.CancelInviteUseCase
	listInvites             workspacedomain.ListInvitesUseCase
	listWorkspaceInvites    workspacedomain.ListWorkspaceInvitesUseCase
	removeMember            workspacedomain.RemoveMemberUseCase
	updateMemberRole        workspacedomain.UpdateMemberRoleUseCase
	listMembers             workspacedomain.ListMembersUseCase
	listMembersPaginated    workspacedomain.ListMembersPaginatedUseCase
	listAssignableMembers   workspacedomain.ListAssignableMembersUseCase
	setMemberPermissions    workspacedomain.SetMemberPermissionsUseCase
	getMemberPermissions    workspacedomain.GetMemberPermissionsUseCase
	listResourcePermissions workspacedomain.ListResourcePermissionsUseCase
	checkAccess             workspacedomain.CheckAccessUseCase
	ensureDefault           workspacedomain.EnsureDefaultWorkspaceUseCase
	assignResource          workspacedomain.AssignResourceUseCase
	unassignResource        workspacedomain.UnassignResourceUseCase
	listResourceAssignments workspacedomain.ListResourceAssignmentsUseCase
	checkResourceAccess     workspacedomain.CheckResourceAccessUseCase
	createCustomRole        workspacedomain.CreateCustomRoleUseCase
	listCustomRoles         workspacedomain.ListCustomRolesUseCase
	updateCustomRole        workspacedomain.UpdateCustomRoleUseCase
	deleteCustomRole        workspacedomain.DeleteCustomRoleUseCase
	assignCustomRole        workspacedomain.AssignCustomRoleUseCase
	trackReferral           affiliate.TrackReferralUseCase
}

func NewWorkspaceHandler(
	createWorkspace workspacedomain.CreateWorkspaceUseCase,
	getWorkspace workspacedomain.GetWorkspaceUseCase,
	listWorkspaces workspacedomain.ListWorkspacesUseCase,
	updateWorkspace workspacedomain.UpdateWorkspaceUseCase,
	inviteMember workspacedomain.InviteMemberUseCase,
	acceptInvite workspacedomain.AcceptInviteUseCase,
	declineInvite workspacedomain.DeclineInviteUseCase,
	cancelInvite workspacedomain.CancelInviteUseCase,
	listInvites workspacedomain.ListInvitesUseCase,
	listWorkspaceInvites workspacedomain.ListWorkspaceInvitesUseCase,
	removeMember workspacedomain.RemoveMemberUseCase,
	updateMemberRole workspacedomain.UpdateMemberRoleUseCase,
	listMembers workspacedomain.ListMembersUseCase,
	listMembersPaginated workspacedomain.ListMembersPaginatedUseCase,
	listAssignableMembers workspacedomain.ListAssignableMembersUseCase,
	setMemberPermissions workspacedomain.SetMemberPermissionsUseCase,
	getMemberPermissions workspacedomain.GetMemberPermissionsUseCase,
	listResourcePermissions workspacedomain.ListResourcePermissionsUseCase,
	checkAccess workspacedomain.CheckAccessUseCase,
	ensureDefault workspacedomain.EnsureDefaultWorkspaceUseCase,
	assignResource workspacedomain.AssignResourceUseCase,
	unassignResource workspacedomain.UnassignResourceUseCase,
	listResourceAssignments workspacedomain.ListResourceAssignmentsUseCase,
	checkResourceAccess workspacedomain.CheckResourceAccessUseCase,
	createCustomRole workspacedomain.CreateCustomRoleUseCase,
	listCustomRoles workspacedomain.ListCustomRolesUseCase,
	updateCustomRole workspacedomain.UpdateCustomRoleUseCase,
	deleteCustomRole workspacedomain.DeleteCustomRoleUseCase,
	assignCustomRole workspacedomain.AssignCustomRoleUseCase,
	trackReferral affiliate.TrackReferralUseCase,
) *WorkspaceHandler {
	return &WorkspaceHandler{
		createWorkspace:         createWorkspace,
		getWorkspace:            getWorkspace,
		listWorkspaces:          listWorkspaces,
		updateWorkspace:         updateWorkspace,
		inviteMember:            inviteMember,
		acceptInvite:            acceptInvite,
		declineInvite:           declineInvite,
		cancelInvite:            cancelInvite,
		listInvites:             listInvites,
		listWorkspaceInvites:    listWorkspaceInvites,
		removeMember:            removeMember,
		updateMemberRole:        updateMemberRole,
		listMembers:             listMembers,
		listMembersPaginated:    listMembersPaginated,
		listAssignableMembers:   listAssignableMembers,
		setMemberPermissions:    setMemberPermissions,
		getMemberPermissions:    getMemberPermissions,
		listResourcePermissions: listResourcePermissions,
		checkAccess:             checkAccess,
		ensureDefault:           ensureDefault,
		assignResource:          assignResource,
		unassignResource:        unassignResource,
		listResourceAssignments: listResourceAssignments,
		checkResourceAccess:     checkResourceAccess,
		createCustomRole:        createCustomRole,
		listCustomRoles:         listCustomRoles,
		updateCustomRole:        updateCustomRole,
		deleteCustomRole:        deleteCustomRole,
		assignCustomRole:        assignCustomRole,
		trackReferral:           trackReferral,
	}
}

// @Summary		Criar workspace
// @Description	Cria um novo workspace para o usuário autenticado. O nome é obrigatório e um código de indicação pode ser informado no corpo ou pelo contexto da requisição.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			request	body		CreateWorkspaceRequest	true	"Dados do workspace a criar"
// @Success		201	{object}	workspace.Workspace
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces [post]
func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name": "string (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	ws, err := h.createWorkspace.Execute(claims.UserID, req.Name)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	code := strings.TrimSpace(req.ReferralCode)
	if code == "" {
		code = middleware.ExtractReferralCode(r)
	}
	if code != "" && h.trackReferral != nil {
		if _, refErr := h.trackReferral.Execute(r.Context(), affiliate.TrackReferralInput{
			Code:                 code,
			WorkspaceID:          ws.ID,
			WorkspaceOwnerUserID: claims.UserID,
		}); refErr != nil {
			log.Printf("[workspace] referral tracking failed for ws=%s code=%s: %v", ws.ID, code, refErr)
		}
	}

	response.WriteSuccess(w, http.StatusCreated, ws)
}

// @Summary		Consultar workspace
// @Description	Retorna os dados de um workspace ao qual o usuário autenticado tem acesso.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Success		200	{object}	workspace.Workspace
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId} [get]
func (h *WorkspaceHandler) Get(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	ws, err := h.getWorkspace.Execute(claims.UserID, claims.Role, wsID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, ws)
}

// @Summary		Listar workspaces
// @Description	Retorna, de forma paginada, os workspaces aos quais o usuário autenticado tem acesso. Aceita busca por nome e por email de membro.
// @Tags			Workspaces
// @Produce		json
// @Param			search			query	string	false	"Busca por nome do workspace"
// @Param			member_email	query	string	false	"Filtra por email de um membro"
// @Param			page			query	int		false	"Número da página"
// @Param			pageSize		query	int		false	"Tamanho da página"
// @Success		200	{array}		workspace.Workspace
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces [get]
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()
	search := strings.TrimSpace(values.Get("search"))
	memberEmail := strings.TrimSpace(values.Get("member_email"))
	pagination := httpx.ParsePagination(values)

	result, err := h.listWorkspaces.Execute(claims.UserID, claims.Role, search, memberEmail, pagination.Page, pagination.PageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

// @Summary		Atualizar workspace
// @Description	Atualiza o nome de um workspace existente. O campo é opcional.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string					true	"ID do workspace"
// @Param			request		body		UpdateWorkspaceRequest	true	"Campos do workspace a atualizar"
// @Success		200	{object}	workspace.Workspace
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId} [put]
func (h *WorkspaceHandler) Update(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	var req UpdateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name": "string (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	ws, err := h.updateWorkspace.Execute(claims.UserID, wsID, claims.Role, workspacedomain.UpdateWorkspaceInput{
		Name: req.Name,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, ws)
}

// @Summary		Convidar membro
// @Description	Envia um convite de participação no workspace para o email informado. É possível atribuir um cargo administrativo, um cargo personalizado e departamentos.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string				true	"ID do workspace"
// @Param			request		body		InviteMemberRequest	true	"Dados do convite"
// @Success		201	{object}	workspace.Invite
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members/invite [post]
func (h *WorkspaceHandler) InviteMember(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	var req InviteMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"email":         "string (required)",
			"role":          "string (admin or omit when roleId is provided)",
			"roleId":        "string (custom role ID, optional)",
			"departmentIds": "[]string (department IDs, optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	invite, err := h.inviteMember.Execute(claims.UserID, wsID, claims.Role, workspacedomain.InviteMemberInput{
		Email:         req.Email,
		Role:          toWorkspaceRole(req.Role),
		RoleID:        req.RoleID,
		DepartmentIDs: req.DepartmentIDs,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, invite)
}

// @Summary		Aceitar convite
// @Description	Aceita um convite de participação em um workspace a partir do token recebido.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			request	body		AcceptInviteRequest	true	"Token do convite"
// @Success		200	{object}	workspace.Member
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		410	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/invites/accept [post]
func (h *WorkspaceHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req AcceptInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"token": "string (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	member, err := h.acceptInvite.Execute(claims.UserID, claims.Email, req.Token)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, member)
}

// @Summary		Recusar convite
// @Description	Recusa um convite de participação em um workspace destinado ao usuário autenticado.
// @Tags			Workspaces
// @Produce		json
// @Param			inviteId	path		string	true	"ID do convite"
// @Success		200	{object}	map[string]string
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/invites/{inviteId}/decline [post]
func (h *WorkspaceHandler) DeclineInvite(w http.ResponseWriter, r *http.Request) {
	inviteID := mux.Vars(r)["inviteId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if err := h.declineInvite.Execute(claims.UserID, claims.Email, inviteID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "invite declined"})
}

// @Summary		Cancelar convite
// @Description	Cancela um convite pendente de um workspace. Requer permissão de gestão de membros.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Param			inviteId	path		string	true	"ID do convite"
// @Success		200	{object}	map[string]string
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/invites/{inviteId} [delete]
func (h *WorkspaceHandler) CancelInvite(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	inviteID := vars["inviteId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if err := h.cancelInvite.Execute(claims.UserID, wsID, claims.Role, inviteID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "invite cancelled"})
}

// @Summary		Listar meus convites
// @Description	Retorna os convites de workspace pendentes para o email do usuário autenticado.
// @Tags			Workspaces
// @Produce		json
// @Success		200	{array}		workspace.Invite
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/invites [get]
func (h *WorkspaceHandler) ListMyInvites(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	invites, err := h.listInvites.Execute(claims.Email)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, invites)
}

// @Summary		Listar convites do workspace
// @Description	Retorna os convites emitidos por um workspace. Requer permissão de leitura de membros.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Success		200	{array}		workspace.Invite
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/invites [get]
func (h *WorkspaceHandler) ListWorkspaceInvites(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	invites, err := h.listWorkspaceInvites.Execute(claims.UserID, wsID, claims.Role)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, invites)
}

// @Summary		Remover membro
// @Description	Remove um membro do workspace. O proprietário não pode ser removido. Requer permissão de exclusão de membros.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Param			userId		path		string	true	"ID do usuário membro"
// @Success		200	{object}	map[string]string
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members/{userId} [delete]
func (h *WorkspaceHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	memberUserID := vars["userId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if err := h.removeMember.Execute(claims.UserID, wsID, memberUserID, claims.Role); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "member removed"})
}

// @Summary		Atualizar cargo do membro
// @Description	Altera o cargo administrativo de um membro do workspace. O cargo do proprietário não pode ser alterado.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string					true	"ID do workspace"
// @Param			userId		path		string					true	"ID do usuário membro"
// @Param			request		body		UpdateMemberRoleRequest	true	"Novo cargo do membro"
// @Success		200	{object}	workspace.Member
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members/{userId}/role [put]
func (h *WorkspaceHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	memberUserID := vars["userId"]

	var req UpdateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"role": "string (required: admin)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	member, err := h.updateMemberRole.Execute(claims.UserID, wsID, memberUserID, claims.Role, toWorkspaceRole(req.Role))
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, member)
}

// @Summary		Listar membros
// @Description	Retorna todos os membros de um workspace ao qual o usuário autenticado tem acesso.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Success		200	{array}		workspace.Member
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members [get]
func (h *WorkspaceHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	members, err := h.listMembers.Execute(claims.UserID, wsID, claims.Role)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, members)
}

func (h *WorkspaceHandler) ListMembersPaginated(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if claims.Role != "admin" {
		response.WriteError(w, http.StatusForbidden, "Admin access required", nil)
		return
	}

	values := r.URL.Query()
	pagination := httpx.ParsePagination(values)

	members, total, err := h.listMembersPaginated.Execute(wsID, pagination.Page, pagination.PageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	type paginatedMembers struct {
		Items    []*workspacedomain.Member `json:"items"`
		Total    int64                     `json:"total"`
		Page     int                       `json:"page"`
		PageSize int                       `json:"pageSize"`
	}

	response.WriteSuccess(w, http.StatusOK, paginatedMembers{
		Items:    members,
		Total:    total,
		Page:     pagination.Page,
		PageSize: pagination.PageSize,
	})
}

// @Summary		Listar membros atribuíveis
// @Description	Retorna, de forma paginada e pesquisável, os membros aos quais o usuário autenticado pode atribuir uma conversa, com escopo definido pelo acesso aos departamentos.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Param			search		query		string	false	"Busca por nome ou email do membro"
// @Param			page		query		int		false	"Número da página"
// @Param			pageSize	query		int		false	"Tamanho da página"
// @Success		200	{array}		workspace.AssignableMember
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members/assignable [get]
func (h *WorkspaceHandler) ListAssignableMembers(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()
	pagination := httpx.ParsePagination(values)
	search := strings.TrimSpace(values.Get("search"))

	members, total, err := h.listAssignableMembers.Execute(
		claims.UserID, wsID, claims.Role == "admin", search, pagination.Page, pagination.PageSize,
	)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	totalPages := 0
	if pagination.PageSize > 0 {
		totalPages = int((total + int64(pagination.PageSize) - 1) / int64(pagination.PageSize))
	}

	response.WritePaginated(w, http.StatusOK, members, response.PaginationMeta{
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
		TotalItems: total,
	})
}

// @Summary		Definir permissões do membro
// @Description	Substitui o conjunto de permissões de um membro do workspace pela lista informada.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string							true	"ID do workspace"
// @Param			userId		path		string							true	"ID do usuário membro"
// @Param			request		body		workspace.SetPermissionsInput	true	"Permissões a definir"
// @Success		200	{array}		workspace.Permission
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members/{userId}/permissions [put]
func (h *WorkspaceHandler) SetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	memberUserID := vars["userId"]

	var req workspacedomain.SetPermissionsInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"permissions": "[]{ resource: string, action: string } (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	perms, err := h.setMemberPermissions.Execute(claims.UserID, wsID, memberUserID, claims.Role, req)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, perms)
}

// @Summary		Consultar permissões do membro
// @Description	Retorna as permissões atribuídas a um membro do workspace.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Param			userId		path		string	true	"ID do usuário membro"
// @Success		200	{array}		workspace.Permission
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members/{userId}/permissions [get]
func (h *WorkspaceHandler) GetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	memberUserID := vars["userId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	perms, err := h.getMemberPermissions.Execute(claims.UserID, wsID, memberUserID, claims.Role)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, perms)
}

// @Summary		Listar permissões disponíveis
// @Description	Retorna o catálogo de recursos e ações que podem ser concedidos a membros e cargos do workspace.
// @Tags			Workspaces
// @Produce		json
// @Success		200	{object}	map[string]interface{}
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/permissions [get]
func (h *WorkspaceHandler) ListResourcePermissions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	permissions := h.listResourcePermissions.Execute()
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"permissions": permissions})
}

// @Summary		Atribuir recurso a um membro
// @Description	Concede a um membro do workspace o acesso a um recurso específico identificado por tipo e ID.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string							true	"ID do workspace"
// @Param			request		body		workspace.AssignResourceInput	true	"Recurso e membro a associar"
// @Success		201	{object}	workspace.ResourceAssignment
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/assignments [post]
func (h *WorkspaceHandler) AssignResource(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	var req workspacedomain.AssignResourceInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"resourceType": "string (required)",
			"resourceId":   "string (required)",
			"memberUserId": "string (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	assignment, err := h.assignResource.Execute(claims.UserID, wsID, claims.Role, req)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, assignment)
}

// @Summary		Remover atribuição de recurso
// @Description	Revoga o acesso de um membro a um recurso específico do workspace.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId		path		string	true	"ID do workspace"
// @Param			resourceType	path		string	true	"Tipo do recurso"
// @Param			resourceId		path		string	true	"ID do recurso"
// @Param			userId			path		string	true	"ID do usuário membro"
// @Success		200	{object}	map[string]string
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/assignments/{resourceType}/{resourceId}/members/{userId} [delete]
func (h *WorkspaceHandler) UnassignResource(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	resourceType := vars["resourceType"]
	resourceID := vars["resourceId"]
	memberUserID := vars["userId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if err := h.unassignResource.Execute(claims.UserID, wsID, resourceType, resourceID, memberUserID, claims.Role); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "resource unassigned"})
}

// @Summary		Listar atribuições de recurso
// @Description	Retorna os membros associados a um recurso específico do workspace.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId		path		string	true	"ID do workspace"
// @Param			resourceType	path		string	true	"Tipo do recurso"
// @Param			resourceId		path		string	true	"ID do recurso"
// @Success		200	{array}		workspace.ResourceAssignment
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/assignments/{resourceType}/{resourceId} [get]
func (h *WorkspaceHandler) ListResourceAssignments(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	resourceType := vars["resourceType"]
	resourceID := vars["resourceId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	assignments, err := h.listResourceAssignments.Execute(claims.UserID, wsID, claims.Role, resourceType, resourceID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, assignments)
}

// @Summary		Criar cargo personalizado
// @Description	Cria um cargo personalizado no workspace com um conjunto próprio de permissões.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string							true	"ID do workspace"
// @Param			request		body		workspace.CreateCustomRoleInput	true	"Dados do cargo a criar"
// @Success		201	{object}	workspace.CustomRole
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/roles [post]
func (h *WorkspaceHandler) CreateCustomRole(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	var req workspacedomain.CreateCustomRoleInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":        "string (required)",
			"description": "string (optional)",
			"permissions": "[]{ resource: string, action: string } (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	role, err := h.createCustomRole.Execute(claims.UserID, wsID, claims.Role, req)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, role)
}

// @Summary		Listar cargos personalizados
// @Description	Retorna os cargos personalizados definidos no workspace.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Success		200	{object}	map[string]interface{}
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/roles [get]
func (h *WorkspaceHandler) ListCustomRoles(w http.ResponseWriter, r *http.Request) {
	wsID := mux.Vars(r)["workspaceId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	roles, err := h.listCustomRoles.Execute(claims.UserID, wsID, claims.Role)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"roles": roles})
}

// @Summary		Atualizar cargo personalizado
// @Description	Atualiza o nome, a descrição e/ou as permissões de um cargo personalizado do workspace.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string							true	"ID do workspace"
// @Param			roleId		path		string							true	"ID do cargo"
// @Param			request		body		workspace.UpdateCustomRoleInput	true	"Campos do cargo a atualizar"
// @Success		200	{object}	workspace.CustomRole
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/roles/{roleId} [put]
func (h *WorkspaceHandler) UpdateCustomRole(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	roleID := vars["roleId"]

	var req workspacedomain.UpdateCustomRoleInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	role, err := h.updateCustomRole.Execute(claims.UserID, wsID, claims.Role, roleID, req)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, role)
}

// @Summary		Excluir cargo personalizado
// @Description	Exclui um cargo personalizado do workspace. Cargos em uso não podem ser excluídos.
// @Tags			Workspaces
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Param			roleId		path		string	true	"ID do cargo"
// @Success		200	{object}	map[string]string
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/roles/{roleId} [delete]
func (h *WorkspaceHandler) DeleteCustomRole(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	roleID := vars["roleId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if err := h.deleteCustomRole.Execute(claims.UserID, wsID, claims.Role, roleID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "role deleted"})
}

// @Summary		Atribuir cargo personalizado a um membro
// @Description	Atribui um cargo personalizado a um membro do workspace.
// @Tags			Workspaces
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string					true	"ID do workspace"
// @Param			userId		path		string					true	"ID do usuário membro"
// @Param			request		body		AssignCustomRoleRequest	true	"ID do cargo a atribuir"
// @Success		200	{object}	workspace.Member
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/members/{userId}/assign-role [put]
func (h *WorkspaceHandler) AssignCustomRole(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsID := vars["workspaceId"]
	memberUserID := vars["userId"]

	var req AssignCustomRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"roleId": "string (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	member, err := h.assignCustomRole.Execute(claims.UserID, wsID, memberUserID, claims.Role, req.RoleID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, member)
}

func (h *WorkspaceHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacedomain.ErrWorkspaceNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrWorkspaceNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrMemberNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrMemberAlreadyExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInviteNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInviteAlreadyExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInviteExpired):
		response.WriteError(w, http.StatusGone, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInviteAlreadyProcessed):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInvalidRole):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInvalidResource):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInvalidAction):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrCannotRemoveOwner):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrCannotChangeOwnerRole):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrCannotModifySelf):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInsufficientPermissions):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrDefaultWorkspace):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrResourceAssignmentNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrResourceAlreadyAssigned):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrResourceAccessDenied):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrRoleNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrRoleNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrRoleInUse):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrInvalidDepartment):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrCannotInviteSelf):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspacedomain.ErrWorkspaceLimitReached):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
