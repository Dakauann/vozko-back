package readme

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterPublicRoutes(router *mux.Router, handler *Handler) {
	router.HandleFunc("/webhooks/readme", handler.PersonalizedDocs).Methods(http.MethodPost)
}
