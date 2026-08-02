package telegram

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	"vozko/infra/http/middleware"
	tguc "vozko/usecases/telegram"
)

// Handler serves the Telegram account and deep-link endpoints.
type Handler struct {
	connect    *tguc.ConnectAccountUseCase
	reregister *tguc.ReregisterWebhookUseCase
	list       *tguc.ListAccountsUseCase
	get        *tguc.GetAccountUseCase
	updateCfg  *tguc.UpdateAccountConfigUseCase
	disconnect *tguc.DisconnectAccountUseCase

	createLink *tguc.CreateDeepLinkUseCase
	listLinks  *tguc.ListDeepLinksUseCase
	deleteLink *tguc.DeleteDeepLinkUseCase
}

// HandlerDeps groups the usecases.
type HandlerDeps struct {
	Connect    *tguc.ConnectAccountUseCase
	Reregister *tguc.ReregisterWebhookUseCase
	List       *tguc.ListAccountsUseCase
	Get        *tguc.GetAccountUseCase
	UpdateCfg  *tguc.UpdateAccountConfigUseCase
	Disconnect *tguc.DisconnectAccountUseCase

	CreateLink *tguc.CreateDeepLinkUseCase
	ListLinks  *tguc.ListDeepLinksUseCase
	DeleteLink *tguc.DeleteDeepLinkUseCase
}

func NewHandler(d HandlerDeps) *Handler {
	return &Handler{
		connect:    d.Connect,
		reregister: d.Reregister,
		list:       d.List,
		get:        d.Get,
		updateCfg:  d.UpdateCfg,
		disconnect: d.Disconnect,
		createLink: d.CreateLink,
		listLinks:  d.ListLinks,
		deleteLink: d.DeleteLink,
	}
}

// ---------------------------------------------------------------- accounts

type connectRequest struct {
	// BotToken is the string BotFather hands the operator. It is accepted once,
	// encrypted at rest and never returned by any endpoint.
	BotToken     string  `json:"botToken"`
	DepartmentID *string `json:"departmentId,omitempty"`
}

// ConnectAccount attaches a bot to the caller's workspace.
func (h *Handler) ConnectAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return
	}

	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	account, err := h.connect.Execute(r.Context(), tguc.ConnectInput{
		WorkspaceID:  workspaceID,
		DepartmentID: req.DepartmentID,
		BotToken:     req.BotToken,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toAccountDTO(account))
}

// ListAccounts returns the workspace's bots.
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return
	}

	query := r.URL.Query()
	in := tgdomain.ListAccountsInput{
		WorkspaceID: workspaceID,
		Search:      query.Get("search"),
		Options:     shared.QueryOptions{Pagination: httpx.ParsePagination(query)},
	}
	if v := query.Get("status"); v != "" {
		status := tgdomain.Status(v)
		in.Status = &status
	}
	if v := query.Get("mode"); v != "" {
		mode := tgdomain.Mode(v)
		in.Mode = &mode
	}

	result, err := h.list.Execute(r.Context(), in)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]accountDTO, 0, len(result.Items))
	for _, account := range result.Items {
		items = append(items, toAccountDTO(account))
	}

	// WritePaginated, not WriteSuccess: every paginated list in this API answers
	// {"data": [...], "meta": {...}}, and the browser client reads exactly that
	// shape. A bespoke {"items": ...} envelope parses without error and yields an
	// empty list — the account is created, the request is 200, and the table is
	// simply blank.
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

// GetAccount returns one bot.
func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	account, err := h.get.Execute(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountDTO(account))
}

type updateAccountRequest struct {
	DepartmentID         *string `json:"departmentId"`
	AgentID              *string `json:"agentId"`
	WorkflowID           *string `json:"workflowId"`
	PipelineID           *string `json:"pipelineId"`
	EnableAgentResponses *bool   `json:"enableAgentResponses"`
	EnableWorkflow       *bool   `json:"enableWorkflow"`
	EnableAnalysis       *bool   `json:"enableAnalysis"`
	EnableAutoStaging    *bool   `json:"enableAutoStaging"`
}

// UpdateAccount edits a bot's automation configuration.
func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)

	var req updateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	account, err := h.updateCfg.Execute(r.Context(), workspaceID, mux.Vars(r)["id"], tguc.UpdateAccountConfigInput{
		DepartmentID:         req.DepartmentID,
		AgentID:              req.AgentID,
		WorkflowID:           req.WorkflowID,
		PipelineID:           req.PipelineID,
		EnableAgentResponses: req.EnableAgentResponses,
		EnableWorkflow:       req.EnableWorkflow,
		EnableAnalysis:       req.EnableAnalysis,
		EnableAutoStaging:    req.EnableAutoStaging,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountDTO(account))
}

// ReregisterWebhook re-points Telegram at us.
//
// This is the recovery action for the channel's worst failure: undelivered
// updates are discarded after 24 hours and cannot be recovered, so the fix has to
// be one button rather than a support ticket.
func (h *Handler) ReregisterWebhook(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	account, err := h.reregister.Execute(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountDTO(account))
}

// DisconnectAccount removes a bot.
func (h *Handler) DisconnectAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if err := h.disconnect.Execute(r.Context(), workspaceID, mux.Vars(r)["id"]); err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// ---------------------------------------------------------------- deep links

type createDeepLinkRequest struct {
	Label        string  `json:"label"`
	LeadID       *string `json:"leadId,omitempty"`
	CampaignID   *string `json:"campaignId,omitempty"`
	AgentID      *string `json:"agentId,omitempty"`
	DepartmentID *string `json:"departmentId,omitempty"`
	// TTLHours bounds the link's life. Zero means it never expires, which is
	// right for a printed QR code and wrong for one-off outreach.
	TTLHours int `json:"ttlHours,omitempty"`
}

// CreateDeepLink mints an attributed t.me link.
func (h *Handler) CreateDeepLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)

	var req createDeepLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	result, err := h.createLink.Execute(r.Context(), tguc.CreateDeepLinkInput{
		WorkspaceID:  workspaceID,
		AccountID:    mux.Vars(r)["id"],
		Label:        req.Label,
		LeadID:       req.LeadID,
		CampaignID:   req.CampaignID,
		AgentID:      req.AgentID,
		DepartmentID: req.DepartmentID,
		TTL:          time.Duration(req.TTLHours) * time.Hour,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, result)
}

// ListDeepLinks lists an account's links.
func (h *Handler) ListDeepLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	links, err := h.listLinks.Execute(r.Context(), workspaceID, mux.Vars(r)["id"], limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"items": links})
}

// DeleteDeepLink removes a link.
func (h *Handler) DeleteDeepLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	vars := mux.Vars(r)
	if err := h.deleteLink.Execute(r.Context(), workspaceID, vars["id"], vars["token"]); err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------- mapping

// accountDTO is the API shape of a connected bot.
//
// It carries no credential of any kind: neither the bot token nor the webhook
// secret is ever serialized, so an over-broad RBAC grant cannot leak the ability
// to impersonate the bot.
type accountDTO struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`
	Mode         string  `json:"mode"`

	BotUserID            string `json:"botUserId"`
	BotUsername          string `json:"botUsername"`
	BotName              string `json:"botName,omitempty"`
	DisplayName          string `json:"displayName"`
	CanConnectToBusiness bool   `json:"canConnectToBusiness"`

	Status       string `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`

	WebhookSetAt        *time.Time `json:"webhookSetAt,omitempty"`
	WebhookPendingCount int        `json:"webhookPendingCount"`
	WebhookLastError    string     `json:"webhookLastError,omitempty"`
	WebhookHealthy      bool       `json:"webhookHealthy"`

	BusinessUsername string                   `json:"businessUsername,omitempty"`
	BusinessEnabled  bool                     `json:"businessEnabled"`
	BusinessRights   *tgdomain.BusinessRights `json:"businessRights,omitempty"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toAccountDTO(a *tgdomain.Account) accountDTO {
	return accountDTO{
		ID:           a.ID,
		WorkspaceID:  a.WorkspaceID,
		DepartmentID: a.DepartmentID,
		Mode:         string(a.Mode),
		// Rendered as a string: a Telegram id can exceed 2^53 and would lose
		// precision in a JavaScript number.
		BotUserID:            strconv.FormatInt(a.BotUserID, 10),
		BotUsername:          a.BotUsername,
		BotName:              a.BotName,
		DisplayName:          a.DisplayName(),
		CanConnectToBusiness: a.CanConnectToBusiness,
		Status:               string(a.Status),
		StatusReason:         a.StatusReason,
		WebhookSetAt:         a.WebhookSetAt,
		WebhookPendingCount:  a.WebhookPendingCount,
		WebhookLastError:     a.WebhookLastError,
		WebhookHealthy:       !a.WebhookUnhealthy(20),
		BusinessUsername:     a.BusinessUsername,
		BusinessEnabled:      a.BusinessEnabled,
		BusinessRights:       a.BusinessRights,
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		PipelineID:           a.PipelineID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		EnableAnalysis:       a.EnableAnalysis,
		EnableAutoStaging:    a.EnableAutoStaging,
		CreatedAt:            a.CreatedAt,
		UpdatedAt:            a.UpdatedAt,
	}
}

// writeDomainError maps a domain error onto an HTTP status.
//
// The mapping is explicit so a caller can distinguish "you pasted a bad token"
// (fixable by them) from "this bot belongs to someone else" (not), instead of
// receiving one opaque 500.
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tgdomain.ErrAccountNotFound),
		errors.Is(err, tgdomain.ErrDeepLinkNotFound):
		response.WriteError(w, http.StatusNotFound, "not found", nil)
	case errors.Is(err, tgdomain.ErrAccountAlreadyLinked):
		response.WriteError(w, http.StatusConflict, "this bot is already connected to another workspace", nil)
	case errors.Is(err, tgdomain.ErrBotTokenRequired),
		errors.Is(err, tgdomain.ErrBotTokenInvalid),
		errors.Is(err, tgdomain.ErrWorkspaceIDRequired),
		errors.Is(err, tgdomain.ErrInvalidMode):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
	}
}
