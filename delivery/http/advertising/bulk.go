package advertisinghttp

import (
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type BulkTargetsRequest struct {
	MetaIDs []string `json:"metaIds"`
}

type BulkEditRequest struct {
	MetaIDs []string               `json:"metaIds"`
	Change  advertising.BulkChange `json:"change"`
}

type BulkApplyRequest struct {
	MetaIDs []string               `json:"metaIds"`
	Edit    advertising.ObjectEdit `json:"edit"`
}

type BulkErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type BulkResultResponse struct {
	MetaID string             `json:"metaId"`
	OK     bool               `json:"ok"`
	Object *RowResponse       `json:"object,omitempty"`
	Error  *BulkErrorResponse `json:"error,omitempty"`
}

type BulkResponse struct {
	Results []BulkResultResponse `json:"results"`
}

// @Summary		Ligar vários itens
// @Description	Liga até 50 campanhas, conjuntos ou anúncios, um por vez, com as mesmas regras de POST /ads/objects/{metaId}/activate. Cada item volta com o próprio resultado.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		BulkTargetsRequest	true	"itens"
// @Success		200		{object}	BulkResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/bulk/activate [post]
func (h *Handler) BulkActivate(w http.ResponseWriter, r *http.Request) { h.bulkStatus(w, r, true) }

// @Summary		Desligar vários itens
// @Description	Desliga até 50 campanhas, conjuntos ou anúncios, um por vez. Cada item volta com o próprio resultado.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		BulkTargetsRequest	true	"itens"
// @Success		200		{object}	BulkResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/bulk/pause [post]
func (h *Handler) BulkPause(w http.ResponseWriter, r *http.Request) { h.bulkStatus(w, r, false) }

func (h *Handler) bulkStatus(w http.ResponseWriter, r *http.Request, on bool) {
	var req BulkTargetsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	results, err := h.d.Bulk.SetStatus(r.Context(), workspaceOf(r), req.MetaIDs, on)
	h.writeBulk(w, results, err, "Failed to change the status of the selected items")
}

// @Summary		Editar um campo em vários itens
// @Description	Como o menu Editar do Gerenciador de Anúncios: muda um campo (name, primaryText, headline, description, link) em até 50 itens, definindo um valor (mode set) ou localizando e substituindo (mode replace, com matchCase). Textos do criativo só valem para anúncios que não usam publicação existente. Itens sem mudança voltam com nothing_to_change.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		BulkEditRequest	true	"itens e mudança"
// @Success		200		{object}	BulkResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/bulk/edit [post]
func (h *Handler) BulkEdit(w http.ResponseWriter, r *http.Request) {
	var req BulkEditRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	results, err := h.d.Bulk.Edit(r.Context(), workspaceOf(r), req.MetaIDs, req.Change)
	h.writeBulk(w, results, err, "Failed to edit the selected items")
}

// @Summary		Aplicar a mesma edição em vários itens
// @Description	Edição em massa do editor ("Valores mistos"): aplica o mesmo ObjectEdit em até 50 itens, um por vez, com as regras de PATCH /ads/objects/{metaId}. Só os campos enviados mudam. Um orçamento sem kind mantém o tipo atual de cada item (diário ou total). Cada item volta com o próprio resultado.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		BulkApplyRequest	true	"itens e edição"
// @Success		200		{object}	BulkResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/bulk/apply [post]
func (h *Handler) BulkApply(w http.ResponseWriter, r *http.Request) {
	var req BulkApplyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	results, err := h.d.Bulk.Apply(r.Context(), workspaceOf(r), req.MetaIDs, req.Edit)
	h.writeBulk(w, results, err, "Failed to apply the edit to the selected items")
}

func (h *Handler) writeBulk(w http.ResponseWriter, results []adsuc.BulkResult, err error, fallback string) {
	if err != nil {
		writeError(w, err, fallback)
		return
	}
	out := make([]BulkResultResponse, 0, len(results))
	for _, result := range results {
		out = append(out, h.presentBulkResult(result, fallback))
	}
	response.WriteSuccess(w, http.StatusOK, BulkResponse{Results: out})
}

func (h *Handler) presentBulkResult(result adsuc.BulkResult, fallback string) BulkResultResponse {
	if result.Err == nil {
		row := presentObject(result.Object, h.now())
		return BulkResultResponse{MetaID: result.MetaID, OK: true, Object: &row}
	}
	described, ok := describeError(result.Err)
	if !ok {
		log.Printf("[ads] %s (%s): %v", fallback, result.MetaID, result.Err)
		described = describedError{code: "failed", message: fallback}
	}
	return BulkResultResponse{MetaID: result.MetaID, Error: &BulkErrorResponse{Code: described.code, Message: described.message}}
}
