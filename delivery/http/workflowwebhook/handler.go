package workflowwebhook

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"

	"github.com/gorilla/mux"
)

// Handler serves the workflow webhook trigger: the workspace-scoped config
// lifecycle (/workflows/{id}/webhook) and the public receiver
// (/webhooks/workflow/{token}) that starts a run.
type Handler struct {
	configUC  workflow_usecase.WorkflowWebhookUseCase
	triggerUC workflow_usecase.HandleWebhookTriggerUseCase
}

func NewHandler(
	configUC workflow_usecase.WorkflowWebhookUseCase,
	triggerUC workflow_usecase.HandleWebhookTriggerUseCase,
) *Handler {
	return &Handler{configUC: configUC, triggerUC: triggerUC}
}

// GetWorkflowWebhook godoc
// @Summary Obter a configuração do gatilho de webhook de um workflow
// @Description Retorna a URL pública e o modo de autenticação do webhook do workflow. O segredo não é retornado aqui; use a rotação para gerar um novo.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Success 200 {object} webhookConfigResponse
// @Failure 404 {object} response.ErrorResponse "Webhook não configurado"
// @Router /workflows/{id}/webhook [get]
func (h *Handler) GetWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	wh, err := h.configUC.Get(middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		handleWebhookConfigError(w, err)
		return
	}
	if wh == nil {
		response.WriteError(w, http.StatusNotFound, "Webhook não configurado", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toWebhookResponse(wh))
}

// CreateWorkflowWebhook godoc
// @Summary Criar o gatilho de webhook de um workflow
// @Description Cria a URL pública que sistemas externos usam para iniciar este workflow. Retorna a URL e, uma única vez, o segredo quando o modo de autenticação exige. Um workflow só pode ter um webhook.
// @Tags workflows
// @Accept json
// @Produce json
// @Param id path string true "ID do workflow"
// @Param body body webhookConfigRequest false "Configuração do webhook"
// @Success 201 {object} webhookConfigResponse
// @Failure 409 {object} response.ErrorResponse "O workflow já possui um webhook"
// @Router /workflows/{id}/webhook [post]
func (h *Handler) CreateWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	req := decodeWebhookConfig(r)
	wh, err := h.configUC.Create(workflow_usecase.WebhookConfigInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		WorkflowID:  mux.Vars(r)["id"],
		AuthMode:    workflow.WebhookAuthMode(req.AuthMode),
		HeaderName:  req.HeaderName,
		Method:      req.Method,
	})
	if err != nil {
		handleWebhookConfigError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toWebhookResponse(wh))
}

// UpdateWorkflowWebhook godoc
// @Summary Atualizar a configuração do gatilho de webhook
// @Description Atualiza o modo de autenticação, o método HTTP aceito ou o estado ativo do webhook. A URL e o token são preservados.
// @Tags workflows
// @Accept json
// @Produce json
// @Param id path string true "ID do workflow"
// @Param body body webhookConfigRequest true "Configuração do webhook"
// @Success 200 {object} webhookConfigResponse
// @Failure 404 {object} response.ErrorResponse "Webhook não configurado"
// @Router /workflows/{id}/webhook [put]
func (h *Handler) UpdateWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	req := decodeWebhookConfig(r)
	wh, err := h.configUC.Update(workflow_usecase.WebhookConfigInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		WorkflowID:  mux.Vars(r)["id"],
		AuthMode:    workflow.WebhookAuthMode(req.AuthMode),
		HeaderName:  req.HeaderName,
		Method:      req.Method,
		Active:      req.Active,
	})
	if err != nil {
		handleWebhookConfigError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toWebhookResponse(wh))
}

// RotateWorkflowWebhook godoc
// @Summary Rotacionar o token e o segredo do webhook
// @Description Gera um novo token (nova URL pública) e um novo segredo. As integrações existentes param de funcionar até serem atualizadas com a nova URL e o novo segredo.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Success 200 {object} webhookConfigResponse
// @Failure 404 {object} response.ErrorResponse "Webhook não configurado"
// @Router /workflows/{id}/webhook/rotate [post]
func (h *Handler) RotateWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	wh, err := h.configUC.Rotate(middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		handleWebhookConfigError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toWebhookResponse(wh))
}

// DeleteWorkflowWebhook godoc
// @Summary Remover o gatilho de webhook de um workflow
// @Description Remove o webhook. A URL pública deixa de existir e não inicia mais execuções.
// @Tags workflows
// @Param id path string true "ID do workflow"
// @Success 204
// @Router /workflows/{id}/webhook [delete]
func (h *Handler) DeleteWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	if err := h.configUC.Delete(middleware.GetWorkspaceID(r), mux.Vars(r)["id"]); err != nil {
		handleWebhookConfigError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// HandleWebhookTrigger godoc
// @Summary Receptor público de webhook que inicia uma execução de workflow
// @Description Endpoint público, autenticado pelo token na URL e, opcionalmente, por header_token ou HMAC, que inicia uma execução do workflow associado. O corpo JSON identifica a entrada de UMA forma: entry_id + entry_type, quando o chamador já conhece o id interno; OU phone, que é resolvido para a conversa de WhatsApp mais recente da workspace. Redisparos idênticos são deduplicados (status "duplicate"); se já houver execução ativa para a mesma entrada e gatilho, retorna "already_running". Campos extras do provedor ficam disponíveis no workflow em {{webhook.body}}.
// @Tags webhooks
// @Accept json
// @Produce json
// @Param token path string true "Token do webhook"
// @Param body body webhookTriggerRequest false "Identificação da entrada e dados do provedor"
// @Success 202 {object} webhookTriggerResponse "Execução iniciada, já em andamento ou duplicada"
// @Failure 400 {object} response.ErrorResponse "Payload sem entry_id/entry_type e sem phone"
// @Failure 401 {object} response.ErrorResponse "Autenticação inválida"
// @Failure 404 {object} response.ErrorResponse "Webhook ou entrada não encontrados"
// @Failure 409 {object} response.ErrorResponse "O workflow não está ativo"
// @Failure 422 {object} response.ErrorResponse "O workflow não possui nó de gatilho de webhook"
// @Failure 429 {object} response.ErrorResponse "Limite de execuções simultâneas atingido"
// @Router /webhooks/workflow/{token} [post]
func (h *Handler) HandleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, webhookMaxBodyBytes))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Corpo inválido", nil)
		return
	}

	result, err := h.triggerUC.Execute(workflow_usecase.WebhookRequest{
		Token:   mux.Vars(r)["token"],
		Method:  r.Method,
		RawBody: body,
		Header:  r.Header.Get,
	})
	if err != nil {
		handleWebhookTriggerError(w, err)
		return
	}

	switch {
	case result.Duplicate:
		response.WriteSuccess(w, http.StatusOK, webhookTriggerResponse{Status: "duplicate"})
	case result.AlreadyRunning:
		response.WriteSuccess(w, http.StatusOK, webhookTriggerResponse{Status: "already_running", RunID: result.RunID})
	default:
		response.WriteSuccess(w, http.StatusAccepted, webhookTriggerResponse{Status: "accepted", RunID: result.RunID})
	}
}

func handleWebhookTriggerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflow.ErrWebhookNotFound):
		response.WriteError(w, http.StatusNotFound, "Webhook não encontrado", nil)
	case errors.Is(err, workflow.ErrWebhookMethodNotAllowed):
		response.WriteError(w, http.StatusMethodNotAllowed, "Método não permitido", nil)
	case errors.Is(err, workflow.ErrWebhookUnauthorized):
		response.WriteError(w, http.StatusUnauthorized, "Autenticação inválida", nil)
	case errors.Is(err, workflow.ErrWebhookEntryRequired):
		response.WriteError(w, http.StatusBadRequest, "O payload precisa conter entry_id e entry_type, ou um phone", nil)
	case errors.Is(err, workflow.ErrWebhookEntryNotFound):
		response.WriteError(w, http.StatusNotFound, "Nenhuma entrada encontrada para o telefone informado", nil)
	case errors.Is(err, workflow.ErrWebhookEntryForbidden):
		response.WriteError(w, http.StatusForbidden, "Entrada não pertence a este workspace", nil)
	case errors.Is(err, workflow.ErrWebhookNoTriggerNode):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow não possui nó de gatilho de webhook", nil)
	case errors.Is(err, workflow.ErrWorkflowNotActive):
		response.WriteError(w, http.StatusConflict, "O workflow não está ativo", nil)
	case errors.Is(err, workflow.ErrWorkspaceAtCapacity):
		response.WriteError(w, http.StatusTooManyRequests, "Limite de execuções simultâneas atingido", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Erro ao processar webhook", nil)
	}
}

func handleWebhookConfigError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflow.ErrWorkflowNotFound):
		response.WriteError(w, http.StatusNotFound, "Workflow não encontrado", nil)
	case errors.Is(err, workflow.ErrWebhookNotFound):
		response.WriteError(w, http.StatusNotFound, "Webhook não configurado", nil)
	case errors.Is(err, workflow.ErrWebhookAlreadyExists):
		response.WriteError(w, http.StatusConflict, "O workflow já possui um webhook", nil)
	case errors.Is(err, workflow.ErrWebhookInvalidAuthMode):
		response.WriteValidationError(w, map[string]string{"auth_mode": "Modo de autenticação inválido"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Erro ao configurar webhook", nil)
	}
}

func decodeWebhookConfig(r *http.Request) webhookConfigRequest {
	var req webhookConfigRequest
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, webhookMaxBodyBytes)).Decode(&req)
	}
	return req
}

func toWebhookResponse(wh *workflow.WorkflowWebhook) webhookConfigResponse {
	return webhookConfigResponse{
		ID:         wh.ID,
		WorkflowID: wh.WorkflowID,
		URL:        webhookPublicURL(wh.Token),
		AuthMode:   string(wh.AuthMode),
		Secret:     wh.Secret,
		HeaderName: wh.HeaderName,
		Method:     wh.Method,
		Active:     wh.Active,
	}
}

func webhookPublicURL(token string) string {
	base := strings.TrimRight(os.Getenv("API_BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/webhooks/workflow/" + token
}
