package dealautomation

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterRoutes(protected *mux.Router, h *Handler) {
	protected.HandleFunc("/deal-automation/{entryType}/{kind}/{containerId}", h.Get).Methods(http.MethodGet)
	protected.HandleFunc("/deal-automation/{entryType}/{kind}/{containerId}", h.Update).Methods(http.MethodPut)
}
