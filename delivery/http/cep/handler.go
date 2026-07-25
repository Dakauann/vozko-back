package cep

import (
	"net/http"

	"vozko/delivery/http/response"
	cepdomain "vozko/domain/cep"
)

type CEPHandler struct {
	search cepdomain.CEPSearchUseCase
}

func NewCEPHandler(search cepdomain.CEPSearchUseCase) *CEPHandler {
	return &CEPHandler{search: search}
}

// @Summary		Consultar um CEP
// @Description	Retorna o endereço (logradouro, bairro, cidade e UF) correspondente a um CEP brasileiro. Utilizado no cadastro e na conferência de endereços de entrega e cobrança.
// @Tags			CEP
// @Produce		json
// @Param			cep	query		string	true	"CEP a consultar (somente dígitos ou no formato 00000-000)"
// @Success		200	{object}	CEPResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Router			/cep/search [get]
func (h *CEPHandler) Search(w http.ResponseWriter, r *http.Request) {
	req := CEPSearchRequest{CEP: r.URL.Query().Get("cep")}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	info, err := h.search.Execute(req.CEP)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "CEP not found", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, CEPResponse{
		Cep:        info.Cep,
		Logradouro: info.Logradouro,
		Complement: info.Complement,
		Bairro:     info.Bairro,
		Localidade: info.Localidade,
		Uf:         info.Uf,
		Erro:       info.Erro,
	})
}
