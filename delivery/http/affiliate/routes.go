package affiliate

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterPublicRoutes(public *mux.Router, h *AffiliateHandler) {
	public.HandleFunc("/affiliate/validate/{code}", h.ValidateCode).Methods(http.MethodGet)
}

func RegisterProtectedRoutes(protected *mux.Router, h *AffiliateHandler) {
	protected.HandleFunc("/affiliate/register", h.Register).Methods(http.MethodPost)
	protected.HandleFunc("/affiliate/me", h.GetMe).Methods(http.MethodGet)
	protected.HandleFunc("/affiliate/me", h.UpdateMe).Methods(http.MethodPut)
	protected.HandleFunc("/affiliate/referrals", h.ListReferrals).Methods(http.MethodGet)
	protected.HandleFunc("/affiliate/earnings", h.ListEarnings).Methods(http.MethodGet)
}

func RegisterAdminRoutes(admin *mux.Router, h *AffiliateHandler) {
	admin.HandleFunc("/admin/affiliates", h.AdminList).Methods(http.MethodGet)
	admin.HandleFunc("/admin/affiliates/{id}", h.AdminGet).Methods(http.MethodGet)
	admin.HandleFunc("/admin/affiliates/{id}", h.AdminUpdate).Methods(http.MethodPut)
}
