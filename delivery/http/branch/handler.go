package branch

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	branchdomain "vozko/domain/branch"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
)

type BranchHandler struct {
	createUC   branchdomain.CreateUseCase
	updateUC   branchdomain.UpdateUseCase
	getUC      branchdomain.GetUseCase
	listWSUC   branchdomain.ListByWorkspaceUseCase
	listUserUC branchdomain.ListByUserUseCase
	deleteUC   branchdomain.DeleteUseCase
	enableUC   branchdomain.EnableUseCase
	rotateUC   branchdomain.RotateSecretUseCase
	sip        BranchSIPConnConfig
}

type BranchSIPConnConfig struct {
	Server    string
	Port      int
	Transport string
	Realm     string
}

func NewBranchHandler(
	createUC branchdomain.CreateUseCase,
	updateUC branchdomain.UpdateUseCase,
	getUC branchdomain.GetUseCase,
	listWSUC branchdomain.ListByWorkspaceUseCase,
	listUserUC branchdomain.ListByUserUseCase,
	deleteUC branchdomain.DeleteUseCase,
	enableUC branchdomain.EnableUseCase,
	rotateUC branchdomain.RotateSecretUseCase,
	sip BranchSIPConnConfig,
) *BranchHandler {
	if sip.Transport == "" {
		sip.Transport = "UDP"
	}
	return &BranchHandler{
		createUC:   createUC,
		updateUC:   updateUC,
		getUC:      getUC,
		listWSUC:   listWSUC,
		listUserUC: listUserUC,
		deleteUC:   deleteUC,
		enableUC:   enableUC,
		rotateUC:   rotateUC,
		sip:        sip,
	}
}

func (h *BranchHandler) connectionInfo(realm, sipUser string, codecs []branchdomain.CodecID) branchConnectionInfo {
	out := branchConnectionInfo{
		Server:     h.sip.Server,
		Port:       h.sip.Port,
		Transport:  h.sip.Transport,
		Realm:      realm,
		SIPUser:    sipUser,
		Configured: h.sip.Server != "",
	}
	for _, c := range codecs {
		out.Codecs = append(out.Codecs, string(c))
	}
	return out
}

// @Summary		Obter configuração SIP de registro
// @Description	Retorna o endpoint SIP do sistema (servidor, porta, transporte e realm) para o qual um softphone deve se registrar. Não reexibe a senha de uso único; o usuário do ramal é o sipUser do próprio ramal.
// @Tags			Ramais
// @Produce		json
// @Success		200	{object}	map[string]interface{}
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/sip-config [get]
func (h *BranchHandler) SIPConfig(w http.ResponseWriter, r *http.Request) {
	response.WriteSuccess(w, http.StatusOK, branchConnectionInfo{
		Server:     h.sip.Server,
		Port:       h.sip.Port,
		Transport:  h.sip.Transport,
		Realm:      h.sip.Realm,
		Configured: h.sip.Server != "",
	})
}

func (h *BranchHandler) toBranchSecretResponse(res *branchdomain.SecretResult) branchSecretResponse {
	var codecs []branchdomain.CodecID
	if res.Branch != nil {
		codecs = res.Branch.Codecs
	}
	return branchSecretResponse{
		Branch:     toBranchResponse(res.Branch),
		Secret:     res.Secret,
		Realm:      res.Realm,
		SIPUser:    res.SIPUser,
		Connection: h.connectionInfo(res.Realm, res.SIPUser, codecs),
	}
}

func isBranchValidationError(err error) bool {
	return errors.Is(err, branchdomain.ErrBranchWorkspaceRequired) ||
		errors.Is(err, branchdomain.ErrBranchMemberRequired) ||
		errors.Is(err, branchdomain.ErrBranchSIPUserRequired) ||
		errors.Is(err, branchdomain.ErrBranchSIPUserInvalid) ||
		errors.Is(err, branchdomain.ErrBranchDisplayNameInvalid) ||
		errors.Is(err, branchdomain.ErrBranchRealmInvalid) ||
		errors.Is(err, branchdomain.ErrBranchMaxContactsInvalid) ||
		errors.Is(err, branchdomain.ErrBranchCodecInvalid) ||
		errors.Is(err, branchdomain.ErrBranchRegistrationStatusInvalid)
}

func branchErrorStatusCode(err error) (int, string) {
	switch {
	case errors.Is(err, branchdomain.ErrBranchNotFound):
		return http.StatusNotFound, "branch_not_found"
	case errors.Is(err, branchdomain.ErrBranchMemberNotFound):
		return http.StatusNotFound, "branch_member_not_found"
	case errors.Is(err, branchdomain.ErrBranchForbidden):
		return http.StatusForbidden, "branch_forbidden"
	case errors.Is(err, branchdomain.ErrBranchSIPUserTaken):
		return http.StatusConflict, "branch_sip_user_taken"
	case errors.Is(err, branchdomain.ErrBranchLimitReached):
		return http.StatusConflict, "branch_limit_reached"
	case errors.Is(err, branchdomain.ErrBranchSIPUserRequired), errors.Is(err, branchdomain.ErrBranchSIPUserInvalid):
		return http.StatusBadRequest, "branch_sip_user_invalid"
	case errors.Is(err, branchdomain.ErrBranchMemberRequired):
		return http.StatusBadRequest, "branch_member_required"
	case errors.Is(err, branchdomain.ErrBranchMaxContactsInvalid):
		return http.StatusBadRequest, "branch_max_contacts_invalid"
	case isBranchValidationError(err):
		return http.StatusBadRequest, "branch_invalid"
	default:
		return http.StatusInternalServerError, ""
	}
}

func writeBranchMutationError(w http.ResponseWriter, err error, fallback string) {
	status, code := branchErrorStatusCode(err)
	if code == "" {
		response.WriteError(w, status, fallback, nil)
		return
	}
	response.WriteErrorWithCode(w, status, code, err.Error(), nil)
}

func branchActor(r *http.Request, wsID string) branchdomain.Actor {
	claims := middleware.GetClaims(r)
	return branchdomain.Actor{
		WorkspaceID: wsID,
		IsAdmin:     claims != nil && claims.Role == string(user.RoleAdmin),
	}
}

func branchPageParams(r *http.Request) (int, int) {
	page, pageSize := 1, 20
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 200 {
			pageSize = v
		}
	}
	return page, pageSize
}

func writeBranchList(w http.ResponseWriter, items []*branchdomain.Branch, total int64, page, pageSize int) {
	out := make([]branchResponse, len(items))
	for i, b := range items {
		out[i] = toBranchResponse(b)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// @Summary		Criar ramal
// @Description	Cria um novo ramal (extensão SIP) para um membro do workspace atual. Retorna a senha SIP de uso único, exibida apenas nesta resposta, para configurar o softphone.
// @Tags			Ramais
// @Accept			json
// @Produce		json
// @Param			request	body		branch.CreateBranchInput	true	"Dados do ramal a criar"
// @Success		201	{object}	branch.SecretResult
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches [post]
func (h *BranchHandler) CreateForWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}

	var input branchdomain.CreateBranchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	input.WorkspaceID = wsID
	input.Actor = branchActor(r, wsID)

	res, err := h.createUC.Execute(input)
	if err != nil {
		writeBranchMutationError(w, err, "Failed to create branch")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, h.toBranchSecretResponse(res))
}

// @Summary		Listar ramais
// @Description	Retorna os ramais do workspace atual, com paginação por page e pageSize. As senhas SIP nunca são retornadas na listagem.
// @Tags			Ramais
// @Produce		json
// @Param			page		query	int	false	"Número da página (padrão 1)"
// @Param			pageSize	query	int	false	"Itens por página (padrão 20, máximo 200)"
// @Success		200	{object}	map[string]interface{}
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches [get]
func (h *BranchHandler) ListForWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}
	page, pageSize := branchPageParams(r)
	items, total, err := h.listWSUC.Execute(wsID, page, pageSize)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list branches", nil)
		return
	}
	writeBranchList(w, items, total, page, pageSize)
}

func (h *BranchHandler) loadOwned(w http.ResponseWriter, r *http.Request) *branchdomain.Branch {
	b, err := h.getUC.Execute(mux.Vars(r)["branchId"])
	if err != nil {
		writeBranchMutationError(w, err, "Failed to get branch")
		return nil
	}
	claims := middleware.GetClaims(r)
	isAdmin := claims != nil && claims.Role == string(user.RoleAdmin)
	if !isAdmin && !b.IsOwnedBy(middleware.GetWorkspaceID(r)) {
		response.WriteError(w, http.StatusForbidden, branchdomain.ErrBranchForbidden.Error(), nil)
		return nil
	}
	return b
}

// @Summary		Obter ramal
// @Description	Retorna os detalhes de um ramal do workspace atual. A senha SIP não é incluída.
// @Tags			Ramais
// @Produce		json
// @Param			branchId	path	string	true	"ID do ramal"
// @Success		200	{object}	branch.Branch
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/{branchId} [get]
func (h *BranchHandler) GetForWorkspace(w http.ResponseWriter, r *http.Request) {
	b := h.loadOwned(w, r)
	if b == nil {
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBranchResponse(b))
}

// @Summary		Atualizar ramal
// @Description	Atualiza os atributos de um ramal do workspace. Todos os campos são opcionais; os campos de identidade (usuário SIP) não são alteráveis aqui.
// @Tags			Ramais
// @Accept			json
// @Produce		json
// @Param			branchId	path		string						true	"ID do ramal"
// @Param			request		body		branch.UpdateBranchInput	true	"Campos do ramal a atualizar"
// @Success		200	{object}	branch.Branch
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/{branchId} [put]
func (h *BranchHandler) UpdateForWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}
	var input branchdomain.UpdateBranchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	input.ID = mux.Vars(r)["branchId"]
	input.Actor = branchActor(r, wsID)

	b, err := h.updateUC.Execute(input)
	if err != nil {
		writeBranchMutationError(w, err, "Failed to update branch")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBranchResponse(b))
}

// @Summary		Remover ramal
// @Description	Exclui um ramal do workspace atual.
// @Tags			Ramais
// @Produce		json
// @Param			branchId	path	string	true	"ID do ramal"
// @Success		200	{object}	map[string]string
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/{branchId} [delete]
func (h *BranchHandler) DeleteForWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}
	if err := h.deleteUC.Execute(mux.Vars(r)["branchId"], branchActor(r, wsID)); err != nil {
		writeBranchMutationError(w, err, "Failed to delete branch")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "branch deleted"})
}

// @Summary		Ativar ou desativar ramal
// @Description	Ativa ou desativa um ramal do workspace conforme o campo enabled informado.
// @Tags			Ramais
// @Accept			json
// @Produce		json
// @Param			branchId	path		string				true	"ID do ramal"
// @Param			request		body		EnableBranchRequest	true	"Estado desejado do ramal"
// @Success		200	{object}	branch.Branch
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/{branchId}/enable [put]
func (h *BranchHandler) EnableForWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}
	var body EnableBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	b, err := h.enableUC.Execute(mux.Vars(r)["branchId"], body.Enabled, branchActor(r, wsID))
	if err != nil {
		writeBranchMutationError(w, err, "Failed to update branch")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBranchResponse(b))
}

// @Summary		Rotacionar senha do ramal
// @Description	Gera uma nova senha SIP para o ramal do workspace e a retorna uma única vez, para reconfigurar o softphone.
// @Tags			Ramais
// @Produce		json
// @Param			branchId	path	string	true	"ID do ramal"
// @Success		200	{object}	branch.SecretResult
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/{branchId}/rotate-secret [post]
func (h *BranchHandler) RotateSecretForWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}
	res, err := h.rotateUC.Execute(mux.Vars(r)["branchId"], branchActor(r, wsID))
	if err != nil {
		writeBranchMutationError(w, err, "Failed to rotate branch secret")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.toBranchSecretResponse(res))
}

// @Summary		Listar meus ramais
// @Description	Retorna os ramais que pertencem ao usuário autenticado no workspace atual, para configurar seu próprio softphone.
// @Tags			Ramais
// @Produce		json
// @Success		200	{object}	map[string]interface{}
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/mine [get]
func (h *BranchHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)
	if claims == nil || wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	items, err := h.listUserUC.Execute(wsID, claims.UserID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list branches", nil)
		return
	}
	out := make([]branchResponse, len(items))
	for i, b := range items {
		out[i] = toBranchResponse(b)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"items": out})
}

// @Summary		Rotacionar senha do meu ramal
// @Description	Permite que um membro gere uma nova senha SIP para um ramal que lhe pertence, sem depender de um gestor. A propriedade é verificada contra o usuário autenticado.
// @Tags			Ramais
// @Produce		json
// @Param			branchId	path	string	true	"ID do ramal"
// @Success		200	{object}	branch.SecretResult
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/branches/mine/{branchId}/rotate-secret [post]
func (h *BranchHandler) RotateMineSecret(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)
	if claims == nil || wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	b, err := h.getUC.Execute(mux.Vars(r)["branchId"])
	if err != nil {
		writeBranchMutationError(w, err, "Failed to get branch")
		return
	}
	if !b.IsOwnedBy(wsID) || strings.TrimSpace(b.UserID) != strings.TrimSpace(claims.UserID) {
		response.WriteError(w, http.StatusForbidden, branchdomain.ErrBranchForbidden.Error(), nil)
		return
	}
	res, err := h.rotateUC.Execute(b.ID, branchdomain.Actor{WorkspaceID: wsID})
	if err != nil {
		writeBranchMutationError(w, err, "Failed to rotate branch secret")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.toBranchSecretResponse(res))
}

func (h *BranchHandler) AdminCreate(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(mux.Vars(r)["workspaceId"])
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, branchdomain.ErrBranchWorkspaceRequired.Error(), nil)
		return
	}
	var input branchdomain.CreateBranchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	input.WorkspaceID = wsID
	input.Actor = branchdomain.Actor{WorkspaceID: wsID, IsAdmin: true}

	res, err := h.createUC.Execute(input)
	if err != nil {
		writeBranchMutationError(w, err, "Failed to create branch")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, h.toBranchSecretResponse(res))
}

func (h *BranchHandler) AdminListByWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(mux.Vars(r)["workspaceId"])
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, branchdomain.ErrBranchWorkspaceRequired.Error(), nil)
		return
	}
	page, pageSize := branchPageParams(r)
	items, total, err := h.listWSUC.Execute(wsID, page, pageSize)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list branches", nil)
		return
	}
	writeBranchList(w, items, total, page, pageSize)
}

func (h *BranchHandler) AdminGet(w http.ResponseWriter, r *http.Request) {
	b, err := h.getUC.Execute(mux.Vars(r)["branchId"])
	if err != nil {
		writeBranchMutationError(w, err, "Failed to get branch")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBranchResponse(b))
}

func (h *BranchHandler) AdminUpdate(w http.ResponseWriter, r *http.Request) {
	var input branchdomain.UpdateBranchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	input.ID = mux.Vars(r)["branchId"]
	input.Actor = branchdomain.Actor{IsAdmin: true}

	b, err := h.updateUC.Execute(input)
	if err != nil {
		writeBranchMutationError(w, err, "Failed to update branch")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBranchResponse(b))
}

func (h *BranchHandler) AdminDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.deleteUC.Execute(mux.Vars(r)["branchId"], branchdomain.Actor{IsAdmin: true}); err != nil {
		writeBranchMutationError(w, err, "Failed to delete branch")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "branch deleted"})
}

func (h *BranchHandler) AdminEnable(w http.ResponseWriter, r *http.Request) {
	var body EnableBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	b, err := h.enableUC.Execute(mux.Vars(r)["branchId"], body.Enabled, branchdomain.Actor{IsAdmin: true})
	if err != nil {
		writeBranchMutationError(w, err, "Failed to update branch")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBranchResponse(b))
}

func (h *BranchHandler) AdminRotateSecret(w http.ResponseWriter, r *http.Request) {
	res, err := h.rotateUC.Execute(mux.Vars(r)["branchId"], branchdomain.Actor{IsAdmin: true})
	if err != nil {
		writeBranchMutationError(w, err, "Failed to rotate branch secret")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.toBranchSecretResponse(res))
}
