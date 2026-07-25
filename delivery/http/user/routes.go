package user

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterProtectedRoutes(protected *mux.Router, h *UserHandler) {
	protected.HandleFunc("/user/me", h.GetMe).Methods(http.MethodGet)
	protected.HandleFunc("/user/me", h.UpdateProfile).Methods(http.MethodPatch)
	protected.HandleFunc("/user/me", h.DeleteMe).Methods(http.MethodDelete)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *UserHandler) {
	adminRoutes.HandleFunc("/admin/users", h.List).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/users/{id}", h.Get).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/users/{id}/role", h.UpdateRole).Methods(http.MethodPatch)
}
