package livedecision

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterAdminRoutes(admin *mux.Router, h *Handler) {
	if h == nil {
		return
	}
	admin.HandleFunc("/admin/live-decisions/summary", h.Summary).Methods(http.MethodGet)
}
