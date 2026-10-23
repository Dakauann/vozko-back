package emailinbox

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	domain "vozko/domain/emailinbox"
	"vozko/infra/http/middleware"
	usecase "vozko/usecases/emailinbox"
)

const maxBody = 16 << 10

type Handler struct {
	inboxes *usecase.Inboxes
}

func NewHandler(inboxes *usecase.Inboxes) *Handler {
	return &Handler{inboxes: inboxes}
}

type InboxResponse struct {
	ID                   string    `json:"id"`
	WorkspaceID          string    `json:"workspaceId"`
	Name                 string    `json:"name"`
	Address              string    `json:"address"`
	DepartmentID         *string   `json:"departmentId,omitempty"`
	PipelineID           *string   `json:"pipelineId,omitempty"`
	AgentID              *string   `json:"agentId,omitempty"`
	WorkflowID           *string   `json:"workflowId,omitempty"`
	EnableAgentResponses bool      `json:"enableAgentResponses"`
	EnableWorkflow       bool      `json:"enableWorkflow"`
	EnableAnalysis       bool      `json:"enableAnalysis"`
	EnableAutoStaging    bool      `json:"enableAutoStaging"`
	EnableAutoMemory     bool      `json:"enableAutoMemory"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type InboxListResponse struct {
	Items []InboxResponse `json:"items"`
}

type InboxSettingsRequest struct {
	DepartmentID         *string `json:"departmentId"`
	PipelineID           *string `json:"pipelineId"`
	AgentID              *string `json:"agentId"`
	WorkflowID           *string `json:"workflowId"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
}

func toResponse(i *domain.Inbox) InboxResponse {
	return InboxResponse{
		ID: i.ID, WorkspaceID: i.WorkspaceID, Name: i.Name, Address: string(i.Address),
		DepartmentID: i.DepartmentID, PipelineID: i.PipelineID, AgentID: i.AgentID, WorkflowID: i.WorkflowID,
		EnableAgentResponses: i.EnableAgentResponses, EnableWorkflow: i.EnableWorkflow, EnableAnalysis: i.EnableAnalysis,
		EnableAutoStaging: i.EnableAutoStaging, EnableAutoMemory: i.EnableAutoMemory,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
	}
}

// @Summary		Listar caixas de e-mail
// @Description	Uma caixa por remetente do workspace. As respostas dos clientes aos e-mails de um remetente chegam no atendimento por essa caixa, com o departamento, o funil e o agente ou fluxo configurados nela. A lista cria a caixa de um remetente novo, para que ela possa ser configurada antes da primeira resposta.
// @Tags			E-mail
// @Produce		json
// @Success		200	{object}	InboxListResponse
// @Failure		403	{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/email/inboxes [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	inboxes, err := h.inboxes.List(r.Context(), middleware.GetWorkspaceID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]InboxResponse, 0, len(inboxes))
	for _, inbox := range inboxes {
		items = append(items, toResponse(inbox))
	}
	response.WriteSuccess(w, http.StatusOK, InboxListResponse{Items: items})
}

// @Summary		Ver caixa de e-mail
// @Description	A caixa do remetente informado (o id da caixa é o id do remetente).
// @Tags			E-mail
// @Produce		json
// @Param			senderId	path		string	true	"id do remetente"
// @Success		200			{object}	InboxResponse
// @Failure		404			{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/email/inboxes/{senderId} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	inbox, err := h.inboxes.Get(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["senderId"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(inbox))
}

// @Summary		Configurar caixa de e-mail
// @Description	Substitui o departamento, o funil, o agente, o fluxo e as automações da caixa. Campos de referência vazios ou ausentes ficam sem valor. Agente, fluxo, funil e departamento precisam ser deste workspace (422 reference_not_in_workspace).
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			senderId	path		string					true	"id do remetente"
// @Param			body		body		InboxSettingsRequest	true	"configuração da caixa"
// @Success		200			{object}	InboxResponse
// @Failure		400			{object}	response.CodedErrorResponse
// @Failure		404			{object}	response.CodedErrorResponse
// @Failure		422			{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/email/inboxes/{senderId} [put]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req InboxSettingsRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil || json.Unmarshal(body, &req) != nil {
		response.WriteCodedError(w, http.StatusBadRequest, "invalid_body", "invalid request body")
		return
	}
	inbox, err := h.inboxes.Update(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["senderId"], domain.InboxSettings{
		DepartmentID: req.DepartmentID, PipelineID: req.PipelineID, AgentID: req.AgentID, WorkflowID: req.WorkflowID,
		EnableAgentResponses: req.EnableAgentResponses, EnableWorkflow: req.EnableWorkflow, EnableAnalysis: req.EnableAnalysis,
		EnableAutoStaging: req.EnableAutoStaging, EnableAutoMemory: req.EnableAutoMemory,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(inbox))
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInboxNotFound):
		response.WriteCodedError(w, http.StatusNotFound, "inbox_not_found", "email inbox not found")
	case errors.Is(err, domain.ErrInvalidReference):
		response.WriteCodedError(w, http.StatusUnprocessableEntity, "reference_not_in_workspace", domain.ErrInvalidReference.Error())
	default:
		log.Printf("[emailinbox] unexpected error: %v", err)
		response.WriteCodedError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
	}
}
