package systemconfig

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterAdminRoutes(admin *mux.Router, h *SystemConfigHandler) {
	admin.HandleFunc("/admin/system/config", h.Get).Methods(http.MethodGet)
	admin.HandleFunc("/admin/system/config", h.Update).Methods(http.MethodPut)
}
