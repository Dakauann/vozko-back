package mercadolivre

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterCatalogRoutes(
	protected *mux.Router,
	h *CatalogHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceMercadoLivreAccounts
	read, update := workspace_domain.ActionRead, workspace_domain.ActionUpdate
	const account = "/mercadolivre/accounts/{id}"

	protected.HandleFunc(account+"/listings", ac(res, read, h.ListListings)).Methods(http.MethodGet)
	protected.HandleFunc(account+"/listings/{itemId}/thumbnail", ac(res, read, h.ListingThumbnail)).Methods(http.MethodGet)
	protected.HandleFunc(account+"/listings/{itemId}/questions", ac(res, read, h.ListListingQuestions)).Methods(http.MethodGet)

	protected.HandleFunc(account+"/questions", ac(res, read, h.ListQuestions)).Methods(http.MethodGet)
	protected.HandleFunc(account+"/questions/response-time", ac(res, read, h.ResponseTime)).Methods(http.MethodGet)
	protected.HandleFunc(account+"/questions/{questionId}", ac(res, read, h.GetQuestion)).Methods(http.MethodGet)
	protected.HandleFunc(account+"/questions/{questionId}/answer", ac(res, update, h.AnswerQuestion)).Methods(http.MethodPost)
	protected.HandleFunc(account+"/questions/{questionId}/block-author", ac(res, update, h.BlockAuthor)).Methods(http.MethodPost)
	protected.HandleFunc(account+"/questions/{questionId}/block-author", ac(res, update, h.UnblockAuthor)).Methods(http.MethodDelete)
	protected.HandleFunc(account+"/questions/{questionId}", ac(res, update, h.DeleteQuestion)).Methods(http.MethodDelete)

	protected.HandleFunc(account+"/comment-rules", ac(res, read, h.ListCommentRules)).Methods(http.MethodGet)
	protected.HandleFunc(account+"/comment-rules", ac(res, update, h.CreateCommentRule)).Methods(http.MethodPost)
	protected.HandleFunc(account+"/comment-rules/{ruleId}", ac(res, update, h.UpdateCommentRule)).Methods(http.MethodPatch)
	protected.HandleFunc(account+"/comment-rules/{ruleId}", ac(res, update, h.DeleteCommentRule)).Methods(http.MethodDelete)

	conversations := workspace_domain.ResourceConversations
	protected.HandleFunc("/mercadolivre/conversations/{entryId}/reasons", ac(conversations, read, h.ListReasons)).Methods(http.MethodGet)
	protected.HandleFunc("/mercadolivre/conversations/{entryId}/reasons", ac(conversations, workspace_domain.ActionSend, h.SendReason)).Methods(http.MethodPost)
	protected.HandleFunc("/mercadolivre/conversations/{entryId}/pack", ac(conversations, read, h.PackSummary)).Methods(http.MethodGet)
}
