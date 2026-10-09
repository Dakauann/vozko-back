package calllist

import "errors"

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrWorkspaceRequired, "call_list_workspace_required"},
	{ErrActorRequired, "call_list_actor_required"},
	{ErrForbidden, "forbidden"},
	{ErrUnavailable, "call_lists_unavailable"},
	{ErrNameRequired, "call_list_name_required"},
	{ErrNameTooLong, "call_list_name_too_long"},
	{ErrAssigneesRequired, "call_list_assignees_required"},
	{ErrTooManyAssignees, "call_list_too_many_assignees"},
	{ErrAssigneeCannotWork, "call_list_assignee_cannot_work"},
	{ErrPhoneChoiceInvalid, "call_list_phone_choice_invalid"},
	{ErrSelectionEmpty, "call_list_selection_empty"},
	{ErrSelectionTooLarge, "call_list_selection_too_large"},
	{ErrListNotFound, "call_list_not_found"},
	{ErrItemNotFound, "call_list_item_not_found"},
	{ErrListBuilding, "call_list_building"},
	{ErrListNotActive, "call_list_not_active"},
	{ErrNotAssignee, "call_list_not_assignee"},
	{ErrStatusTransition, "call_list_status_transition"},
	{ErrNothingToChange, "call_list_nothing_to_change"},
	{ErrReservationHeld, "call_list_reservation_held"},
	{ErrItemTaken, "call_list_item_taken"},
	{ErrItemNotReserved, "call_list_item_not_reserved"},
	{ErrItemMismatch, "call_list_item_mismatch"},
	{ErrItemClosed, "call_list_item_closed"},
	{ErrItemNotCalled, "call_list_item_not_called"},
	{ErrCallNotTheItems, "call_list_call_not_the_items"},
	{ErrDispositionRequired, "call_list_disposition_required"},
	{ErrDispositionUnknown, "call_list_disposition_unknown"},
	{ErrDispositionReserved, "call_list_disposition_reserved"},
	{ErrNoOutcomes, "call_list_no_outcomes"},
	{ErrCallbackTime, "call_list_callback_time"},
	{ErrNoteTooLong, "call_list_note_too_long"},
	{ErrBuildClaimLost, "call_list_build_claim_lost"},
	{ErrBuildCursorInvalid, "call_list_build_cursor_invalid"},
	{ErrItemCursorInvalid, "call_list_items_cursor_invalid"},
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
