package instagram

import (
	"net/http"

	"github.com/gorilla/mux"

	igdomain "vozko/domain/instagram"
	workspace_domain "vozko/domain/workspace"
)

// RegisterProtectedRoutes wires the authenticated Instagram routes.
//
// Read/write is split along the RBAC actions already used elsewhere: reading posts
// and comments is ActionRead, publishing and moderating is ActionUpdate,
// connecting is ActionCreate, disconnecting is ActionDelete.
func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	// A nil handler means the channel is not wired; register nothing rather than
	// routes whose methods would nil-panic on the first request.
	if h == nil {
		return
	}

	res := workspace_domain.ResourceInstagramAccounts

	// Onboarding sits under /oauth/instagram/..., matching both the existing
	// /oauth/meta/embedded convention and, critically, the redirect URI
	// registered in the Meta App Dashboard. Meta validates redirect_uri by exact
	// string match, so this path is not free to change.
	protected.HandleFunc(igdomain.OAuthStartPath,
		ac(res, workspace_domain.ActionCreate, h.StartConnect)).Methods(http.MethodGet)

	ig := protected.PathPrefix("/instagram").Subrouter()

	// Accounts, posts and comments live under /instagram/...

	// Accounts.
	ig.HandleFunc("/accounts", ac(res, workspace_domain.ActionRead, h.ListAccounts)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionRead, h.GetAccount)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateAccount)).Methods(http.MethodPut)
	ig.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionDelete, h.DisconnectAccount)).Methods(http.MethodDelete)
	// Avatar proxy, for the same reason as the media proxy: profile_picture_url is
	// a signed CDN link that expires, so the browser cannot be handed it directly.
	ig.HandleFunc("/accounts/{id}/avatar", ac(res, workspace_domain.ActionRead, h.ProxyAvatar)).Methods(http.MethodGet)

	// Posts.
	ig.HandleFunc("/accounts/{id}/media", ac(res, workspace_domain.ActionRead, h.ListMedia)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/media", ac(res, workspace_domain.ActionUpdate, h.CreateMedia)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/media/{mediaId}", ac(res, workspace_domain.ActionRead, h.GetMedia)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/media/{mediaId}", ac(res, workspace_domain.ActionUpdate, h.UpdateMedia)).Methods(http.MethodPatch)
	// Asset proxy: Instagram's CDN URLs are signed and expire, so the browser
	// cannot be handed them directly.
	ig.HandleFunc("/accounts/{id}/media/{mediaId}/asset", ac(res, workspace_domain.ActionRead, h.ProxyMedia)).Methods(http.MethodGet)

	// Comments.
	ig.HandleFunc("/accounts/{id}/media/{mediaId}/comments", ac(res, workspace_domain.ActionRead, h.ListComments)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}/replies", ac(res, workspace_domain.ActionUpdate, h.ReplyComment)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}/hide", ac(res, workspace_domain.ActionUpdate, h.HideComment)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}/private-reply", ac(res, workspace_domain.ActionUpdate, h.PrivateReply)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comments/{commentId}", ac(res, workspace_domain.ActionUpdate, h.DeleteComment)).Methods(http.MethodDelete)

	// Comment automation rules. Read is a read; every mutation is an update on
	// the Instagram resource, matching how account config is gated.
	ig.HandleFunc("/accounts/{id}/comment-rules", ac(res, workspace_domain.ActionRead, h.ListCommentRules)).Methods(http.MethodGet)
	ig.HandleFunc("/accounts/{id}/comment-rules", ac(res, workspace_domain.ActionUpdate, h.CreateCommentRule)).Methods(http.MethodPost)
	ig.HandleFunc("/accounts/{id}/comment-rules/{ruleId}", ac(res, workspace_domain.ActionUpdate, h.UpdateCommentRule)).Methods(http.MethodPut)
	ig.HandleFunc("/accounts/{id}/comment-rules/{ruleId}", ac(res, workspace_domain.ActionUpdate, h.DeleteCommentRule)).Methods(http.MethodDelete)
}

// RegisterPublicRoutes wires the unauthenticated Instagram routes.
//
// Both are unauthenticated by necessity: Meta calls the webhook, and Instagram
// redirects the browser to the OAuth callback. The callback is protected instead by
// a signed, single-use state, and the webhook by the X-Hub-Signature-256 HMAC.
func RegisterPublicRoutes(public *mux.Router, h *Handler, wh *WebhookHandler) {
	if h != nil {
		// Registered with AND without a trailing slash.
		//
		// Meta matches redirect_uri by exact string, and its own docs warn that the
		// App Dashboard may silently append a trailing slash to a saved URI. If that
		// happens, the configured value becomes ".../callback/", which gorilla/mux
		// would 404 on, turning a one-character dashboard quirk into a dead
		// onboarding flow. Accepting both spellings removes that failure mode
		// entirely, whichever form ends up registered.
		for _, path := range []string{igdomain.OAuthCallbackPath, igdomain.OAuthCallbackPath + "/"} {
			public.HandleFunc(path, h.HandleCallback).Methods(http.MethodGet)
		}
	}
	if wh != nil {
		public.HandleFunc("/webhooks/instagram", wh.Handle).Methods(http.MethodGet, http.MethodPost)
	}
}
