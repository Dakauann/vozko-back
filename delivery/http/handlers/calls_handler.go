package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	callbillinghttp "vozko/delivery/http/callbilling"
	"vozko/delivery/http/response"
	"vozko/domain/calls/billing"
	cdr "vozko/domain/calls/cdr"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

type callResponse struct {
	ID             string     `json:"id"`
	CallID         string     `json:"callId"`
	WorkspaceID    string     `json:"workspaceId"`
	Type           string     `json:"type"`
	Direction      string     `json:"direction"`
	Source         string     `json:"source"`
	Status         string     `json:"status"`
	AgentID        *string    `json:"agentId,omitempty"`
	ConversationID *string    `json:"conversationId,omitempty"`
	LeadID         *string    `json:"leadId,omitempty"`
	PhoneFrom      string     `json:"phoneFrom,omitempty"`
	PhoneTo        string     `json:"phoneTo,omitempty"`
	TrunkID        *string    `json:"trunkId,omitempty"`
	ParentCallID   *string    `json:"parentCallId,omitempty"`
	EndReason      *string    `json:"endReason,omitempty"`
	StartedAt      time.Time  `json:"startedAt"`
	AnsweredAt     *time.Time `json:"answeredAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	DurationSec    int        `json:"durationSec"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func toCallResponse(c *cdr.Call) callResponse {
	return callResponse{
		ID:             c.ID,
		CallID:         c.CallID,
		WorkspaceID:    c.WorkspaceID,
		Type:           string(c.Type),
		Direction:      string(c.Direction),
		Source:         string(c.Source),
		Status:         string(c.Status),
		AgentID:        c.AgentID,
		ConversationID: c.ConversationID,
		LeadID:         c.LeadID,
		PhoneFrom:      c.PhoneFrom,
		PhoneTo:        c.PhoneTo,
		TrunkID:        c.TrunkID,
		ParentCallID:   c.ParentCallID,
		EndReason:      c.EndReason,
		StartedAt:      c.StartedAt,
		AnsweredAt:     c.AnsweredAt,
		EndedAt:        c.EndedAt,
		DurationSec:    c.DurationSec,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
}

type CallsHandler struct {
	listUC       cdr.ListCallsUseCase
	getUC        cdr.GetCallUseCase
	billingQuery billing.QueryUseCase
}

func NewCallsHandler(listUC cdr.ListCallsUseCase, getUC cdr.GetCallUseCase, billingQuery billing.QueryUseCase) *CallsHandler {
	return &CallsHandler{listUC: listUC, getUC: getUC, billingQuery: billingQuery}
}

type callDetailResponse struct {
	callResponse
	Billing *callbillinghttp.CallBillingResponse `json:"billing,omitempty"`
}

func (h *CallsHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	values := r.URL.Query()
	filters := cdr.ListFilters{
		WorkspaceID: workspaceID,
		QueryOptions: shared.QueryOptions{
			Pagination: parsePagination(values),
		},
	}

	if v := values.Get("type"); v != "" {
		t := cdr.CallType(v)
		filters.Type = &t
	}
	if v := values.Get("direction"); v != "" {
		d := cdr.Direction(v)
		filters.Direction = &d
	}
	if v := values.Get("source"); v != "" {
		s := cdr.Source(v)
		filters.Source = &s
	}
	if v := values.Get("status"); v != "" {
		s := cdr.Status(v)
		filters.Status = &s
	}
	if v := values.Get("agentId"); v != "" {
		filters.AgentID = &v
	}
	if v := values.Get("leadId"); v != "" {
		filters.LeadID = &v
	}
	if v := values.Get("startedFrom"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filters.StartedFrom = &t
		}
	}
	if v := values.Get("startedTo"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filters.StartedTo = &t
		}
	}

	result, err := h.listUC.Execute(filters)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list calls", nil)
		return
	}

	items := make([]callDetailResponse, len(result.Items))
	for i, c := range result.Items {
		items[i] = callDetailResponse{callResponse: toCallResponse(c)}
	}

	// Enrich each row with its billing summary (cost breakdown + recording URL)
	// in a single batch query so the unified Calls view shows cost and recording
	// inline without an N+1 fan-out from the client.
	if h.billingQuery != nil && len(items) > 0 {
		callIDs := make([]string, 0, len(items))
		for i := range items {
			callIDs = append(callIDs, items[i].CallID)
		}
		if byCallID, err := h.billingQuery.GetByCallIDs(callIDs); err == nil {
			for i := range items {
				if record, ok := byCallID[items[i].CallID]; ok && record != nil {
					dto := callbillinghttp.ToCallBillingResponse(record)
					items[i].Billing = &dto
				}
			}
		}
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[callDetailResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

func (h *CallsHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	callID := mux.Vars(r)["callId"]
	if callID == "" {
		response.WriteError(w, http.StatusBadRequest, "callId is required", nil)
		return
	}

	c, err := h.getUC.Execute(callID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to load call", nil)
		return
	}
	if c == nil || c.WorkspaceID != workspaceID {
		response.WriteError(w, http.StatusNotFound, "call not found", nil)
		return
	}

	detail := callDetailResponse{callResponse: toCallResponse(c)}
	if h.billingQuery != nil {
		record, err := h.billingQuery.GetByCallID(c.CallID)
		if err == nil && record != nil {
			dto := callbillinghttp.ToCallBillingResponse(record)
			detail.Billing = &dto
		} else if err != nil && !errors.Is(err, billing.ErrBillingRecordNotFound) {

			_ = err
		}
	}

	response.WriteSuccess(w, http.StatusOK, detail)
}
