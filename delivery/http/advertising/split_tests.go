package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

// @Summary		Testes A/B da conta
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{array}		advertising.SplitTest
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/tests [get]
func (h *Handler) SplitTests(w http.ResponseWriter, r *http.Request) {
	tests, err := h.d.SplitTests.List(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list split tests")
		return
	}
	response.WriteSuccess(w, http.StatusOK, nonNil(tests))
}

// @Summary		Criar teste A/B
// @Description	Divide o público entre 2 a 5 campanhas ou conjuntos da mesma conta, de 1 a 30 dias, com confiança de 65, 80, 90 ou 95 por cento.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		advertising.SplitTest	true	"teste"
// @Success		201		{object}	MetaIDResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/tests [post]
func (h *Handler) CreateSplitTest(w http.ResponseWriter, r *http.Request) {
	var test advertising.SplitTest
	if !decodeJSON(w, r, &test) {
		return
	}
	id, err := h.d.SplitTests.Create(r.Context(), workspaceOf(r), test)
	if err != nil {
		writeError(w, err, "Failed to create the split test")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, MetaIDResponse{MetaID: id})
}
