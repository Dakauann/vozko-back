package whatsappbusinessphone

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type RateLimiter interface {
	Validate(next http.Handler) http.Handler
}

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *WhatsAppBusinessPhoneHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
	phoneVerificationRateLimiter RateLimiter,
) {
	bp := workspace_domain.ResourceBusinessPhones
	protected.HandleFunc("/whatsapp/business-phones", ac(bp, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/business-phones/verticals", ac(bp, workspace_domain.ActionRead, h.ListVerticals)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/business-phones/{id}", ac(bp, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)

	protected.HandleFunc("/whatsapp/business-phones/{id}/sync", ac(bp, workspace_domain.ActionUpdate, h.SyncOneForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/business-phones/{id}/deregister", ac(bp, workspace_domain.ActionUpdate, h.DeregisterForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/business-phones/{id}/release", ac(bp, workspace_domain.ActionUpdate, h.ReleaseForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/business-phones/{id}/business-profile", ac(bp, workspace_domain.ActionRead, h.GetBusinessProfileForWorkspace)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/business-phones/{id}/business-profile", ac(bp, workspace_domain.ActionUpdate, h.UpdateBusinessProfileForWorkspace)).Methods(http.MethodPut)
	protected.HandleFunc("/whatsapp/business-phones/{id}/upload-profile-picture", ac(bp, workspace_domain.ActionUpdate, h.UploadProfilePictureForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/business-phones/{id}/calling", ac(bp, workspace_domain.ActionRead, h.GetCallingStatusForWorkspace)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/business-phones/{id}/calling", ac(bp, workspace_domain.ActionUpdate, h.SetCallingStatusForWorkspace)).Methods(http.MethodPut)

	verifySubrouter := protected.PathPrefix("/whatsapp/business-phones/{id}").Subrouter()
	verifySubrouter.Use(phoneVerificationRateLimiter.Validate)
	verifySubrouter.HandleFunc("/request-verification", ac(bp, workspace_domain.ActionUpdate, h.RequestVerificationForWorkspace)).Methods(http.MethodPost)
	verifySubrouter.HandleFunc("/verify-code", ac(bp, workspace_domain.ActionUpdate, h.VerifyCodeForWorkspace)).Methods(http.MethodPost)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *WhatsAppBusinessPhoneHandler) {
	adminRoutes.HandleFunc("/admin/whatsapp/business-phones", h.ListAll).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/whatsapp/business-phones/{id}", h.GetAdmin).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}", h.Delete).Methods(http.MethodDelete)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/sync", h.SyncOne).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/register", h.Register).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/request-verification", h.RequestVerification).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/verify-code", h.VerifyCode).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/business-profile", h.GetBusinessProfile).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/business-profile", h.UpdateBusinessProfile).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/owner", h.AssignOwner).Methods(http.MethodPatch)
	adminRoutes.HandleFunc("/whatsapp/business-phones/{id}/owner", h.UnassignOwner).Methods(http.MethodDelete)
	adminRoutes.HandleFunc("/whatsapp/business-phones/profile-picture", h.UploadProfilePicture).Methods(http.MethodPost)
}
