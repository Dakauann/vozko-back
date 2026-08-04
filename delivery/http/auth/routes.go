package auth

import (
	"net/http"

	"github.com/gorilla/mux"
)

type RateLimiter interface {
	Validate(next http.Handler) http.Handler
}

type RateLimiters struct {
	Register          RateLimiter
	Login             RateLimiter
	ForgotPassword    RateLimiter
	ResetPassword     RateLimiter
	EmailVerification RateLimiter
}

func RegisterPublicRoutes(public *mux.Router, h *AuthHandler, rl RateLimiters) {
	// Public self-registration is disabled for now: new users are created only by
	// admins via POST /admin/users/register (see RegisterAdminRoutes below).
	// Re-enable by uncommenting this line.
	// public.Handle("/auth/register", rl.Register.Validate(http.HandlerFunc(h.Register))).Methods(http.MethodPost)
	public.Handle("/auth/login", rl.Login.Validate(http.HandlerFunc(h.Login))).Methods(http.MethodPost)
	public.HandleFunc("/auth/refresh", h.RefreshToken).Methods(http.MethodPost)
	// public.Handle("/auth/forgot-password", rl.ForgotPassword.Validate(http.HandlerFunc(h.ForgotPassword))).Methods(http.MethodPost)
	// public.Handle("/auth/reset-password", rl.ResetPassword.Validate(http.HandlerFunc(h.ResetPassword))).Methods(http.MethodPost)
	// public.Handle("/auth/send-email-verification", rl.EmailVerification.Validate(http.HandlerFunc(h.SendEmailVerification))).Methods(http.MethodPost)
}

func RegisterProtectedRoutes(protected *mux.Router, h *AuthHandler) {
	protected.HandleFunc("/auth/verify-email", h.VerifyEmail).Methods(http.MethodPost)
	protected.HandleFunc("/auth/change-password", h.ChangePassword).Methods(http.MethodPost)
	protected.HandleFunc("/auth/logout", h.Logout).Methods(http.MethodPost)
	protected.HandleFunc("/auth/logout-all", h.LogoutAll).Methods(http.MethodPost)
	protected.HandleFunc("/auth/sessions", h.ListSessions).Methods(http.MethodGet)
	protected.HandleFunc("/auth/sessions/{sessionId}", h.RevokeSession).Methods(http.MethodDelete)
}

func RegisterAdminRoutes(admin *mux.Router, h *AuthHandler) {
	admin.HandleFunc("/admin/users/send-email-verification", h.SendEmailVerification).Methods(http.MethodPost)
	admin.HandleFunc("/admin/users/register", h.AdminRegister).Methods(http.MethodPost)
}
