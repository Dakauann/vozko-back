package teamchathttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	protected.HandleFunc("/team-chat/people", h.People).Methods(http.MethodGet)
	protected.HandleFunc("/team-chat/summary", h.Summary).Methods(http.MethodGet)
	protected.HandleFunc("/team-chat/threads", h.Threads).Methods(http.MethodGet)
	protected.HandleFunc("/team-chat/direct", h.OpenDirect).Methods(http.MethodPost)
	protected.HandleFunc("/team-chat/groups", ac(workspace_domain.ResourceTeamChat, workspace_domain.ActionCreate, h.CreateGroup)).Methods(http.MethodPost)
	protected.HandleFunc("/team-chat/threads/{id}", h.Thread).Methods(http.MethodGet)
	protected.HandleFunc("/team-chat/threads/{id}", h.Rename).Methods(http.MethodPatch)
	protected.HandleFunc("/team-chat/threads/{id}/members", h.AddMembers).Methods(http.MethodPost)
	protected.HandleFunc("/team-chat/threads/{id}/members/{userId}", h.RemoveMember).Methods(http.MethodDelete)
	protected.HandleFunc("/team-chat/threads/{id}/leave", h.Leave).Methods(http.MethodPost)
	protected.HandleFunc("/team-chat/threads/{id}/mute", h.Mute).Methods(http.MethodPut)
	protected.HandleFunc("/team-chat/threads/{id}/messages", h.Messages).Methods(http.MethodGet)
	protected.HandleFunc("/team-chat/threads/{id}/messages", h.Send).Methods(http.MethodPost)
	protected.HandleFunc("/team-chat/threads/{id}/read", h.MarkRead).Methods(http.MethodPost)
	protected.HandleFunc("/team-chat/messages/{id}", h.Edit).Methods(http.MethodPatch)
	protected.HandleFunc("/team-chat/messages/{id}", h.Delete).Methods(http.MethodDelete)
	protected.HandleFunc("/team-chat/threads/{id}/scheduled", h.ScheduledList).Methods(http.MethodGet)
	protected.HandleFunc("/team-chat/threads/{id}/scheduled", h.Schedule).Methods(http.MethodPost)
	protected.HandleFunc("/team-chat/scheduled/{id}", h.Reschedule).Methods(http.MethodPatch)
	protected.HandleFunc("/team-chat/scheduled/{id}", h.CancelScheduled).Methods(http.MethodDelete)
}
