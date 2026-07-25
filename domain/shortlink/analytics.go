package shortlink

import "time"

const (
	DimTotal   = "total"
	DimCountry = "country"
	DimDevice  = "device"
	DimReferer = "referer"
	DimBrowser = "browser"
	DimOS      = "os"
)

type AnalyticsInput struct {
	ShortLinkID string
	WorkspaceID string
	From        time.Time
	To          time.Time
}

type TimePoint struct {
	Date   string `json:"date"`
	Clicks int64  `json:"clicks"`
}

type DimensionCount struct {
	Label  string `json:"label"`
	Clicks int64  `json:"clicks"`
}

type Analytics struct {
	TotalClicks  int64            `json:"totalClicks"`
	UniqueClicks int64            `json:"uniqueClicks"`
	TimeSeries   []TimePoint      `json:"timeSeries"`
	ByCountry    []DimensionCount `json:"byCountry"`
	ByDevice     []DimensionCount `json:"byDevice"`
	ByReferer    []DimensionCount `json:"byReferer"`
	ByBrowser    []DimensionCount `json:"byBrowser"`
	ByOS         []DimensionCount `json:"byOs"`
}

type DailyStatDelta struct {
	ShortLinkID    string
	WorkspaceID    string
	Day            time.Time
	Dimension      string
	DimensionValue string
	Clicks         int64
	UniqueClicks   int64
}
