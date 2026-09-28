package facebook

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
	fbuc "vozko/usecases/facebook"
)

func (h *Handler) TakeThreadControl(w http.ResponseWriter, r *http.Request) {
	actor, ok := threadActor(w, r)
	if !ok {
		return
	}
	owner, err := h.threadControl.Take(r.Context(), actor, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to take control of the conversation")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"ok": true, "threadOwnerAppId": owner})
}

func (h *Handler) ReleaseThreadControl(w http.ResponseWriter, r *http.Request) {
	actor, ok := threadActor(w, r)
	if !ok {
		return
	}
	if err := h.threadControl.Release(r.Context(), actor, mux.Vars(r)["id"]); err != nil {
		writeDomainError(w, err, "Failed to release the conversation")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) ThreadState(w http.ResponseWriter, r *http.Request) {
	actor, ok := threadActor(w, r)
	if !ok {
		return
	}
	state, err := h.threadControl.State(r.Context(), actor, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to read the conversation thread")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{
		"holder": state.Holder, "ownerAppId": state.OwnerAppID, "isDefaultRouteApp": state.IsDefaultRouteApp,
	})
}

func threadActor(w http.ResponseWriter, r *http.Request) (fbuc.ThreadActor, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return fbuc.ThreadActor{}, false
	}
	return fbuc.ThreadActor{
		WorkspaceID: middleware.GetWorkspaceID(r),
		UserID:      claims.UserID,
		IsAdmin:     claims.Role == string(user.RoleAdmin),
	}, true
}
