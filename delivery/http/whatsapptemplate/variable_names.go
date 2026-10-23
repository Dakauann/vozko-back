package whatsapptemplate

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"vozko/delivery/http/response"
	whatsapptemplatedomain "vozko/domain/whatsapp/template"
)

type VariableNamesRequest struct {
	Names []string `json:"names" example:"primeiro_nome,pedido"`
}

type VariableNamesResponse struct {
	Valid bool `json:"valid" example:"true"`
}

// @Summary		Conferir nomes de variáveis
// @Description	Confere os nomes das variáveis nomeadas de um modelo pela mesma regra da criação e da edição: só letras minúsculas sem acento, números e sublinhado, começando por letra (como `primeiro_nome` ou `pedido2`). Responde `valid: true` ou recusa o primeiro nome inválido com 400 `template_variable_name_invalid` e o nome em `expected.variable`. Exige a ação `create` de modelos de WhatsApp.
// @Tags			Modelos de WhatsApp
// @Accept			json
// @Produce		json
// @Param			request	body		VariableNamesRequest	true	"Nomes a conferir"
// @Success		200	{object}	VariableNamesResponse
// @Failure		400	{object}	response.ErrorResponse	"template_variable_name_invalid"
// @Security		BearerAuth
// @Router			/whatsapp/templates/variable-names/check [post]
func (h *WhatsAppTemplateHandler) CheckVariableNames(w http.ResponseWriter, r *http.Request) {
	var req VariableNamesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	if err := whatsapptemplatedomain.CheckParameterNames(req.Names); err != nil {
		writeTemplateError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, VariableNamesResponse{Valid: true})
}

func validationExpectation(err error) map[string]string {
	var named *whatsapptemplatedomain.ParameterNameError
	if errors.As(err, &named) {
		return map[string]string{"variable": named.Name}
	}
	if button, ok := whatsapptemplatedomain.TrackedButtonIndex(err); ok {
		return map[string]string{"buttonIndex": strconv.Itoa(button)}
	}
	return nil
}
