package notificationhttp

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterProtectedRoutes(protected *mux.Router, h *Handler) {
	protected.HandleFunc("/notifications", h.List).Methods(http.MethodGet)
	protected.HandleFunc("/notifications/summary", h.Summary).Methods(http.MethodGet)
	protected.HandleFunc("/notifications/read", h.MarkRead).Methods(http.MethodPost)
	protected.HandleFunc("/notifications/unread", h.MarkUnread).Methods(http.MethodPost)
	protected.HandleFunc("/notifications/read-all", h.MarkAllRead).Methods(http.MethodPost)
	protected.HandleFunc("/notifications/preferences", h.Preferences).Methods(http.MethodGet)
	protected.HandleFunc("/notifications/preferences", h.SavePreferences).Methods(http.MethodPut)
}

func RegisterAdminRoutes(admin *mux.Router, h *AdminHandler) {
	admin.HandleFunc("/admin/notifications/broadcasts", h.ListBroadcasts).Methods(http.MethodGet)
	admin.HandleFunc("/admin/notifications/broadcasts", h.CreateBroadcast).Methods(http.MethodPost)
	admin.HandleFunc("/admin/notifications/broadcasts/preview", h.PreviewBroadcast).Methods(http.MethodPost)
	admin.HandleFunc("/admin/notifications/broadcasts/{id}/recipients", h.ListRecipients).Methods(http.MethodGet)
	admin.HandleFunc("/admin/notifications/images", h.UploadImage).Methods(http.MethodPost)
}
