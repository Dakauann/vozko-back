package paymentsplit

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterAdminRoutes(admin *mux.Router, h *PaymentSplitHandler) {
	admin.HandleFunc("/payment-splits", h.Create).Methods(http.MethodPost)
	admin.HandleFunc("/payment-splits", h.List).Methods(http.MethodGet)
	admin.HandleFunc("/payment-splits/suppliers", h.GetSuppliers).Methods(http.MethodGet)
	admin.HandleFunc("/payment-splits/{id}", h.Get).Methods(http.MethodGet)
	admin.HandleFunc("/payment-splits/{id}", h.Update).Methods(http.MethodPut)
	admin.HandleFunc("/payment-splits/{id}", h.Delete).Methods(http.MethodDelete)
}
