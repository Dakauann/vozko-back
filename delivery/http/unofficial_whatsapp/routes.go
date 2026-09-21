package unofficial_whatsapp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}

	res := workspace_domain.ResourceUnofficialWhatsAppInstances
	r := protected.PathPrefix("/unofficial-whatsapp").Subrouter()

	r.HandleFunc("/instances", ac(res, workspace_domain.ActionCreate, h.CreateInstance)).Methods(http.MethodPost)

	r.HandleFunc("/instances/allowance", ac(res, workspace_domain.ActionRead, h.GetAllowance)).Methods(http.MethodGet)
	r.HandleFunc("/instances", ac(res, workspace_domain.ActionRead, h.ListInstances)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}", ac(res, workspace_domain.ActionRead, h.GetInstance)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateInstance)).Methods(http.MethodPut)
	r.HandleFunc("/instances/{id}", ac(res, workspace_domain.ActionDelete, h.DeleteInstance)).Methods(http.MethodDelete)

	r.HandleFunc("/instances/{id}/connect", ac(res, workspace_domain.ActionUpdate, h.Connect)).Methods(http.MethodPost)
	r.HandleFunc("/instances/{id}/link-status", ac(res, workspace_domain.ActionRead, h.LinkStatus)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}/disconnect", ac(res, workspace_domain.ActionUpdate, h.Disconnect)).Methods(http.MethodPost)

	r.HandleFunc("/instances/{id}/reset", ac(res, workspace_domain.ActionUpdate, h.Reset)).Methods(http.MethodPost)
	r.HandleFunc("/instances/{id}/webhook/rotate", ac(res, workspace_domain.ActionUpdate, h.RotateWebhookToken)).Methods(http.MethodPost)

	r.HandleFunc("/instances/{id}/conversations", ac(res, workspace_domain.ActionSend, h.StartConversation)).Methods(http.MethodPost)

}

func RegisterGroupRoutes(
	protected *mux.Router,
	h *GroupHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}

	res := workspace_domain.ResourceUnofficialWhatsAppInstances
	r := protected.PathPrefix("/unofficial-whatsapp").Subrouter()

	r.HandleFunc("/instances/{id}/groups", ac(res, workspace_domain.ActionRead, h.ListGroups)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}/groups/{groupJid}", ac(res, workspace_domain.ActionRead, h.GetGroup)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}/groups/{groupJid}/invite-link", ac(res, workspace_domain.ActionRead, h.GetInviteLink)).Methods(http.MethodGet)

	r.HandleFunc("/instances/{id}/groups/{groupJid}", ac(res, workspace_domain.ActionUpdate, h.UpdateGroup)).Methods(http.MethodPatch)
	r.HandleFunc("/instances/{id}/groups/{groupJid}/participants", ac(res, workspace_domain.ActionUpdate, h.UpdateParticipants)).Methods(http.MethodPost)

	r.HandleFunc("/instances/{id}/groups/{groupJid}/participants", ac(res, workspace_domain.ActionDelete, h.RemoveParticipants)).Methods(http.MethodDelete)
	r.HandleFunc("/instances/{id}/groups/{groupJid}/leave", ac(res, workspace_domain.ActionDelete, h.LeaveGroup)).Methods(http.MethodPost)

	r.HandleFunc("/conversations/{entryId}/group", ac(res, workspace_domain.ActionRead, h.GetGroup)).Methods(http.MethodGet)
	r.HandleFunc("/conversations/{entryId}/group/invite-link", ac(res, workspace_domain.ActionRead, h.GetInviteLink)).Methods(http.MethodGet)
	r.HandleFunc("/conversations/{entryId}/group", ac(res, workspace_domain.ActionUpdate, h.UpdateGroup)).Methods(http.MethodPatch)
	r.HandleFunc("/conversations/{entryId}/group/participants", ac(res, workspace_domain.ActionUpdate, h.UpdateParticipants)).Methods(http.MethodPost)
	r.HandleFunc("/conversations/{entryId}/group/participants", ac(res, workspace_domain.ActionDelete, h.RemoveParticipants)).Methods(http.MethodDelete)
	r.HandleFunc("/conversations/{entryId}/group/leave", ac(res, workspace_domain.ActionDelete, h.LeaveGroup)).Methods(http.MethodPost)
}
