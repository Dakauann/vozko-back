package shortlink

import "time"

const (
	ClickExchange = "shortlink_click_exchange"
	ClickTopic    = "shortlink_click"

	MaxClickProcessingAttempts = 5
)

type ClickMessage struct {
	ClickEventID string    `json:"click_event_id"`
	ShortLinkID  string    `json:"short_link_id"`
	WorkspaceID  string    `json:"workspace_id"`
	Code         string    `json:"code"`
	OccurredAt   time.Time `json:"occurred_at"`
	IP           string    `json:"ip"`
	UserAgent    string    `json:"user_agent"`
	Referer      string    `json:"referer"`
	Language     string    `json:"language"`
	GeoCountry   string    `json:"geo_country"`
	GeoRegion    string    `json:"geo_region"`
	GeoCity      string    `json:"geo_city"`
	UTMSource    string    `json:"utm_source"`
	UTMMedium    string    `json:"utm_medium"`
	UTMCampaign  string    `json:"utm_campaign"`
	Attempt      int       `json:"attempt"`
}
