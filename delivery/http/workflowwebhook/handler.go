package workflowwebhook

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
	"vozko/pkg/webhookanswer"
	workflow_usecase "vozko/usecases/workflow"

	"github.com/gorilla/mux"
)

type DeliveryLister interface {
	Execute(viewer shared.Person, workspaceID, workflowID string, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.WebhookDelivery], error)
}

type Handler struct {
	configUC     workflow_usecase.WorkflowWebhookUseCase
	triggerUC    workflow_usecase.HandleWebhookTriggerUseCase
	deliveriesUC DeliveryLister
	workflows    httpx.WorkflowAccess
}

func NewHandler(
	configUC workflow_usecase.WorkflowWebhookUseCase,
	triggerUC workflow_usecase.HandleWebhookTriggerUseCase,
	deliveriesUC DeliveryLister,
	workflows httpx.WorkflowAccess,
) *Handler {
	return &Handler{configUC: configUC, triggerUC: triggerUC, deliveriesUC: deliveriesUC, workflows: workflows}
}

// @Summary Listar as últimas chamadas recebidas pelo webhook de um workflow
// @Description Lista as chamadas ao webhook público deste workflow, da mais recente para a mais antiga: resultado (accepted, duplicate, already_running ou rejected), motivo da recusa (por exemplo workflow_not_active, entry_not_found, unauthorized), código HTTP devolvido, como a conversa foi identificada (entry_id ou phone), a execução iniciada, a latência e apenas os nomes dos campos do corpo, nunca os valores. Chamadas com token desconhecido não aparecem porque não pertencem a nenhum workflow. Chamadas que identificaram uma conversa só aparecem para quem pode ver essa conversa (mesma regra de departamento e de responsável da caixa de entrada); recusas sem conversa aparecem para quem tem `workflows:read` e acesso ao departamento do workflow. As chamadas ficam guardadas por 30 dias.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Param page query int false "Página, a partir de 1"
// @Param pageSize query int false "Itens por página"
// @Success 200 {object} response.PaginatedPayload{data=[]workflow.WebhookDelivery}
// @Failure 401 {object} response.ErrorResponse
// @Failure 403 {object} response.ErrorResponse "Workflow de outro departamento"
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado"
// @Failure 500 {object} response.ErrorResponse "Erro ao listar as chamadas"
// @Router /workflows/{id}/webhook/deliveries [get]
func (h *Handler) ListWorkflowWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	workflowID := mux.Vars(r)["id"]
	if !httpx.RequireWorkflowAccess(w, r, h.workflows, workflowID) {
		return
	}
	result, err := h.deliveriesUC.Execute(viewer, middleware.GetWorkspaceID(r), workflowID, httpx.ParsePagination(r.URL.Query()))
	if err != nil {
		response.WriteRefusal(w, refusalWebhookDeliveriesFailed)
		return
	}
	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

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
		response.WriteRefusal(w, refusalWebhookNotFound)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toWebhookResponse(wh))
}

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
		response.WriteRefusal(w, refusalWebhookInvalidBody)
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

	answer := webhookTriggerResponse{Status: string(workflow.WebhookOutcomeAccepted), RunID: result.RunID}
	switch {
	case result.Duplicate:
		answer = webhookTriggerResponse{Status: string(workflow.WebhookOutcomeDuplicate)}
	case result.AlreadyRunning:
		answer.Status = string(workflow.WebhookOutcomeAlreadyRunning)
	}
	response.WriteSuccess(w, webhookanswer.AcceptStatus(answer.Status), answer)
}

var webhookRejectionMessages = map[string]string{
	"webhook_inactive":      "Webhook não encontrado",
	"webhook_not_found":     "Webhook não encontrado",
	"method_not_allowed":    "Método não permitido",
	"unauthorized":          "Autenticação inválida",
	"entry_required":        "O payload precisa conter entry_id e entry_type, ou um phone",
	"entry_not_found":       "Nenhuma entrada encontrada para o telefone informado",
	"entry_forbidden":       "Entrada não pertence a este workspace",
	"no_trigger_node":       "O workflow não possui nó de gatilho de webhook",
	"workflow_not_active":   "O workflow não está ativo",
	"workspace_at_capacity": "Limite de execuções simultâneas atingido",
}

func handleWebhookTriggerError(w http.ResponseWriter, err error) {
	reason := workflow.WebhookRejectionReason(err)
	message, ok := webhookRejectionMessages[reason]
	if !ok {
		message = "Erro ao processar webhook"
	}
	response.WriteErrorWithCode(w, webhookanswer.RejectionStatus(reason), reason, message, nil)
}

var (
	refusalWebhookNotFound         = response.Refusal{Status: http.StatusNotFound, Code: "webhook_not_found", Message: "Webhook não configurado"}
	refusalWebhookConfigFailed     = response.Refusal{Status: http.StatusInternalServerError, Code: "webhook_config_failed", Message: "Erro ao configurar webhook"}
	refusalWebhookDeliveriesFailed = response.Refusal{Status: http.StatusInternalServerError, Code: "webhook_deliveries_failed", Message: "Erro ao listar as chamadas do webhook"}
	refusalWebhookInvalidBody      = response.Refusal{Status: http.StatusBadRequest, Code: "invalid_body", Message: "Corpo inválido"}
)

var webhookRequestRefusals = []response.Refusal{refusalWebhookConfigFailed, refusalWebhookDeliveriesFailed, httpx.RefusalWorkflowUnreadable}

var webhookOwnConfigRefusals = []response.ErrorRefusal{
	{Err: workflow.ErrWebhookNotFound, Refusal: refusalWebhookNotFound},
	{Err: workflow.ErrWebhookAlreadyExists, Refusal: response.Refusal{Status: http.StatusConflict, Code: "webhook_already_exists", Message: "O workflow já possui um webhook"}},
	{Err: workflow.ErrWebhookInvalidAuthMode, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "invalid_auth_mode", Message: "Modo de autenticação inválido", Expected: map[string]string{"auth_mode": "Modo de autenticação inválido"}}},
}

var webhookConfigRefusals = append(append([]response.ErrorRefusal{}, httpx.WorkflowAccessRefusals...), webhookOwnConfigRefusals...)

func handleWebhookConfigError(w http.ResponseWriter, err error) {
	if !response.WriteErrorRefusal(w, err, webhookConfigRefusals) {
		response.WriteRefusal(w, refusalWebhookConfigFailed)
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
