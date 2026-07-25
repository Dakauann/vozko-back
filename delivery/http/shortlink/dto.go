package shortlink

import (
	"strings"
	"time"

	"vozko/domain/shortlink"
)

type createShortLinkRequest struct {
	TargetURL    string  `json:"targetUrl" example:"https://example.com/very/long/path"`
	CustomAlias  string  `json:"customAlias,omitempty" example:"promo-julho"`
	Title        string  `json:"title,omitempty" example:"Campanha de Julho"`
	RedirectType string  `json:"redirectType,omitempty" example:"302"`
	Password     string  `json:"password,omitempty"`
	DepartmentID *string `json:"departmentId,omitempty"`
	ExpiresAt    *string `json:"expiresAt,omitempty" example:"2026-12-31T23:59:59Z"`
	MaxClicks    *int64  `json:"maxClicks,omitempty" example:"1000"`
}

func (r createShortLinkRequest) Validate() map[string]string {
	errs := map[string]string{}
	if strings.TrimSpace(r.TargetURL) == "" {
		errs["targetUrl"] = "string (required)"
	}
	if r.RedirectType != "" && r.RedirectType != "301" && r.RedirectType != "302" {
		errs["redirectType"] = "string ('301' | '302')"
	}
	if !validExpiresAt(r.ExpiresAt) {
		errs["expiresAt"] = "string (RFC3339 datetime)"
	}
	if r.MaxClicks != nil && *r.MaxClicks <= 0 {
		errs["maxClicks"] = "integer (> 0)"
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (r createShortLinkRequest) toInput(workspaceID, createdBy string) shortlink.CreateShortLinkInput {
	return shortlink.CreateShortLinkInput{
		WorkspaceID:  workspaceID,
		DepartmentID: normalizeDepartmentPtr(r.DepartmentID),
		CreatedBy:    createdBy,
		TargetURL:    strings.TrimSpace(r.TargetURL),
		CustomAlias:  strings.TrimSpace(r.CustomAlias),
		Title:        strings.TrimSpace(r.Title),
		RedirectType: r.RedirectType,
		Password:     r.Password,
		ExpiresAt:    parseExpiresAt(r.ExpiresAt),
		MaxClicks:    r.MaxClicks,
	}
}

type updateShortLinkRequest struct {
	TargetURL      *string `json:"targetUrl,omitempty"`
	Title          *string `json:"title,omitempty"`
	RedirectType   *string `json:"redirectType,omitempty"`
	Status         *string `json:"status,omitempty" example:"inactive"`
	Password       *string `json:"password,omitempty"`
	ExpiresAt      *string `json:"expiresAt,omitempty"`
	MaxClicks      *int64  `json:"maxClicks,omitempty"`
	ClearPassword  bool    `json:"clearPassword,omitempty"`
	ClearExpiry    bool    `json:"clearExpiry,omitempty"`
	ClearMaxClicks bool    `json:"clearMaxClicks,omitempty"`
}

func (r updateShortLinkRequest) Validate() map[string]string {
	errs := map[string]string{}
	if r.RedirectType != nil && *r.RedirectType != "301" && *r.RedirectType != "302" {
		errs["redirectType"] = "string ('301' | '302')"
	}
	if r.Status != nil && *r.Status != "active" && *r.Status != "inactive" {
		errs["status"] = "string ('active' | 'inactive')"
	}
	if !validExpiresAt(r.ExpiresAt) {
		errs["expiresAt"] = "string (RFC3339 datetime)"
	}
	if r.MaxClicks != nil && *r.MaxClicks <= 0 {
		errs["maxClicks"] = "integer (> 0)"
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (r updateShortLinkRequest) toInput() shortlink.UpdateShortLinkInput {
	return shortlink.UpdateShortLinkInput{
		TargetURL:      trimmedPtr(r.TargetURL),
		Title:          r.Title,
		RedirectType:   r.RedirectType,
		Status:         r.Status,
		Password:       r.Password,
		ExpiresAt:      parseExpiresAt(r.ExpiresAt),
		MaxClicks:      r.MaxClicks,
		ClearPassword:  r.ClearPassword,
		ClearExpiry:    r.ClearExpiry,
		ClearMaxClicks: r.ClearMaxClicks,
	}
}

func validExpiresAt(value *string) bool {
	if value == nil || strings.TrimSpace(*value) == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(*value))
	return err == nil
}

func parseExpiresAt(value *string) *time.Time {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*value))
	if err != nil {
		return nil
	}
	return &parsed
}

func trimmedPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func normalizeDepartmentPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ---- Response DTOs ----
// Delivery-layer representations of the short-link responses. They mirror the
// domain JSON field-for-field (same tags, nil-ness preserved) so the wire output
// is unchanged, while keeping the API contract owned by this package.

type shortLinkResponse struct {
	ID               string     `json:"id" example:"c7f1e2a0-9b3d-4a1e-8f2c-1d2e3f4a5b6c"`
	WorkspaceID      string     `json:"workspaceId"`
	DepartmentID     string     `json:"departmentId,omitempty"`
	CreatedBy        string     `json:"createdBy,omitempty"`
	Code             string     `json:"code" example:"promo-julho"`
	ShortURL         string     `json:"shortUrl,omitempty" example:"https://example.com/r/promo-julho"`
	TargetURL        string     `json:"targetUrl" example:"https://example.com/destino"`
	Title            string     `json:"title,omitempty" example:"Campanha de Julho"`
	RedirectType     string     `json:"redirectType" enums:"301,302" example:"302"`
	Status           string     `json:"status" enums:"active,inactive" example:"active"`
	HasPassword      bool       `json:"hasPassword"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	MaxClicks        *int64     `json:"maxClicks,omitempty"`
	ClickCount       int64      `json:"clickCount"`
	UniqueClickCount int64      `json:"uniqueClickCount"`
	LastClickedAt    *time.Time `json:"lastClickedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

func newShortLinkResponse(l *shortlink.ShortLink) shortLinkResponse {
	return shortLinkResponse{
		ID:               l.ID,
		WorkspaceID:      l.WorkspaceID,
		DepartmentID:     l.DepartmentID,
		CreatedBy:        l.CreatedBy,
		Code:             l.Code,
		ShortURL:         l.ShortURL,
		TargetURL:        l.TargetURL,
		Title:            l.Title,
		RedirectType:     string(l.RedirectType),
		Status:           string(l.Status),
		HasPassword:      l.HasPassword,
		ExpiresAt:        l.ExpiresAt,
		MaxClicks:        l.MaxClicks,
		ClickCount:       l.ClickCount,
		UniqueClickCount: l.UniqueClickCount,
		LastClickedAt:    l.LastClickedAt,
		CreatedAt:        l.CreatedAt,
		UpdatedAt:        l.UpdatedAt,
	}
}

func newShortLinkResponses(links []*shortlink.ShortLink) []shortLinkResponse {
	out := make([]shortLinkResponse, 0, len(links))
	for _, l := range links {
		out = append(out, newShortLinkResponse(l))
	}
	return out
}

type clickResponse struct {
	ID            string    `json:"id"`
	ShortLinkID   string    `json:"shortLinkId"`
	WorkspaceID   string    `json:"workspaceId"`
	OccurredAt    time.Time `json:"occurredAt"`
	Country       string    `json:"country,omitempty"`
	Region        string    `json:"region,omitempty"`
	City          string    `json:"city,omitempty"`
	DeviceType    string    `json:"deviceType,omitempty"`
	OS            string    `json:"os,omitempty"`
	Browser       string    `json:"browser,omitempty"`
	RefererDomain string    `json:"refererDomain,omitempty"`
	UTMSource     string    `json:"utmSource,omitempty"`
	UTMMedium     string    `json:"utmMedium,omitempty"`
	UTMCampaign   string    `json:"utmCampaign,omitempty"`
	IsBot         bool      `json:"isBot"`
	IsProxy       bool      `json:"isProxy"`
	Language      string    `json:"language,omitempty"`
}

func newClickResponse(c *shortlink.Click) clickResponse {
	return clickResponse{
		ID:            c.ID,
		ShortLinkID:   c.ShortLinkID,
		WorkspaceID:   c.WorkspaceID,
		OccurredAt:    c.OccurredAt,
		Country:       c.Country,
		Region:        c.Region,
		City:          c.City,
		DeviceType:    c.DeviceType,
		OS:            c.OS,
		Browser:       c.Browser,
		RefererDomain: c.RefererDomain,
		UTMSource:     c.UTMSource,
		UTMMedium:     c.UTMMedium,
		UTMCampaign:   c.UTMCampaign,
		IsBot:         c.IsBot,
		IsProxy:       c.IsProxy,
		Language:      c.Language,
	}
}

// newClickResponses preserves nil-ness so an empty result stays consistent with
// the previous domain-typed output.
func newClickResponses(clicks []*shortlink.Click) []clickResponse {
	if clicks == nil {
		return nil
	}
	out := make([]clickResponse, 0, len(clicks))
	for _, c := range clicks {
		out = append(out, newClickResponse(c))
	}
	return out
}

type workspaceStatsResponse struct {
	TotalLinks  int   `json:"totalLinks"`
	TotalClicks int64 `json:"totalClicks"`
}

func newWorkspaceStatsResponse(s *shortlink.WorkspaceClickStats) workspaceStatsResponse {
	return workspaceStatsResponse{TotalLinks: s.TotalLinks, TotalClicks: s.TotalClicks}
}

type timePointResponse struct {
	Date   string `json:"date"`
	Clicks int64  `json:"clicks"`
}

type dimensionCountResponse struct {
	Label  string `json:"label"`
	Clicks int64  `json:"clicks"`
}

type analyticsResponse struct {
	TotalClicks  int64                    `json:"totalClicks"`
	UniqueClicks int64                    `json:"uniqueClicks"`
	TimeSeries   []timePointResponse      `json:"timeSeries"`
	ByCountry    []dimensionCountResponse `json:"byCountry"`
	ByDevice     []dimensionCountResponse `json:"byDevice"`
	ByReferer    []dimensionCountResponse `json:"byReferer"`
	ByBrowser    []dimensionCountResponse `json:"byBrowser"`
	ByOS         []dimensionCountResponse `json:"byOs"`
}

func newAnalyticsResponse(a *shortlink.Analytics) analyticsResponse {
	resp := analyticsResponse{
		TotalClicks:  a.TotalClicks,
		UniqueClicks: a.UniqueClicks,
		ByCountry:    newDimensionCounts(a.ByCountry),
		ByDevice:     newDimensionCounts(a.ByDevice),
		ByReferer:    newDimensionCounts(a.ByReferer),
		ByBrowser:    newDimensionCounts(a.ByBrowser),
		ByOS:         newDimensionCounts(a.ByOS),
	}
	if a.TimeSeries != nil {
		resp.TimeSeries = make([]timePointResponse, 0, len(a.TimeSeries))
		for _, p := range a.TimeSeries {
			resp.TimeSeries = append(resp.TimeSeries, timePointResponse{Date: p.Date, Clicks: p.Clicks})
		}
	}
	return resp
}

func newDimensionCounts(in []shortlink.DimensionCount) []dimensionCountResponse {
	if in == nil {
		return nil
	}
	out := make([]dimensionCountResponse, 0, len(in))
	for _, d := range in {
		out = append(out, dimensionCountResponse{Label: d.Label, Clicks: d.Clicks})
	}
	return out
}
