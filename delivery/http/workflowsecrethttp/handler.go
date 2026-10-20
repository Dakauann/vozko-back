package workflowsecrethttp

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/workflowsecret"
	"vozko/infra/http/middleware"
)

const maxPutBodyBytes = 4*workflowsecret.MaxValueBytes + 1024

type Handler struct {
	secrets workflowsecret.UseCases
}

func NewHandler(secrets workflowsecret.UseCases) *Handler {
	return &Handler{secrets: secrets}
}

// @Summary		Listar os segredos do workspace
// @Description	Lista os segredos que os fluxos do workspace podem usar, só com nome, data da última alteração e quem alterou. O valor nunca é retornado por nenhuma rota. Nos fluxos, use {{secret.<nome>}} na URL, nos cabeçalhos ou no corpo de um nó de requisição HTTP, e secrets.get('<nome>') no nó de código. Exige `workflows:read`.
// @Tags			workflows
// @Produce		json
// @Success		200	{object}	SecretListResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workflow-secrets [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.secrets.List(r.Context(), middleware.GetWorkspaceID(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := SecretListResponse{Secrets: make([]SecretResponse, len(summaries))}
	for i, s := range summaries {
		out.Secrets[i] = toResponse(s)
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Criar ou substituir um segredo do workspace
// @Description	Grava o valor do segredo com o nome informado, criptografado. Se o nome já existe, o valor é substituído e quem criou é mantido. O nome usa só letras minúsculas, dígitos e _, com 1 a 64 caracteres; o valor tem até 8 KB. O valor é só de escrita: a resposta traz o nome, a data e quem alterou, nunca o valor. Nos fluxos o valor só é lido quando um nó de requisição HTTP monta a chamada ou um nó de código chama secrets.get, e nunca é gravado no estado, nas etapas da execução nem nos logs. Exige `workflows:update`.
// @Tags			workflows
// @Accept			json
// @Produce		json
// @Param			name	path		string				true	"Nome do segredo (ex.: unifecaf_senha)"
// @Param			request	body		PutSecretRequest	true	"Valor do segredo"
// @Success		200	{object}	SecretResponse
// @Failure		400	{object}	response.ErrorResponse	"invalid_body, invalid_name, value_required ou value_too_long"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workflow-secrets/{name} [put]
func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	var req PutSecretRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxPutBodyBytes)).Decode(&req); err != nil {
		response.WriteRefusal(w, refusalSecretInvalidBody)
		return
	}
	summary, err := h.secrets.Put(r.Context(), middleware.GetWorkspaceID(r), userID(r), mux.Vars(r)["name"], req.Value)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(summary))
}

// @Summary		Remover um segredo do workspace
// @Description	Apaga o segredo de vez. Fluxos que ainda usam {{secret.<nome>}} passam a falhar nesse nó com um erro que cita o nome, sem enviar a requisição. Exige `workflows:update`.
// @Tags			workflows
// @Param			name	path	string	true	"Nome do segredo"
// @Success		204
// @Failure		400	{object}	response.ErrorResponse	"invalid_name"
// @Failure		404	{object}	response.ErrorResponse	"not_found"
// @Security		BearerAuth
// @Router			/workflow-secrets/{name} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.secrets.Delete(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["name"]); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toResponse(s workflowsecret.Summary) SecretResponse {
	return SecretResponse{Name: s.Name, UpdatedAt: s.UpdatedAt, UpdatedBy: s.UpdatedBy}
}

func userID(r *http.Request) string {
	if claims := middleware.GetClaims(r); claims != nil {
		return claims.UserID
	}
	return ""
}

func secretRefusal(status int, code, message string) response.Refusal {
	return response.Refusal{Status: status, Code: code, Message: message}
}

var (
	refusalSecretInvalidBody = secretRefusal(http.StatusBadRequest, "invalid_body", "Corpo inválido: envie {\"value\": \"...\"}")
	refusalSecretFailed      = secretRefusal(http.StatusInternalServerError, "secret_failed", "Erro ao processar o segredo")
)

var secretRequestRefusals = []response.Refusal{refusalSecretInvalidBody, refusalSecretFailed}

var secretRefusals = []response.ErrorRefusal{
	{Err: workflowsecret.ErrInvalidName, Refusal: secretRefusal(http.StatusBadRequest, "invalid_name", "O nome usa só letras minúsculas, dígitos e _, com 1 a 64 caracteres")},
	{Err: workflowsecret.ErrValueRequired, Refusal: secretRefusal(http.StatusBadRequest, "value_required", "Informe o valor do segredo")},
	{Err: workflowsecret.ErrValueTooLong, Refusal: secretRefusal(http.StatusBadRequest, "value_too_long", "O valor tem no máximo 8 KB")},
	{Err: workflowsecret.ErrWorkspaceRequired, Refusal: secretRefusal(http.StatusBadRequest, "workspace_required", "Selecione um workspace")},
	{Err: workflowsecret.ErrNotFound, Refusal: secretRefusal(http.StatusNotFound, "not_found", "Segredo não encontrado")},
}

func writeDomainError(w http.ResponseWriter, err error) {
	if !response.WriteErrorRefusal(w, err, secretRefusals) {
		response.WriteRefusal(w, refusalSecretFailed)
	}
}
