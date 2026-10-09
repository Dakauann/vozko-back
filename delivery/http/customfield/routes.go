package customfield

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterRoutes(protected *mux.Router, h *CustomFieldHandler) {
	cfRoutes := protected.PathPrefix("/custom-fields").Subrouter()
	cfRoutes.HandleFunc("", h.List).Methods(http.MethodGet)
	cfRoutes.HandleFunc("", h.Create).Methods(http.MethodPost)
	cfRoutes.HandleFunc("/{id}", h.Get).Methods(http.MethodGet)
	cfRoutes.HandleFunc("/{id}", h.Update).Methods(http.MethodPatch)
	cfRoutes.HandleFunc("/{id}", h.Delete).Methods(http.MethodDelete)
}
