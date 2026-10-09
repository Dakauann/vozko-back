package calllisthttp

import (
	"errors"
	"net/http"

	"vozko/domain/calls/calllist"
)

var refusalStatuses = []struct {
	err    error
	status int
}{
	{calllist.ErrForbidden, http.StatusForbidden},
	{calllist.ErrNotAssignee, http.StatusForbidden},
	{calllist.ErrListNotFound, http.StatusNotFound},
	{calllist.ErrItemNotFound, http.StatusNotFound},
	{calllist.ErrUnavailable, http.StatusServiceUnavailable},
	{calllist.ErrSelectionTooLarge, http.StatusRequestEntityTooLarge},
	{calllist.ErrAssigneeCannotWork, http.StatusUnprocessableEntity},
	{calllist.ErrNoOutcomes, http.StatusUnprocessableEntity},
	{calllist.ErrListBuilding, http.StatusConflict},
	{calllist.ErrListNotActive, http.StatusConflict},
	{calllist.ErrStatusTransition, http.StatusConflict},
	{calllist.ErrReservationHeld, http.StatusConflict},
	{calllist.ErrItemTaken, http.StatusConflict},
	{calllist.ErrItemNotReserved, http.StatusConflict},
	{calllist.ErrItemClosed, http.StatusConflict},
	{calllist.ErrItemNotCalled, http.StatusConflict},
	{calllist.ErrCallNotTheItems, http.StatusConflict},
	{calllist.ErrBuildClaimLost, http.StatusConflict},
}

func Refusal(err error) (int, string, bool) {
	code := calllist.ErrorCode(err)
	if code == "" {
		return 0, "", false
	}
	for _, known := range refusalStatuses {
		if errors.Is(err, known.err) {
			return known.status, code, true
		}
	}
	return http.StatusBadRequest, code, true
}
