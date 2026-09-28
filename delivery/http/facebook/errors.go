package facebook

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/commentautomationhttp"
	"vozko/delivery/http/response"
	fbdomain "vozko/domain/facebook"
	fbuc "vozko/usecases/facebook"
)

type errorMapping struct {
	target  error
	status  int
	code    string
	message string
}

var domainErrors = []errorMapping{
	{fbdomain.ErrPageNotFound, http.StatusNotFound, "not_found", "Facebook page not found"},
	{fbdomain.ErrContactNotFound, http.StatusNotFound, "not_found", "Contact not found"},
	{fbdomain.ErrConversationNotFound, http.StatusNotFound, "not_found", "Conversation not found"},
	{fbdomain.ErrGrantNotFound, http.StatusNotFound, "not_found", "Facebook connection not found"},
	{fbdomain.ErrPageAlreadyLinked, http.StatusConflict, "already_linked", "This Facebook page is already connected to another workspace"},
	{fbdomain.ErrCapabilityDenied, http.StatusForbidden, "capability_denied", "The page did not grant the permission or task this action needs"},
	{fbdomain.ErrThreadOwnedElsewhere, http.StatusConflict, "thread_owned_elsewhere", "Another app controls this conversation"},
	{fbdomain.ErrWorkspaceIDRequired, http.StatusBadRequest, "invalid_request", "Workspace is required"},
	{fbdomain.ErrPostNotFound, http.StatusNotFound, "not_found", "Facebook post not found"},
	{fbdomain.ErrProfileRateLimited, http.StatusTooManyRequests, "profile_rate_limited", "Messenger profile changes are limited to 10 every 10 minutes per page"},
	{fbdomain.ErrReelLimit, http.StatusTooManyRequests, "reel_limit", "Facebook allows 30 reels per page every 24 hours"},
	{fbdomain.ErrCommentNotFound, http.StatusNotFound, "not_found", "Comment not found"},
	{fbdomain.ErrCommentNotOurs, http.StatusConflict, "comment_not_ours", "Only comments the page wrote can be edited"},
	{fbdomain.ErrCommentEmpty, http.StatusBadRequest, "invalid_request", "Comment text is required"},
	{fbdomain.ErrTextTooLong, http.StatusBadRequest, "text_too_long", "Messenger text is limited to 2000 characters"},
	{fbdomain.ErrPublishJobNotFound, http.StatusNotFound, "not_found", "Publish job not found"},
	{fbdomain.ErrPostNotEditable, http.StatusConflict, "post_not_editable", "Only posts published through Vozko can be edited"},
	{fbdomain.ErrPublishKindUnavailable, http.StatusBadRequest, "kind_unavailable", "This post kind is not available yet"},
	{fbuc.ErrConversationAccessDenied, http.StatusForbidden, "forbidden", "You don't have access to this conversation"},
}

func writeDomainError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, fbdomain.ErrInvalidPost):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_post", err.Error(), nil)
		return
	case errors.Is(err, fbdomain.ErrInvalidProfile):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_profile", err.Error(), nil)
		return
	case commentautomationhttp.WriteError(w, err):
		return
	case errors.Is(err, fbdomain.ErrDeleteNotPermitted):
		response.WriteErrorWithCode(w, http.StatusConflict, "delete_not_permitted",
			"Facebook only allows deleting this post in Meta Business Suite", map[string]string{"manageUrl": fbdomain.ManagePostsURL})
		return
	}
	for _, m := range domainErrors {
		if errors.Is(err, m.target) {
			response.WriteErrorWithCode(w, m.status, m.code, m.message, nil)
			return
		}
	}
	if failure := fbdomain.Classify(err); failure != fbdomain.FailureUnknown {
		writeGraphFailure(w, failure)
		return
	}
	log.Printf("[facebook] %s: %v", fallback, err)
	response.WriteError(w, http.StatusInternalServerError, fallback, nil)
}

func writeGraphFailure(w http.ResponseWriter, failure fbdomain.Failure) {
	switch failure {
	case fbdomain.FailureReauth, fbdomain.FailureRoleLost:
		response.WriteErrorWithCode(w, http.StatusConflict, "reconnect_required", "The Facebook page needs to be reconnected", nil)
	case fbdomain.FailureRetryable:
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "facebook_busy", "Facebook is limiting requests; try again shortly", nil)
	case fbdomain.FailureAccessLevel:
		response.WriteErrorWithCode(w, http.StatusForbidden, "app_not_approved", "This Facebook feature is not approved for the app yet", nil)
	case fbdomain.FailurePageRestricted, fbdomain.FailurePolicyBlock:
		response.WriteErrorWithCode(w, http.StatusForbidden, "page_restricted", "Facebook restricted this page", nil)
	default:
		response.WriteErrorWithCode(w, http.StatusBadGateway, "facebook_rejected", "Facebook rejected the request", nil)
	}
}
