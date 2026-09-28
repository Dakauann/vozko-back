package facebook

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/metawebhook"
	fbdomain "vozko/domain/facebook"
	workspace_domain "vozko/domain/workspace"
)

type AccessControl func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, ac AccessControl) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceFacebookPages
	read, update, create, del := workspace_domain.ActionRead, workspace_domain.ActionUpdate, workspace_domain.ActionCreate, workspace_domain.ActionDelete

	protected.HandleFunc(fbdomain.OAuthStartPath, ac(res, create, h.StartConnect)).Methods(http.MethodGet)

	fb := protected.PathPrefix("/facebook").Subrouter()
	fb.HandleFunc("/pages", ac(res, read, h.ListPages)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}", ac(res, read, h.GetPage)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}", ac(res, update, h.UpdatePage)).Methods(http.MethodPut)
	fb.HandleFunc("/pages/{id}", ac(res, del, h.DisconnectPage)).Methods(http.MethodDelete)
	fb.HandleFunc("/pages/{id}/health-check", ac(res, update, h.CheckHealth)).Methods(http.MethodPost)

	fb.HandleFunc("/pages/{id}/posts", ac(res, read, h.ListPosts)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/posts", ac(res, update, h.CreatePost)).Methods(http.MethodPost)
	fb.HandleFunc("/pages/{id}/posts/{postId}", ac(res, read, h.GetPost)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/posts/{postId}", ac(res, update, h.UpdatePost)).Methods(http.MethodPatch)
	fb.HandleFunc("/pages/{id}/posts/{postId}", ac(res, update, h.DeletePost)).Methods(http.MethodDelete)
	fb.HandleFunc("/pages/{id}/posts/{postId}/asset", ac(res, read, h.PostAsset)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/stories", ac(res, read, h.ListStories)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/messenger-profile", ac(res, read, h.GetMessengerProfile)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/messenger-profile", ac(res, update, h.UpdateMessengerProfile)).Methods(http.MethodPut)
	fb.HandleFunc("/pages/{id}/publish-jobs", ac(res, read, h.ListPublishJobs)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/publish-jobs/{jobId}", ac(res, read, h.GetPublishJob)).Methods(http.MethodGet)

	fb.HandleFunc("/pages/{id}/posts/{postId}/comments", ac(res, read, h.ListComments)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/posts/{postId}/comments", ac(res, update, h.CommentOnPost)).Methods(http.MethodPost)
	fb.HandleFunc("/pages/{id}/comments/{commentId}/replies", ac(res, update, h.ReplyToComment)).Methods(http.MethodPost)
	fb.HandleFunc("/pages/{id}/comments/{commentId}", ac(res, update, h.EditComment)).Methods(http.MethodPatch)
	fb.HandleFunc("/pages/{id}/comments/{commentId}", ac(res, update, h.DeleteComment)).Methods(http.MethodDelete)
	fb.HandleFunc("/pages/{id}/comments/{commentId}/hide", ac(res, update, h.HideComment)).Methods(http.MethodPost)
	fb.HandleFunc("/pages/{id}/comments/{commentId}/like", ac(res, update, h.LikeComment)).Methods(http.MethodPost)
	fb.HandleFunc("/pages/{id}/comments/{commentId}/private-reply", ac(res, update, h.SendCommentPrivateReply)).Methods(http.MethodPost)

	fb.HandleFunc("/pages/{id}/comment-rules", ac(res, read, h.ListCommentRules)).Methods(http.MethodGet)
	fb.HandleFunc("/pages/{id}/comment-rules", ac(res, update, h.CreateCommentRule)).Methods(http.MethodPost)
	fb.HandleFunc("/pages/{id}/comment-rules/{ruleId}", ac(res, update, h.UpdateCommentRule)).Methods(http.MethodPut)
	fb.HandleFunc("/pages/{id}/comment-rules/{ruleId}", ac(res, update, h.DeleteCommentRule)).Methods(http.MethodDelete)

	conversations, send := workspace_domain.ResourceConversations, workspace_domain.ActionSend
	fb.HandleFunc("/conversations/{id}/thread", ac(conversations, workspace_domain.ActionRead, h.ThreadState)).Methods(http.MethodGet)
	fb.HandleFunc("/conversations/{id}/take-control", ac(conversations, send, h.TakeThreadControl)).Methods(http.MethodPost)
	fb.HandleFunc("/conversations/{id}/release-control", ac(conversations, send, h.ReleaseThreadControl)).Methods(http.MethodPost)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler, wh *metawebhook.Handler) {
	if h != nil {
		for _, path := range []string{fbdomain.OAuthCallbackPath, fbdomain.OAuthCallbackPath + "/"} {
			public.HandleFunc(path, h.HandleCallback).Methods(http.MethodGet)
		}
	}
	if wh != nil {
		public.HandleFunc("/webhooks/facebook", wh.Handle).Methods(http.MethodGet, http.MethodPost)
	}
}
