package notificationhttp

import (
	"context"
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/notification"
)

const (
	codeLimitInvalid  = "notification_limit_invalid"
	codeUnreadInvalid = "notification_unread_invalid"
	codeImageMissing  = "notification_image_missing"
)

var refusalStatuses = []struct {
	err    error
	status int
}{
	{notification.ErrViewerRequired, http.StatusUnauthorized},
	{notification.ErrWorkspaceRequired, http.StatusForbidden},
	{notification.ErrNotFound, http.StatusNotFound},
	{notification.ErrImageTooLarge, http.StatusRequestEntityTooLarge},
}

func statusOf(err error) int {
	for _, known := range refusalStatuses {
		if errors.Is(err, known.err) {
			return known.status
		}
	}
	return http.StatusBadRequest
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if notification.IsRefusal(err) {
		response.WriteErrorWithCode(w, statusOf(err), notification.ErrorCode(err), err.Error(), nil)
		return
	}
	log.Printf("[notifications] %v", err)
	response.WriteError(w, http.StatusInternalServerError, "Failed to handle notifications", nil)
}
