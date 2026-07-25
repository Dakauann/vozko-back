package workspaceaddon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	balance_domain "vozko/domain/balance"
	workspaceaddondomain "vozko/domain/workspace/workspace_addon"
	"vozko/infra/http/middleware"
)

type WorkspaceAddonHandler struct {
	createDefinition  workspaceaddondomain.CreateAddonDefinitionUseCase
	updateDefinition  workspaceaddondomain.UpdateAddonDefinitionUseCase
	archiveDefinition workspaceaddondomain.ArchiveAddonDefinitionUseCase
	listDefinitions   workspaceaddondomain.ListAddonDefinitionsUseCase
	getDefinition     workspaceaddondomain.GetAddonDefinitionUseCase
	listAvailable     workspaceaddondomain.ListAvailableAddonsUseCase
	purchase          workspaceaddondomain.PurchaseAddonUseCase
	previewPurchase   workspaceaddondomain.PreviewAddonPurchaseUseCase
	cancel            workspaceaddondomain.CancelAddonSubscriptionUseCase
	listWorkspace     workspaceaddondomain.ListWorkspaceAddonsUseCase
	getEntitlements   workspaceaddondomain.GetWorkspaceEntitlementsUseCase
}

func NewWorkspaceAddonHandler(
	createDefinition workspaceaddondomain.CreateAddonDefinitionUseCase,
	updateDefinition workspaceaddondomain.UpdateAddonDefinitionUseCase,
	archiveDefinition workspaceaddondomain.ArchiveAddonDefinitionUseCase,
	listDefinitions workspaceaddondomain.ListAddonDefinitionsUseCase,
	getDefinition workspaceaddondomain.GetAddonDefinitionUseCase,
	listAvailable workspaceaddondomain.ListAvailableAddonsUseCase,
	purchase workspaceaddondomain.PurchaseAddonUseCase,
	previewPurchase workspaceaddondomain.PreviewAddonPurchaseUseCase,
	cancel workspaceaddondomain.CancelAddonSubscriptionUseCase,
	listWorkspace workspaceaddondomain.ListWorkspaceAddonsUseCase,
	getEntitlements workspaceaddondomain.GetWorkspaceEntitlementsUseCase,
) *WorkspaceAddonHandler {
	return &WorkspaceAddonHandler{
		createDefinition:  createDefinition,
		updateDefinition:  updateDefinition,
		archiveDefinition: archiveDefinition,
		listDefinitions:   listDefinitions,
		getDefinition:     getDefinition,
		listAvailable:     listAvailable,
		purchase:          purchase,
		previewPurchase:   previewPurchase,
		cancel:            cancel,
		listWorkspace:     listWorkspace,
		getEntitlements:   getEntitlements,
	}
}

func (h *WorkspaceAddonHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	includeArchived := false
	if raw := r.URL.Query().Get("includeArchived"); raw != "" {
		parsed, _ := strconv.ParseBool(raw)
		includeArchived = parsed
	}
	defs, err := h.listDefinitions.Execute(includeArchived)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, defs)
}

func (h *WorkspaceAddonHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["addonId"]
	def, err := h.getDefinition.Execute(id)
	if err != nil {
		h.writeDefinitionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, def)
}

func (h *WorkspaceAddonHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input workspaceaddondomain.AddonDefinitionMutationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	def, err := h.createDefinition.Execute(input)
	if err != nil {
		h.writeDefinitionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, def)
}

func (h *WorkspaceAddonHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["addonId"]
	var input workspaceaddondomain.AddonDefinitionMutationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	def, err := h.updateDefinition.Execute(id, input)
	if err != nil {
		h.writeDefinitionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, def)
}

func (h *WorkspaceAddonHandler) Archive(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["addonId"]
	if err := h.archiveDefinition.Execute(id); err != nil {
		h.writeDefinitionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "archived"})
}

// @Summary		Listar addons disponíveis
// @Description	Retorna o catálogo de addons disponíveis para contratação pelo workspace, exibindo apenas o preço final ao cliente. O custo interno nunca é exposto.
// @Tags			Addons
// @Produce		json
// @Param			workspaceId	path	string	true	"ID do workspace"
// @Success		200	{array}		customerAddonDefinition
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/addons/available [get]
func (h *WorkspaceAddonHandler) ListAvailable(w http.ResponseWriter, r *http.Request) {
	defs, err := h.listAvailable.Execute(h.workspaceID(r))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toCustomerAddonDefinitions(defs))
}

// @Summary		Listar addons do workspace
// @Description	Retorna as assinaturas de addons do workspace, incluindo as ativas e as canceladas.
// @Tags			Addons
// @Produce		json
// @Param			workspaceId	path	string	true	"ID do workspace"
// @Success		200	{array}		workspace_addon.AddonSubscription
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/addons [get]
func (h *WorkspaceAddonHandler) ListWorkspace(w http.ResponseWriter, r *http.Request) {
	subs, err := h.listWorkspace.Execute(h.workspaceID(r))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, subs)
}

// @Summary		Consultar direitos do workspace
// @Description	Retorna os direitos (entitlements) consolidados do workspace, somando a base do plano às unidades dos addons ativos.
// @Tags			Addons
// @Produce		json
// @Param			workspaceId	path	string	true	"ID do workspace"
// @Success		200	{array}		workspace_addon.WorkspaceEntitlement
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/entitlements [get]
func (h *WorkspaceAddonHandler) GetEntitlements(w http.ResponseWriter, r *http.Request) {
	ents, err := h.getEntitlements.Execute(h.workspaceID(r))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ents)
}

// @Summary		Contratar addon
// @Description	Contrata um addon para o workspace, cobra o valor proporcional de ativação do saldo e cria a assinatura recorrente.
// @Tags			Addons
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string								true	"ID do workspace"
// @Param			request		body		workspace_addon.PurchaseAddonInput	true	"Dados da contratação do addon"
// @Success		201	{object}	workspace_addon.AddonSubscription
// @Failure		400	{object}	response.ErrorResponse
// @Failure		402	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/addons [post]
func (h *WorkspaceAddonHandler) Purchase(w http.ResponseWriter, r *http.Request) {
	var input workspaceaddondomain.PurchaseAddonInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	sub, err := h.purchase.Execute(h.workspaceID(r), input)
	if err != nil {
		h.writePurchaseError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, sub)
}

// @Summary		Simular contratação de addon
// @Description	Calcula o valor exato a pagar agora e o valor recorrente de uma contratação de addon. Não cobra nem persiste nada.
// @Tags			Addons
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string								true	"ID do workspace"
// @Param			request		body		workspace_addon.PurchaseAddonInput	true	"Dados da simulação do addon"
// @Success		200	{object}	workspace_addon.AddonPurchasePreview
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/addons/preview [post]
func (h *WorkspaceAddonHandler) Preview(w http.ResponseWriter, r *http.Request) {
	var input workspaceaddondomain.PurchaseAddonInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	preview, err := h.previewPurchase.Execute(h.workspaceID(r), input)
	if err != nil {
		h.writePurchaseError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, preview)
}

// @Summary		Cancelar assinatura de addon
// @Description	Cancela uma assinatura de addon do workspace a partir do seu identificador.
// @Tags			Addons
// @Produce		json
// @Param			workspaceId			path	string	true	"ID do workspace"
// @Param			addonSubscriptionId	path	string	true	"ID da assinatura do addon"
// @Success		200	{object}	workspace_addon.AddonSubscription
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/addons/{addonSubscriptionId}/cancel [post]
func (h *WorkspaceAddonHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	subID := mux.Vars(r)["addonSubscriptionId"]
	sub, err := h.cancel.Execute(h.workspaceID(r), subID)
	if err != nil {
		if errors.Is(err, workspaceaddondomain.ErrAddonSubscriptionNotFound) {
			response.WriteError(w, http.StatusNotFound, "Addon subscription not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, sub)
}

func (h *WorkspaceAddonHandler) workspaceID(r *http.Request) string {
	if id := mux.Vars(r)["workspaceId"]; id != "" {
		return id
	}
	return middleware.GetWorkspaceID(r)
}

func (h *WorkspaceAddonHandler) writeDefinitionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspaceaddondomain.ErrAddonNotFound):
		response.WriteError(w, http.StatusNotFound, "Addon not found", nil)
	case errors.Is(err, workspaceaddondomain.ErrAddonKeyExists),
		errors.Is(err, workspaceaddondomain.ErrAddonArchived):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspaceaddondomain.ErrInvalidAddonKey),
		errors.Is(err, workspaceaddondomain.ErrInvalidAddonName),
		errors.Is(err, workspaceaddondomain.ErrInvalidEntitlementKind),
		errors.Is(err, workspaceaddondomain.ErrInvalidUnitsPerQuantity),
		errors.Is(err, workspaceaddondomain.ErrInvalidAddonPrice):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func (h *WorkspaceAddonHandler) writePurchaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspaceaddondomain.ErrAddonNotFound):
		response.WriteError(w, http.StatusNotFound, "Addon not found", nil)
	case errors.Is(err, workspaceaddondomain.ErrAddonInactive):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, balance_domain.ErrInsufficientBalance):
		response.WriteError(w, http.StatusPaymentRequired, "insufficient_balance", nil)
	case errors.Is(err, workspaceaddondomain.ErrInvalidQuantity),
		errors.Is(err, workspaceaddondomain.ErrInvalidAddonSubscription):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
