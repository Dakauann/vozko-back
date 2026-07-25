package waba

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/shared"
	wabadomain "vozko/domain/whatsapp/waba"
)

type WABAHandler struct {
	listUseCase wabadomain.ListUseCase
	getUseCase  wabadomain.GetUseCase
}

func NewWABAHandler(
	listUC wabadomain.ListUseCase,
	getUC wabadomain.GetUseCase,
) *WABAHandler {
	return &WABAHandler{
		listUseCase: listUC,
		getUseCase:  getUC,
	}
}

// @Summary		Listar contas do WhatsApp Business
// @Description	Retorna a lista paginada de contas do WhatsApp Business (WABA) vinculadas ao workspace.
// @Tags			Contas do WhatsApp Business
// @Produce		json
// @Param			page		query		int	false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int	false	"Quantidade de itens por página"
// @Success		200	{object}	WABAListResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/wabas [get]
func (h *WABAHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))

	input := wabadomain.ListInput{
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{
				Page:     page,
				PageSize: pageSize,
			},
		},
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list WhatsApp Business Accounts", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, toWABAResponses(result.Items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Consultar uma conta do WhatsApp Business
// @Description	Retorna os detalhes de uma conta do WhatsApp Business (WABA) a partir do seu identificador.
// @Tags			Contas do WhatsApp Business
// @Produce		json
// @Param			id	path	string	true	"Identificador da conta (WABA)"
// @Success		200	{object}	WABAResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/wabas/{id} [get]
func (h *WABAHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "WABA ID is required", nil)
		return
	}

	account, err := h.getUseCase.Execute(id)
	if err != nil {
		if errors.Is(err, wabadomain.ErrWABANotFound) {
			response.WriteError(w, http.StatusNotFound, "WhatsApp Business Account not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get WhatsApp Business Account", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toWABAResponse(account))
}
