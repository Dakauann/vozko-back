package cep

import (
	"errors"
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
// @Description	Retorna o endereço (logradouro, bairro, cidade, UF e código IBGE do município) correspondente a um CEP brasileiro. Utilizado no cadastro e na conferência de endereços de entrega, cobrança e de leads. O CEP aceita somente dígitos, hífen, ponto e espaços. Quando a consulta externa está indisponível e o CEP ainda não está em cache, responde pela base de referência do IBGE (CNEFE 2022) carregada no servidor, se ela conhece o CEP: cidade, UF e código IBGE sempre, logradouro e bairro só quando o CEP tem um só (nada é inventado, e essa resposta não entra no cache). Sem isso, responde 503.
// @Tags			CEP
// @Produce		json
// @Param			cep	query		string	true	"CEP a consultar (somente dígitos ou no formato 00000-000)"
// @Success		200	{object}	CEPResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Router			/cep/search [get]
func (h *CEPHandler) Search(w http.ResponseWriter, r *http.Request) {
	req := CEPSearchRequest{CEP: r.URL.Query().Get("cep")}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	info, err := h.search.Execute(r.Context(), req.CEP)
	switch {
	case errors.Is(err, cepdomain.ErrInvalidCEP):
		response.WriteValidationError(w, map[string]string{"cep": "must have 8 digits"})
		return
	case errors.Is(err, cepdomain.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, "CEP not found", nil)
		return
	case errors.Is(err, cepdomain.ErrUnavailable), errors.Is(err, cepdomain.ErrLookupNotConfigured):
		response.WriteError(w, http.StatusServiceUnavailable, "CEP lookup unavailable", nil)
		return
	case err != nil:
		response.WriteError(w, http.StatusInternalServerError, "CEP lookup failed", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, CEPResponse{
		Cep:        info.Cep,
		Logradouro: info.Logradouro,
		Complement: info.Complement,
		Bairro:     info.Bairro,
		Localidade: info.Localidade,
		Uf:         info.Uf,
		IBGE:       info.IBGE,
	})
}
