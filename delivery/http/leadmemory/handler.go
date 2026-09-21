package leadmemory

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/actor"
	leadmemory "vozko/domain/lead_memory"
	"vozko/infra/http/middleware"
)

type LeadRefResolver interface {
	ResolveLeadRef(workspaceID, ref string) string
}

type LeadMemoryHandler struct {
	createUC leadmemory.CreateUseCase
	updateUC leadmemory.UpdateUseCase
	deleteUC leadmemory.DeleteUseCase
	listUC   leadmemory.ListUseCase
	leadRefs LeadRefResolver
}

func NewLeadMemoryHandler(
	createUC leadmemory.CreateUseCase,
	updateUC leadmemory.UpdateUseCase,
	deleteUC leadmemory.DeleteUseCase,
	listUC leadmemory.ListUseCase,
	leadRefs LeadRefResolver,
) *LeadMemoryHandler {
	return &LeadMemoryHandler{
		createUC: createUC,
		updateUC: updateUC,
		deleteUC: deleteUC,
		listUC:   listUC,
		leadRefs: leadRefs,
	}
}

func (h *LeadMemoryHandler) resolveLeadID(workspaceID, ref string) (string, bool) {
	if h.leadRefs == nil {
		return ref, true
	}
	if resolved := h.leadRefs.ResolveLeadRef(workspaceID, ref); resolved != "" {
		return resolved, true
	}
	return ref, false
}

// @Summary		Listar memórias de um lead
// @Description	Lista as memórias registradas sobre um lead (fatos salvos pela IA e por operadores), da mais recente para a mais antiga.
// @Tags			Memórias do Lead
// @Produce		json
// @Param			id			path		string	true	"ID do lead"
// @Param			category	query		string	false	"Filtro de categoria: personal | preference | deal | objection | commitment | event | other"
// @Param			limit		query		int		false	"Máximo de itens (padrão 50)"
// @Param			offset		query		int		false	"Deslocamento de paginação"
// @Success		200	{object}	LeadMemoryListResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/memories [get]
func (h *LeadMemoryHandler) List(w http.ResponseWriter, r *http.Request) {
	query := leadmemory.ListQuery{}
	if raw := strings.TrimSpace(r.URL.Query().Get("category")); raw != "" {
		cat := leadmemory.Category(strings.ToLower(raw))
		if !cat.Valid() {
			response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_category", "categoria inválida", nil)
			return
		}
		query.Category = &cat
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			query.Limit = v
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			query.Offset = v
		}
	}

	workspaceID := middleware.GetWorkspaceID(r)
	leadID, linked := h.resolveLeadID(workspaceID, mux.Vars(r)["id"])
	if !linked {
		response.WriteSuccess(w, http.StatusOK, LeadMemoryListResponse{
			Memories:   []LeadMemoryResponse{},
			Total:      0,
			LeadLinked: false,
		})
		return
	}

	result, err := h.listUC.Execute(r.Context(), leadmemory.ListInput{
		WorkspaceID: workspaceID,
		LeadID:      leadID,
		Query:       query,
	})
	if err != nil {
		h.writeDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, LeadMemoryListResponse{
		Memories:   toResponses(result.Items),
		Total:      result.Total,
		LeadLinked: true,
	})
}

// @Summary		Registrar uma memória sobre um lead
// @Description	Registra um fato sobre o lead. Um conteúdo equivalente a uma memória já existente devolve a memória existente (200) em vez de criar uma duplicata.
// @Tags			Memórias do Lead
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do lead"
// @Param			request	body		CreateLeadMemoryRequest	true	"Fato e categoria"
// @Success		201	{object}	LeadMemoryEnvelope
// @Success		200	{object}	LeadMemoryEnvelope	"Uma memória equivalente já existia; nada novo foi criado"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse	"Limite de memórias do lead atingido, ou conversa sem lead vinculado (lead_not_linked)"
// @Security		BearerAuth
// @Router			/leads/{id}/memories [post]
func (h *LeadMemoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateLeadMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"content":  "string (obrigatório, até 600 caracteres)",
			"category": "personal | preference | deal | objection | commitment | event | other",
		})
		return
	}

	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	leadID, linked := h.resolveLeadID(workspaceID, mux.Vars(r)["id"])
	if !linked {
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "lead_not_linked",
			"Esta conversa ainda não está vinculada a um lead; não é possível salvar memórias.", nil)
		return
	}

	result, err := h.createUC.Execute(r.Context(), leadmemory.CreateInput{
		WorkspaceID: workspaceID,
		LeadID:      leadID,
		Content:     req.Content,
		Category:    leadmemory.Category(strings.ToLower(strings.TrimSpace(req.Category))),
		Actor:       leadmemory.WriteActor{Kind: actor.KindHuman, ID: claims.UserID},
	})
	if err != nil {
		h.writeDomainError(w, err)
		return
	}

	status := http.StatusCreated
	if result.Deduplicated {
		status = http.StatusOK
	}
	response.WriteSuccess(w, status, LeadMemoryEnvelope{Memory: toResponse(result.Memory, "")})
}

// @Summary		Editar uma memória de lead
// @Description	Altera o conteúdo e/ou a categoria de uma memória. A autoria passa a ser de quem editou; o histórico fica na linha do tempo da conversa.
// @Tags			Memórias do Lead
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID da memória"
// @Param			request	body		UpdateLeadMemoryRequest	true	"Novos valores (campos omitidos são mantidos)"
// @Success		200	{object}	LeadMemoryEnvelope
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse	"O novo conteúdo duplicaria outra memória"
// @Security		BearerAuth
// @Router			/lead-memories/{id} [patch]
func (h *LeadMemoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateLeadMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"content":  "string (opcional)",
			"category": "personal | preference | deal | objection | commitment | event | other (opcional)",
		})
		return
	}

	in := leadmemory.UpdateInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		MemoryRef:   mux.Vars(r)["id"],
		Actor:       leadmemory.WriteActor{Kind: actor.KindHuman, ID: middleware.GetClaims(r).UserID},
	}
	if req.Content != nil {
		in.Content = *req.Content
	}
	if req.Category != nil {
		in.Category = leadmemory.Category(strings.ToLower(strings.TrimSpace(*req.Category)))
	}

	m, err := h.updateUC.Execute(r.Context(), in)
	if err != nil {
		h.writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, LeadMemoryEnvelope{Memory: toResponse(m, "")})
}

// @Summary		Apagar uma memória de lead
// @Description	Apaga (soft delete) uma memória. Ela some do contexto da IA e do painel; a exclusão definitiva acompanha o ciclo de vida do lead.
// @Tags			Memórias do Lead
// @Produce		json
// @Param			id	path	string	true	"ID da memória"
// @Success		204	"Memória apagada"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/lead-memories/{id} [delete]
func (h *LeadMemoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.deleteUC.Execute(r.Context(), leadmemory.DeleteInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		MemoryRef:   mux.Vars(r)["id"],
		Actor:       leadmemory.WriteActor{Kind: actor.KindHuman, ID: middleware.GetClaims(r).UserID},
	})
	if err != nil {
		h.writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *LeadMemoryHandler) writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, leadmemory.ErrNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, leadmemory.ErrDuplicate):
		response.WriteErrorWithCode(w, http.StatusConflict, "memory_duplicate", err.Error(), nil)
	case errors.Is(err, leadmemory.ErrLimitReached):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "memory_limit", err.Error(), nil)
	case errors.Is(err, leadmemory.ErrAmbiguousID):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "ambiguous_id", err.Error(), nil)
	case errors.Is(err, leadmemory.ErrContentRequired),
		errors.Is(err, leadmemory.ErrContentTooLong),
		errors.Is(err, leadmemory.ErrInvalidCategory),
		errors.Is(err, leadmemory.ErrLeadRequired),
		errors.Is(err, leadmemory.ErrWorkspaceRequired),
		errors.Is(err, leadmemory.ErrActorRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
