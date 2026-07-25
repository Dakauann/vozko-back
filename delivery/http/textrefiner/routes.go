package textrefiner

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterProtectedRoutes(protected *mux.Router, h *TextRefinerHandler) {
	protected.HandleFunc("/text-refiner/refine", h.Refine).Methods(http.MethodPost)
	protected.HandleFunc("/text-refiner/models", h.ListModels).Methods(http.MethodGet)
}
