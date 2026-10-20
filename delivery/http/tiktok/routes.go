package tiktok

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

const WebhookPath = "/webhooks/tiktok"

type AccessControl func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, ac AccessControl) {
	if h == nil || ac == nil {
		return
	}
	res := workspace_domain.ResourceTikTokAccounts
	read := func(handler http.HandlerFunc) http.HandlerFunc { return ac(res, workspace_domain.ActionRead, handler) }
	update := func(handler http.HandlerFunc) http.HandlerFunc { return ac(res, workspace_domain.ActionUpdate, handler) }

	protected.HandleFunc(OAuthStartPath, ac(res, workspace_domain.ActionCreate, h.StartConnect)).Methods(http.MethodGet)

	tt := protected.PathPrefix("/tiktok/accounts").Subrouter()
	tt.HandleFunc("", read(h.ListAccounts)).Methods(http.MethodGet)
	tt.HandleFunc("/{id}", read(h.GetAccount)).Methods(http.MethodGet)
	tt.HandleFunc("/{id}", update(h.UpdateAccount)).Methods(http.MethodPut)
	tt.HandleFunc("/{id}", ac(res, workspace_domain.ActionDelete, h.DisconnectAccount)).Methods(http.MethodDelete)
	tt.HandleFunc("/{id}/health-check", update(h.CheckHealth)).Methods(http.MethodPost)
	tt.HandleFunc("/{id}/videos", read(h.ListVideos)).Methods(http.MethodGet)
	tt.HandleFunc("/{id}/videos/{videoId}/comments", read(h.ListComments)).Methods(http.MethodGet)
	tt.HandleFunc("/{id}/comments/{commentId}/replies", update(h.ReplyComment)).Methods(http.MethodPost)
	tt.HandleFunc("/{id}/comments/{commentId}/hide", update(h.HideComment)).Methods(http.MethodPost)
	tt.HandleFunc("/{id}/comments/{commentId}/unhide", update(h.UnhideComment)).Methods(http.MethodPost)
	tt.HandleFunc("/{id}/comments/{commentId}/like", update(h.LikeComment)).Methods(http.MethodPost)
	tt.HandleFunc("/{id}/comments/{commentId}/unlike", update(h.UnlikeComment)).Methods(http.MethodPost)
	tt.HandleFunc("/{id}/comment-rules", read(h.ListCommentRules)).Methods(http.MethodGet)
	tt.HandleFunc("/{id}/comment-rules", update(h.CreateCommentRule)).Methods(http.MethodPost)
	tt.HandleFunc("/{id}/comment-rules/{ruleId}", update(h.UpdateCommentRule)).Methods(http.MethodPut)
	tt.HandleFunc("/{id}/comment-rules/{ruleId}", update(h.DeleteCommentRule)).Methods(http.MethodDelete)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler, webhooks *WebhookHandler) {
	if h != nil {
		public.HandleFunc(OAuthCallbackPath, h.HandleCallback).Methods(http.MethodGet)
	}
	if webhooks != nil {
		public.HandleFunc(WebhookPath, webhooks.Handle).Methods(http.MethodPost)
	}
}
