package siptrunk

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	siptrunkdomain "vozko/domain/sip_trunk"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
)

type SIPTrunkHandler struct {
	createUC         siptrunkdomain.CreateUseCase
	updateUC         siptrunkdomain.UpdateUseCase
	deleteUC         siptrunkdomain.DeleteUseCase
	getUC            siptrunkdomain.GetUseCase
	listUC           siptrunkdomain.ListUseCase
	listByIDsUC      siptrunkdomain.ListByIDsUseCase
	listAccessibleUC siptrunkdomain.ListAccessibleUseCase
	enableUC         siptrunkdomain.EnableUseCase
	getStatusUC      siptrunkdomain.GetStatusUseCase
	assignOwnerUC    siptrunkdomain.AssignOwnerUseCase
}

func NewSIPTrunkHandler(
	createUC siptrunkdomain.CreateUseCase,
	updateUC siptrunkdomain.UpdateUseCase,
	deleteUC siptrunkdomain.DeleteUseCase,
	getUC siptrunkdomain.GetUseCase,
	listUC siptrunkdomain.ListUseCase,
	listByIDsUC siptrunkdomain.ListByIDsUseCase,
	listAccessibleUC siptrunkdomain.ListAccessibleUseCase,
	enableUC siptrunkdomain.EnableUseCase,
	getStatusUC siptrunkdomain.GetStatusUseCase,
	assignOwnerUC siptrunkdomain.AssignOwnerUseCase,
) *SIPTrunkHandler {
	return &SIPTrunkHandler{
		createUC:         createUC,
		updateUC:         updateUC,
		deleteUC:         deleteUC,
		getUC:            getUC,
		listUC:           listUC,
		listByIDsUC:      listByIDsUC,
		listAccessibleUC: listAccessibleUC,
		enableUC:         enableUC,
		getStatusUC:      getStatusUC,
		assignOwnerUC:    assignOwnerUC,
	}
}

func isValidationError(err error) bool {
	return errors.Is(err, siptrunkdomain.ErrTrunkNameRequired) ||
		errors.Is(err, siptrunkdomain.ErrTrunkNameTooLong) ||
		errors.Is(err, siptrunkdomain.ErrTrunkDescriptionTooLong) ||
		errors.Is(err, siptrunkdomain.ErrTrunkHostRequired) ||
		errors.Is(err, siptrunkdomain.ErrTrunkHostInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkHostBlocked) ||
		errors.Is(err, siptrunkdomain.ErrTrunkUsernameRequired) ||
		errors.Is(err, siptrunkdomain.ErrTrunkUsernameInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkPasswordRequired) ||
		errors.Is(err, siptrunkdomain.ErrTrunkPasswordInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkPortInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkTransportInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkTypeInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkPhoneRequired) ||
		errors.Is(err, siptrunkdomain.ErrTrunkPhoneInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkCodecInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkUserAgentInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkOutboundProxyInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkRegisterExpiryInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkRegisterRetryInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkMinSEInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkSessionTimerInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkSessionRefresherInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkBindPortInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkPublicAddressInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkStunServerInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkPTimeInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkDTMFPayloadInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkSRTPModeInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkSRTPRequiresSecureTransport) ||
		errors.Is(err, siptrunkdomain.ErrTrunkDialPrefixInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkDialStripInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkNumberFormatInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkRingingTimeoutInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkIdentityFieldInvalid) ||
		errors.Is(err, siptrunkdomain.ErrTrunkStunServerBlocked) ||
		errors.Is(err, siptrunkdomain.ErrTrunkTooManyEntries)
}

func writeTrunkMutationError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, siptrunkdomain.ErrTrunkNotFound):
		response.WriteError(w, http.StatusNotFound, "SIP trunk not found", nil)
	case errors.Is(err, siptrunkdomain.ErrTrunkForbidden), errors.Is(err, siptrunkdomain.ErrTrunkGlobalForbidden):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, siptrunkdomain.ErrTrunkLimitReached):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, siptrunkdomain.ErrTrunkOwnerWorkspaceRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case isValidationError(err):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, fallback, nil)
	}
}

func (h *SIPTrunkHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var body AdminCreateInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	ownerWS := strings.TrimSpace(body.WorkspaceID)
	if ownerWS == "" {
		response.WriteError(w, http.StatusBadRequest, siptrunkdomain.ErrTrunkOwnerWorkspaceRequired.Error(), nil)
		return
	}

	input := body.CreateTrunkInput
	input.WorkspaceID = ownerWS
	input.IsGloballyVisible = body.IsGloballyVisible
	input.Actor = siptrunkdomain.Actor{IsAdmin: true}

	trunk, err := h.createUC.Execute(input)
	if err != nil {
		writeTrunkMutationError(w, err, "Failed to create SIP trunk")
		return
	}

	if _, err := h.assignOwnerUC.Execute(trunk.ID, ownerWS, claims.UserID); err != nil {

		response.WriteSuccess(w, http.StatusCreated, toSIPTrunkResponse(trunk))
		return
	}
	refreshed, err := h.getUC.Execute(trunk.ID)
	if err != nil {
		response.WriteSuccess(w, http.StatusCreated, toSIPTrunkResponse(trunk))
		return
	}

	response.WriteSuccess(w, http.StatusCreated, toSIPTrunkResponse(refreshed))
}

func (h *SIPTrunkHandler) AssignOwner(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	trunkID := vars["trunkId"]

	var req AssignOwnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	trunk, err := h.assignOwnerUC.Execute(trunkID, req.WorkspaceID, claims.UserID)
	if err != nil {
		if errors.Is(err, siptrunkdomain.ErrTrunkNotFound) {
			response.WriteError(w, http.StatusNotFound, "SIP trunk not found", nil)
			return
		}
		if errors.Is(err, siptrunkdomain.ErrTrunkOwnerWorkspaceRequired) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to assign owner", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toSIPTrunkResponse(trunk))
}

// @Summary		Criar tronco SIP
// @Description	Cria um novo tronco SIP no workspace atual. As credenciais informadas são validadas e o usuário que cria fica registrado como proprietário do tronco.
// @Tags			Troncos SIP
// @Accept			json
// @Produce		json
// @Param			request	body		sip_trunk.CreateTrunkInput	true	"Dados do tronco a criar"
// @Success		201	{object}	sip_trunk.SIPTrunk
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks [post]
func (h *SIPTrunkHandler) CreateForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}

	var input siptrunkdomain.CreateTrunkInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	input.WorkspaceID = wsID
	input.IsGloballyVisible = false
	input.Actor = siptrunkdomain.Actor{WorkspaceID: wsID}

	trunk, err := h.createUC.Execute(input)
	if err != nil {
		writeTrunkMutationError(w, err, "Failed to create SIP trunk")
		return
	}

	if _, err := h.assignOwnerUC.Execute(trunk.ID, wsID, claims.UserID); err == nil {
		if refreshed, err2 := h.getUC.Execute(trunk.ID); err2 == nil {
			trunk = refreshed
		}
	}

	response.WriteSuccess(w, http.StatusCreated, toSIPTrunkResponse(trunk))
}

func (h *SIPTrunkHandler) Update(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]

	var body AdminUpdateInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	input := body.UpdateTrunkInput
	input.ID = trunkID
	input.IsGloballyVisible = body.IsGloballyVisible
	input.Actor = siptrunkdomain.Actor{IsAdmin: true}

	trunk, err := h.updateUC.Execute(input)
	if err != nil {
		writeTrunkMutationError(w, err, "Failed to update SIP trunk")
		return
	}

	response.WriteSuccess(w, http.StatusOK, toSIPTrunkResponse(trunk))
}

// @Summary		Atualizar tronco SIP
// @Description	Atualiza os dados de um tronco SIP do workspace. Todos os campos são opcionais; apenas os campos informados são alterados.
// @Tags			Troncos SIP
// @Accept			json
// @Produce		json
// @Param			trunkId	path		string						true	"ID do tronco SIP"
// @Param			request	body		sip_trunk.UpdateTrunkInput	true	"Campos do tronco a atualizar"
// @Success		200	{object}	sip_trunk.SIPTrunk
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{trunkId} [put]
func (h *SIPTrunkHandler) UpdateForWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}

	var input siptrunkdomain.UpdateTrunkInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	input.ID = trunkID
	input.IsGloballyVisible = nil
	input.Actor = siptrunkdomain.Actor{WorkspaceID: wsID}

	trunk, err := h.updateUC.Execute(input)
	if err != nil {
		writeTrunkMutationError(w, err, "Failed to update SIP trunk")
		return
	}

	response.WriteSuccess(w, http.StatusOK, toSIPTrunkResponse(trunk))
}

func (h *SIPTrunkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]

	if err := h.deleteUC.Execute(trunkID, siptrunkdomain.Actor{IsAdmin: true}); err != nil {
		writeTrunkMutationError(w, err, "Failed to delete SIP trunk")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "SIP trunk deleted"})
}

// @Summary		Remover tronco SIP
// @Description	Exclui um tronco SIP do workspace atual.
// @Tags			Troncos SIP
// @Produce		json
// @Param			trunkId	path	string	true	"ID do tronco SIP"
// @Success		200	{object}	map[string]string
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{trunkId} [delete]
func (h *SIPTrunkHandler) DeleteForWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}

	if err := h.deleteUC.Execute(trunkID, siptrunkdomain.Actor{WorkspaceID: wsID}); err != nil {
		writeTrunkMutationError(w, err, "Failed to delete SIP trunk")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "SIP trunk deleted"})
}

func (h *SIPTrunkHandler) canAccess(r *http.Request, trunk *siptrunkdomain.SIPTrunk) (bool, error) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return false, nil
	}
	if claims.Role == string(user.RoleAdmin) {
		return true, nil
	}
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		return false, nil
	}
	return trunk.IsOwnedBy(wsID) || trunk.IsGloballyVisible, nil
}

// @Summary		Obter tronco SIP
// @Description	Retorna os detalhes de um tronco SIP. Credenciais e dados sensíveis são omitidos quando o solicitante não é o proprietário do tronco.
// @Tags			Troncos SIP
// @Produce		json
// @Param			trunkId	path	string	true	"ID do tronco SIP"
// @Success		200	{object}	sip_trunk.SIPTrunk
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{trunkId} [get]
func (h *SIPTrunkHandler) Get(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	trunk, err := h.getUC.Execute(trunkID)
	if err != nil {
		if errors.Is(err, siptrunkdomain.ErrTrunkNotFound) {
			response.WriteError(w, http.StatusNotFound, "SIP trunk not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get SIP trunk", nil)
		return
	}

	ok, err := h.canAccess(r, trunk)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to check SIP trunk access", nil)
		return
	}
	if !ok {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this SIP trunk", nil)
		return
	}

	viewerWS := middleware.GetWorkspaceID(r)
	isAdmin := claims.Role == string(user.RoleAdmin)
	response.WriteSuccess(w, http.StatusOK, toSIPTrunkResponseFor(trunk, viewerWS, isAdmin))
}

// @Summary		Listar troncos SIP
// @Description	Retorna os troncos SIP acessíveis ao workspace (próprios e os de visibilidade global). Administradores recebem todos os troncos. Suporta paginação por page e pageSize.
// @Tags			Troncos SIP
// @Produce		json
// @Param			page		query	int	false	"Número da página (padrão 1)"
// @Param			pageSize	query	int	false	"Itens por página (padrão 20, máximo 200)"
// @Success		200	{object}	map[string]interface{}
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks [get]
func (h *SIPTrunkHandler) List(w http.ResponseWriter, r *http.Request) {
	page := 1
	pageSize := 20

	if p := r.URL.Query().Get("page"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			page = val
		}
	}
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if val, err := strconv.Atoi(ps); err == nil && val > 0 && val <= 200 {
			pageSize = val
		}
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if claims.Role == string(user.RoleAdmin) {
		trunks, total, err := h.listUC.Execute(page, pageSize)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, "Failed to list SIP trunks", nil)
			return
		}
		items := make([]sipTrunkResponse, len(trunks))
		for i, t := range trunks {
			items[i] = toSIPTrunkResponse(t)
		}
		response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"items":    items,
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
		})
		return
	}

	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}

	trunks, total, err := h.listAccessibleUC.Execute(wsID, nil, page, pageSize)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list SIP trunks", nil)
		return
	}
	items := make([]sipTrunkResponse, len(trunks))
	for i, t := range trunks {
		items[i] = toSIPTrunkResponseFor(t, wsID, false)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (h *SIPTrunkHandler) Enable(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]

	var input EnableTrunkRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	trunk, err := h.enableUC.Execute(trunkID, input.Enabled, siptrunkdomain.Actor{IsAdmin: true})
	if err != nil {
		writeTrunkMutationError(w, err, "Failed to update SIP trunk")
		return
	}

	response.WriteSuccess(w, http.StatusOK, toSIPTrunkResponse(trunk))
}

// @Summary		Ativar ou desativar tronco SIP
// @Description	Ativa ou desativa um tronco SIP do workspace conforme o campo enabled informado.
// @Tags			Troncos SIP
// @Accept			json
// @Produce		json
// @Param			trunkId	path		string				true	"ID do tronco SIP"
// @Param			request	body		EnableTrunkRequest	true	"Estado desejado do tronco"
// @Success		200	{object}	sip_trunk.SIPTrunk
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{trunkId}/enable [put]
func (h *SIPTrunkHandler) EnableForWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace context required", nil)
		return
	}

	var input EnableTrunkRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	trunk, err := h.enableUC.Execute(trunkID, input.Enabled, siptrunkdomain.Actor{WorkspaceID: wsID})
	if err != nil {
		writeTrunkMutationError(w, err, "Failed to update SIP trunk")
		return
	}

	response.WriteSuccess(w, http.StatusOK, toSIPTrunkResponse(trunk))
}

// @Summary		Listar codecs suportados
// @Description	Retorna o catálogo de codecs de áudio que um tronco pode selecionar, na ordem de preferência padrão do sistema.
// @Tags			Troncos SIP
// @Produce		json
// @Success		200	{array}	sip_trunk.CodecInfo
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/codecs [get]
func (h *SIPTrunkHandler) GetSupportedCodecs(w http.ResponseWriter, r *http.Request) {
	response.WriteSuccess(w, http.StatusOK, siptrunkdomain.OfferableCodecs())
}

// @Summary		Obter status do tronco SIP
// @Description	Retorna o status de registro atual de um tronco SIP. A mensagem de erro é omitida quando o solicitante não é o proprietário do tronco.
// @Tags			Troncos SIP
// @Produce		json
// @Param			trunkId	path	string	true	"ID do tronco SIP"
// @Success		200	{object}	map[string]interface{}
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{trunkId}/status [get]
func (h *SIPTrunkHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	trunkID := vars["trunkId"]

	trunk, err := h.getUC.Execute(trunkID)
	if err != nil {
		if errors.Is(err, siptrunkdomain.ErrTrunkNotFound) {
			response.WriteError(w, http.StatusNotFound, "SIP trunk not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get SIP trunk", nil)
		return
	}
	ok, err := h.canAccess(r, trunk)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to check SIP trunk access", nil)
		return
	}
	if !ok {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this SIP trunk", nil)
		return
	}

	status, err := h.getStatusUC.Execute(trunkID)
	if err != nil {
		if errors.Is(err, siptrunkdomain.ErrTrunkNotFound) {
			response.WriteError(w, http.StatusNotFound, "SIP trunk not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get SIP trunk status", nil)
		return
	}

	claims := middleware.GetClaims(r)
	statusError := status.Error
	isAdmin := claims != nil && claims.Role == string(user.RoleAdmin)
	if !isAdmin && !trunk.IsOwnedBy(middleware.GetWorkspaceID(r)) {
		statusError = ""
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"trunkId":   status.TrunkID,
		"status":    status.Status,
		"error":     statusError,
		"timestamp": status.Timestamp.Format("2006-01-02T15:04:05Z"),
	})
}
