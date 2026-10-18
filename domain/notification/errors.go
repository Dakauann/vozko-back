package notification

import "errors"

var (
	ErrWorkspaceRequired  = errors.New("notification: the workspace is required")
	ErrViewerRequired     = errors.New("notification: the person reading is required")
	ErrCategoryUnknown    = errors.New("notification: the category is not in the catalog")
	ErrSeverityUnknown    = errors.New("notification: the severity is unknown")
	ErrTitleRequired      = errors.New("notification: the title is required")
	ErrTitleTooLong       = errors.New("notification: the title is too long")
	ErrBodyTooLong        = errors.New("notification: the text is too long")
	ErrActionURLInvalid   = errors.New("notification: the link must be a path of the app or an https address")
	ErrActionLabelTooLong = errors.New("notification: the link label is too long")
	ErrImageForeign       = errors.New("notification: the image must be one uploaded to Vozko")
	ErrAudienceInvalid    = errors.New("notification: the audience is invalid")
	ErrDedupKeyTooLong    = errors.New("notification: the dedup key is too long")
	ErrIDsInvalid         = errors.New("notification: send between 1 and 100 notification ids")
	ErrImageEmpty         = errors.New("notification: the image is empty")
	ErrImageTooLarge      = errors.New("notification: the image is larger than 5 MB")
	ErrImageType          = errors.New("notification: the image must be PNG, JPEG, WebP or GIF")
	ErrTargetInvalid      = errors.New("notification: choose all workspaces or up to 1000 workspaces")
	ErrCursorInvalid      = errors.New("notification: the cursor was not given by this server")
	ErrNotFound           = errors.New("notification: not found")
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrWorkspaceRequired, "notification_workspace_required"},
	{ErrViewerRequired, "notification_viewer_required"},
	{ErrCategoryUnknown, "notification_category_unknown"},
	{ErrSeverityUnknown, "notification_severity_unknown"},
	{ErrTitleRequired, "notification_title_required"},
	{ErrTitleTooLong, "notification_title_too_long"},
	{ErrBodyTooLong, "notification_body_too_long"},
	{ErrActionURLInvalid, "notification_action_url_invalid"},
	{ErrActionLabelTooLong, "notification_action_label_too_long"},
	{ErrImageForeign, "notification_image_foreign"},
	{ErrAudienceInvalid, "notification_audience_invalid"},
	{ErrDedupKeyTooLong, "notification_dedup_key_too_long"},
	{ErrIDsInvalid, "notification_ids_invalid"},
	{ErrImageEmpty, "notification_image_empty"},
	{ErrImageTooLarge, "notification_image_too_large"},
	{ErrImageType, "notification_image_type"},
	{ErrTargetInvalid, "notification_target_invalid"},
	{ErrCursorInvalid, "notification_cursor_invalid"},
	{ErrNotFound, "notification_not_found"},
	{ErrReadStatusUnknown, "notification_status_invalid"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

func IsRefusal(err error) bool {
	return ErrorCode(err) != ""
}
