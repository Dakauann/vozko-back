package teamchathttp

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

type refusal struct {
	err    error
	status int
	code   string
}

var refusals = []refusal{
	{teamchat.ErrForbidden, http.StatusForbidden, "forbidden"},
	{teamchat.ErrNotFound, http.StatusNotFound, "thread_not_found"},
	{teamchat.ErrMessageNotFound, http.StatusNotFound, "message_not_found"},
	{teamchat.ErrScheduledNotFound, http.StatusNotFound, "scheduled_not_found"},
	{teamchat.ErrScheduledFinal, http.StatusConflict, "scheduled_final"},
	{teamchat.ErrNotAuthor, http.StatusForbidden, "not_author"},
	{teamchat.ErrNotAdmin, http.StatusForbidden, "not_admin"},
	{teamchat.ErrDirectThread, http.StatusConflict, "direct_thread"},
	{teamchat.ErrSystemMessage, http.StatusConflict, "system_message"},
	{teamchat.ErrRecipientUnavailable, http.StatusConflict, "recipient_unavailable"},
	{teamchat.ErrMemberUnavailable, http.StatusConflict, "member_unavailable"},
	{teamchat.ErrWorkspaceRequired, http.StatusBadRequest, "workspace_required"},
	{teamchat.ErrPersonRequired, http.StatusBadRequest, "person_required"},
	{teamchat.ErrSamePerson, http.StatusBadRequest, "same_person"},
	{teamchat.ErrTitleRequired, http.StatusBadRequest, "title_required"},
	{teamchat.ErrTitleTooLong, http.StatusBadRequest, "title_too_long"},
	{teamchat.ErrMembersRequired, http.StatusBadRequest, "members_required"},
	{teamchat.ErrTooManyMembers, http.StatusBadRequest, "too_many_members"},
	{teamchat.ErrBodyRequired, http.StatusBadRequest, "body_required"},
	{teamchat.ErrBodyTooLong, http.StatusBadRequest, "body_too_long"},
	{teamchat.ErrTooManyMentions, http.StatusBadRequest, "too_many_mentions"},
	{teamchat.ErrClientIDTooLong, http.StatusBadRequest, "client_id_too_long"},
	{teamchat.ErrReplyOutsideThread, http.StatusBadRequest, "reply_outside_thread"},
	{teamchat.ErrPageDirection, http.StatusBadRequest, "page_direction"},
	{teamchat.ErrSendAtTooSoon, http.StatusBadRequest, "send_at_too_soon"},
	{teamchat.ErrSendAtTooFar, http.StatusBadRequest, "send_at_too_far"},
	{shared.ErrKeysetInvalid, http.StatusBadRequest, "invalid_cursor"},
}

func writeDomainError(w http.ResponseWriter, err error) {
	for _, r := range refusals {
		if errors.Is(err, r.err) {
			response.WriteErrorWithCode(w, r.status, r.code, r.err.Error(), nil)
			return
		}
	}
	response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
}
