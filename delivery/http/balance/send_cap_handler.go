package balance

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	balancedomain "vozko/domain/balance"
	"vozko/domain/user"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type SendCapHandler struct {
	list   balancedomain.ListMonthlySendCapsUseCase
	set    balancedomain.SetMonthlySendCapUseCase
	unlock balancedomain.UnlockMonthlySendCapUseCase
}

func NewSendCapHandler(
	list balancedomain.ListMonthlySendCapsUseCase,
	set balancedomain.SetMonthlySendCapUseCase,
	unlock balancedomain.UnlockMonthlySendCapUseCase,
) *SendCapHandler {
	return &SendCapHandler{list: list, set: set, unlock: unlock}
}

func RegisterSendCapAdminRoutes(adminRoutes *mux.Router, h *SendCapHandler) {
	adminRoutes.HandleFunc("/admin/send-caps", h.List).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/send-caps/{workspaceId}", h.Set).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/send-caps/{workspaceId}/unlock", h.Unlock).Methods(http.MethodPost)
}

func sendCapActor(r *http.Request) (balancedomain.SendCapActor, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return balancedomain.SendCapActor{}, false
	}
	return balancedomain.SendCapActor{
		UserID:      claims.UserID,
		Email:       claims.Email,
		SystemAdmin: claims.Role == string(user.RoleAdmin),
	}, true
}

// Internal system-admin operation; intentionally excluded from public Swagger.
func (h *SendCapHandler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := sendCapActor(r)
	if !ok {
		response.WriteErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", nil)
		return
	}
	level, err := balancedomain.ParseSendCapLevel(r.URL.Query().Get("level"))
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_level", "level must be ok, near or reached", nil)
		return
	}
	listing, err := h.list.Execute(actor, level)
	if err != nil {
		writeSendCapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSendCapListResponse(listing))
}

// Internal system-admin operation; intentionally excluded from public Swagger.
func (h *SendCapHandler) Set(w http.ResponseWriter, r *http.Request) {
	actor, ok := sendCapActor(r)
	if !ok {
		response.WriteErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", nil)
		return
	}
	var req SetSendCapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Limit == nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_body", "limit is required", map[string]string{"limit": "positive integer"})
		return
	}
	workspaceID := mux.Vars(r)["workspaceId"]
	cap, err := h.set.Execute(actor, workspaceID, *req.Limit)
	if err != nil {
		writeSendCapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSendCapChangeResponse(workspaceID, cap))
}

// Internal system-admin operation; intentionally excluded from public Swagger.
func (h *SendCapHandler) Unlock(w http.ResponseWriter, r *http.Request) {
	actor, ok := sendCapActor(r)
	if !ok {
		response.WriteErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", nil)
		return
	}
	var req UnlockSendCapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Limit == nil) == !req.RemoveCap {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_body", "send either limit or removeCap, plus the code",
			map[string]string{"limit": "positive integer", "removeCap": "true to remove the cap", "code": "4 digits"})
		return
	}
	workspaceID := mux.Vars(r)["workspaceId"]
	cap, err := h.unlock.Execute(actor, balancedomain.UnlockMonthlySendCapInput{WorkspaceID: workspaceID, Limit: req.Limit, Code: req.Code})
	if err != nil {
		writeSendCapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSendCapChangeResponse(workspaceID, cap))
}

func writeSendCapError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, balancedomain.ErrSendCapForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden", "you are not allowed to change this monthly send cap", nil)
	case errors.Is(err, balancedomain.ErrSendCapUnlockRequired):
		response.WriteErrorWithCode(w, http.StatusForbidden, "unlock_required", "raising or removing a monthly send cap requires an unlock", nil)
	case errors.Is(err, balancedomain.ErrInvalidUnlockCode):
		response.WriteErrorWithCode(w, http.StatusForbidden, "invalid_unlock_code", "invalid unlock code", nil)
	case errors.Is(err, balancedomain.ErrInvalidSendCapLimit):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_limit", "the monthly limit must be a positive number", nil)
	case errors.Is(err, balancedomain.ErrMonthlySendCapNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "send_cap_not_found", "this workspace has no monthly send cap", nil)
	case errors.Is(err, workspace.ErrWorkspaceNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "workspace_not_found", "workspace not found", nil)
	default:
		log.Printf("[monthly-send-cap] request failed: %v", err)
		response.WriteErrorWithCode(w, http.StatusInternalServerError, "internal_error", "could not process the monthly send cap", nil)
	}
}
