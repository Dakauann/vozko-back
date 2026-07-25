package workspacedepartment

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	workspacedepartmentdomain "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

type WorkspaceDepartmentHandler struct {
	create       workspacedepartmentdomain.CreateDepartmentUseCase
	get          workspacedepartmentdomain.GetDepartmentUseCase
	list         workspacedepartmentdomain.ListDepartmentsUseCase
	listByIDs    workspacedepartmentdomain.ListDepartmentsByIDsUseCase
	update       workspacedepartmentdomain.UpdateDepartmentUseCase
	delete       workspacedepartmentdomain.DeleteDepartmentUseCase
	addMember    workspacedepartmentdomain.AddMemberUseCase
	removeMember workspacedepartmentdomain.RemoveMemberUseCase
	listMembers  workspacedepartmentdomain.ListMembersUseCase
}

func NewWorkspaceDepartmentHandler(
	create workspacedepartmentdomain.CreateDepartmentUseCase,
	get workspacedepartmentdomain.GetDepartmentUseCase,
	list workspacedepartmentdomain.ListDepartmentsUseCase,
	listByIDs workspacedepartmentdomain.ListDepartmentsByIDsUseCase,
	update workspacedepartmentdomain.UpdateDepartmentUseCase,
	del workspacedepartmentdomain.DeleteDepartmentUseCase,
	addMember workspacedepartmentdomain.AddMemberUseCase,
	removeMember workspacedepartmentdomain.RemoveMemberUseCase,
	listMembers workspacedepartmentdomain.ListMembersUseCase,
) *WorkspaceDepartmentHandler {
	return &WorkspaceDepartmentHandler{
		create:       create,
		get:          get,
		list:         list,
		listByIDs:    listByIDs,
		update:       update,
		delete:       del,
		addMember:    addMember,
		removeMember: removeMember,
		listMembers:  listMembers,
	}
}

// @Summary		Criar um departamento
// @Description	Cria um novo departamento no workspace do usuário autenticado.
// @Tags			Departamentos
// @Accept			json
// @Produce		json
// @Param			request	body		CreateDepartmentRequest	true	"Dados do departamento"
// @Success		201	{object}	DepartmentResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments [post]
func (h *WorkspaceDepartmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)

	var req CreateDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	dept, err := h.create.Execute(workspacedepartmentdomain.CreateDepartmentInput{
		WorkspaceID: wsID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeDepartmentError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toDepartmentResponse(dept))
}

// @Summary		Consultar um departamento
// @Description	Retorna os detalhes de um departamento a partir do seu identificador.
// @Tags			Departamentos
// @Produce		json
// @Param			id	path		string	true	"Identificador do departamento"
// @Success		200	{object}	DepartmentResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments/{id} [get]
func (h *WorkspaceDepartmentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	dept, err := h.get.Execute(id)
	if err != nil {
		writeDepartmentError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toDepartmentResponse(dept))
}

// @Summary		Listar departamentos
// @Description	Retorna os departamentos do workspace visíveis para o usuário autenticado, respeitando o escopo de departamentos quando aplicável.
// @Tags			Departamentos
// @Produce		json
// @Success		200	{array}		DepartmentResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments [get]
func (h *WorkspaceDepartmentHandler) List(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	filter := middleware.GetDepartmentFilter(r)

	var depts []workspacedepartmentdomain.Department
	var err error
	if filter == nil || !filter.ShouldFilter() {
		depts, err = h.list.Execute(wsID)
	} else {
		if len(filter.DepartmentIDs) == 0 {

			response.WriteSuccess(w, http.StatusOK, toDepartmentResponses([]workspacedepartmentdomain.Department{}))
			return
		}
		depts, err = h.listByIDs.Execute(filter.DepartmentIDs)
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toDepartmentResponses(depts))
}

// @Summary		Atualizar um departamento
// @Description	Atualiza o nome e a descrição de um departamento. Todos os campos são opcionais.
// @Tags			Departamentos
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"Identificador do departamento"
// @Param			request	body		UpdateDepartmentRequest	true	"Campos do departamento a atualizar"
// @Success		200	{object}	DepartmentResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments/{id} [put]
func (h *WorkspaceDepartmentHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req UpdateDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	dept, err := h.update.Execute(id, workspacedepartmentdomain.UpdateDepartmentInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeDepartmentError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toDepartmentResponse(dept))
}

// @Summary		Remover um departamento
// @Description	Exclui um departamento do workspace do usuário autenticado.
// @Tags			Departamentos
// @Produce		json
// @Param			id	path		string	true	"Identificador do departamento"
// @Success		200	{object}	map[string]bool
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments/{id} [delete]
func (h *WorkspaceDepartmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if err := h.delete.Execute(id); err != nil {
		writeDepartmentError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

// @Summary		Adicionar um membro ao departamento
// @Description	Vincula um membro a um departamento do workspace.
// @Tags			Departamentos
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"Identificador do departamento"
// @Param			request	body		AddMemberRequest	true	"Membro a adicionar"
// @Success		201	{object}	DepartmentMemberResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments/{id}/members [post]
func (h *WorkspaceDepartmentHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	deptID := mux.Vars(r)["id"]

	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	dm, err := h.addMember.Execute(deptID, workspacedepartmentdomain.AddMemberInput{
		MemberID: req.MemberID,
	})
	if err != nil {
		writeDepartmentError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toDepartmentMemberResponse(dm))
}

// @Summary		Remover um membro do departamento
// @Description	Desvincula um membro de um departamento do workspace.
// @Tags			Departamentos
// @Produce		json
// @Param			id			path		string	true	"Identificador do departamento"
// @Param			memberId	path		string	true	"Identificador do membro"
// @Success		200	{object}	map[string]bool
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments/{id}/members/{memberId} [delete]
func (h *WorkspaceDepartmentHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	deptID := vars["id"]
	memberID := vars["memberId"]

	if err := h.removeMember.Execute(deptID, memberID); err != nil {
		writeDepartmentError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"removed": true})
}

// @Summary		Listar membros de um departamento
// @Description	Retorna os membros vinculados a um departamento do workspace.
// @Tags			Departamentos
// @Produce		json
// @Param			id	path		string	true	"Identificador do departamento"
// @Success		200	{array}		DepartmentMemberResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/departments/{id}/members [get]
func (h *WorkspaceDepartmentHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	deptID := mux.Vars(r)["id"]

	members, err := h.listMembers.Execute(deptID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toDepartmentMemberResponses(members))
}

func writeDepartmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacedepartmentdomain.ErrDepartmentNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspacedepartmentdomain.ErrDepartmentNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, workspacedepartmentdomain.ErrDepartmentMemberExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspacedepartmentdomain.ErrDepartmentMemberNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspacedepartmentdomain.ErrDepartmentRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
