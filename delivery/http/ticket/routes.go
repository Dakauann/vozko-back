package ticket

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterProtectedRoutes(protected *mux.Router, h *TicketHandler) {
	protected.HandleFunc("/tickets", h.ListUserTickets).Methods(http.MethodGet)
	protected.HandleFunc("/orders/{orderId}/ticket", h.GetByOrder).Methods(http.MethodGet)
	protected.HandleFunc("/tickets/{ticketId}/documents", h.UploadDocument).Methods(http.MethodPost)
}

func RegisterAdminRoutes(admin *mux.Router, h *TicketHandler) {
	admin.HandleFunc("/tickets", h.ListAll).Methods(http.MethodGet)
	admin.HandleFunc("/tickets/{ticketId}/status", h.UpdateStatus).Methods(http.MethodPatch)
	admin.HandleFunc("/tickets/{ticketId}/generate-label", h.GenerateLabel).Methods(http.MethodPost)
}
