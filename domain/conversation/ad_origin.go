package conversation

import (
	"context"
	"net/url"
	"strings"
	"time"

	"vozko/domain/shared"
)

type AdPlatform string

const (
	AdPlatformUnknown   AdPlatform = ""
	AdPlatformFacebook  AdPlatform = "facebook"
	AdPlatformInstagram AdPlatform = "instagram"
)

func AdPlatformFromURL(raw string) AdPlatform {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return AdPlatformUnknown
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "instagram.com" || strings.HasSuffix(host, ".instagram.com"):
		return AdPlatformInstagram
	case host == "fb.me" || host == "fb.com" || host == "facebook.com" || strings.HasSuffix(host, ".facebook.com"):
		return AdPlatformFacebook
	}
	return AdPlatformUnknown
}

type AdReferral struct {
	AdID      string
	Platform  AdPlatform
	Title     string
	SourceURL string
	ImageURL   string
	Image      []byte
	ClickID    string
	SourceType string
}

func (r *AdReferral) Usable() bool {
	return r != nil && (strings.TrimSpace(r.AdID) != "" || strings.TrimSpace(r.Title) != "")
}

func (r *WhatsAppReferral) AdReferral() *AdReferral {
	if r == nil {
		return nil
	}
	ad := &AdReferral{
		AdID:      strings.TrimSpace(r.SourceID),
		Platform:  AdPlatformFromURL(r.SourceURL),
		Title:     firstNonBlank(r.Headline, r.Body),
		SourceURL: strings.TrimSpace(r.SourceURL),
		ImageURL:   firstNonBlank(r.ImageURL, r.ThumbnailURL),
		ClickID:    strings.TrimSpace(r.CTWAClid),
		SourceType: strings.TrimSpace(r.SourceType),
	}
	if !ad.Usable() {
		return nil
	}
	return ad
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type AdOrigin struct {
	EntryID      string           `json:"-"`
	EntryType    shared.EntryType `json:"-"`
	AdID         string           `json:"adId,omitempty"`
	Platform     AdPlatform       `json:"platform,omitempty"`
	Title        string           `json:"title,omitempty"`
	SourceURL    string           `json:"sourceUrl,omitempty"`
	ClickID      string           `json:"-"`
	SourceType   string           `json:"-"`
	ImageMediaID string           `json:"-"`
	Image        *AttachedMedia   `json:"image,omitempty"`
	ArrivedAt    time.Time        `json:"arrivedAt"`
}

type AdOriginRepository interface {
	Claim(origin *AdOrigin) (bool, error)
	Get(entryID string, entryType shared.EntryType) (*AdOrigin, error)
}

type AdOriginRecorder interface {
	Record(ctx context.Context, entryID string, entryType shared.EntryType, ad *AdReferral)
}

type AdOriginReader interface {
	AdOrigin(entryID string, entryType shared.EntryType) (*AdOrigin, error)
}

type AdOriginBroadcaster interface {
	BroadcastAdOrigin(entryID string, entryType shared.EntryType, origin *AdOrigin)
}

type RemoteFileFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}
