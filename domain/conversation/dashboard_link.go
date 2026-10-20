package conversation

import (
	"net/url"

	"vozko/domain/shared"
)

func DashboardLink(entry shared.EntryRef) string {
	query := url.Values{}
	query.Set("entry", entry.EntryID)
	query.Set("type", string(entry.EntryType))
	return "/dashboard/live-chat?" + query.Encode()
}
