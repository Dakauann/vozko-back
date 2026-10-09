package savedview

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterRoutes(protected *mux.Router, h *SavedViewHandler) {
	svRoutes := protected.PathPrefix("/saved-views").Subrouter()
	svRoutes.HandleFunc("", h.List).Methods(http.MethodGet)
	svRoutes.HandleFunc("", h.Create).Methods(http.MethodPost)
	svRoutes.HandleFunc("/{id}", h.Update).Methods(http.MethodPut)
	svRoutes.HandleFunc("/{id}", h.Delete).Methods(http.MethodDelete)
	svRoutes.HandleFunc("/{id}/default", h.SetDefault).Methods(http.MethodPut)
}
