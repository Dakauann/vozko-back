package instagram

import (
	"net/http"

	"github.com/gorilla/mux"

	igdomain "vozko/domain/instagram"
	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}

	res := workspace_domain.ResourceInstagramAccounts

	protected.HandleFunc(igdomain.OAuthStartPath,
		ac(res, workspace_domain.ActionCreate, h.StartConnect)).Methods(http.MethodGet)

	ig := protected.PathPrefix("/instagram").Subrouter()

	ig.HandleFunc("/accounts", ac(res, workspace_domain.ActionRead, h.ListAccounts)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionRead, h.GetAccount)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateAccount)).Methods(http.MethodPut)
	ig.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionDelete, h.DisconnectAccount)).Methods(http.MethodDelete)
	ig.HandleFunc("/accounts/{id}/avatar", ac(res, workspace_domain.ActionRead, h.ProxyAvatar)).Methods(http.MethodGet)

	ig.HandleFunc("/accounts/{id}/media", ac(res, workspace_domain.ActionRead, h.ListMedia)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/media", ac(res, workspace_domain.ActionUpdate, h.CreateMedia)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/media/{mediaId}", ac(res, workspace_domain.ActionRead, h.GetMedia)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/media/{mediaId}", ac(res, workspace_domain.ActionUpdate, h.UpdateMedia)).Methods(http.MethodPatch)
	ig.HandleFunc("/accounts/{id}/media/{mediaId}/asset", ac(res, workspace_domain.ActionRead, h.ProxyMedia)).Methods(http.MethodGet)

	ig.HandleFunc("/accounts/{id}/media/{mediaId}/comments", ac(res, workspace_domain.ActionRead, h.ListComments)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}/replies", ac(res, workspace_domain.ActionUpdate, h.ReplyComment)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}/hide", ac(res, workspace_domain.ActionUpdate, h.HideComment)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}/private-reply", ac(res, workspace_domain.ActionUpdate, h.PrivateReply)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}", ac(res, workspace_domain.ActionUpdate, h.DeleteComment)).Methods(http.MethodDelete)

	ig.HandleFunc("/accounts/{id}/comment-rules", ac(res, workspace_domain.ActionRead, h.ListCommentRules)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/comment-rules", ac(res, workspace_domain.ActionUpdate, h.CreateCommentRule)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comment-rules/{ruleId}", ac(res, workspace_domain.ActionUpdate, h.UpdateCommentRule)).Methods(http.MethodPut)
	ig.HandleFunc("/accounts/{id}/comment-rules/{ruleId}", ac(res, workspace_domain.ActionUpdate, h.DeleteCommentRule)).Methods(http.MethodDelete)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler, wh *WebhookHandler) {
	if h != nil {
		for _, path := range []string{igdomain.OAuthCallbackPath, igdomain.OAuthCallbackPath + "/"} {
			public.HandleFunc(path, h.HandleCallback).Methods(http.MethodGet)
		}
	}
	if wh != nil {
		public.HandleFunc("/webhooks/instagram", wh.Handle).Methods(http.MethodGet, http.MethodPost)
	}
}
