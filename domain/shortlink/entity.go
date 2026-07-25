package shortlink

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrShortLinkIDRequired  = errors.New("short link id is required")
	ErrWorkspaceIDRequired  = errors.New("workspace id is required")
	ErrCodeRequired         = errors.New("short link code is required")
	ErrShortLinkNotFound    = errors.New("short link not found")
	ErrCodeTaken            = errors.New("short link code already in use")
	ErrCodeGenerationFailed = errors.New("failed to generate a unique short link code")
	ErrMaxShortLinksReached = errors.New("maximum number of short links reached")

	ErrReservedAlias      = errors.New("short link alias is reserved")
	ErrInvalidAliasLength = errors.New("short link alias length is invalid")
	ErrInvalidAliasChar   = errors.New("short link alias contains invalid characters")

	ErrTargetURLRequired    = errors.New("target url is required")
	ErrTargetURLTooLong     = errors.New("target url exceeds the maximum length")
	ErrTargetURLInvalid     = errors.New("target url is not a valid url")
	ErrTargetURLScheme      = errors.New("target url must use http or https")
	ErrTargetURLCredentials = errors.New("target url must not contain embedded credentials")
	ErrTargetURLBlocked     = errors.New("target url points to a blocked or internal address")
	ErrTargetURLLoop        = errors.New("target url must not point back at the shortener")

	ErrInvalidRedirectType = errors.New("redirect type must be 301 or 302")
	ErrInvalidStatus       = errors.New("status must be active or inactive")
	ErrInvalidMaxClicks    = errors.New("max clicks must be greater than zero")
	ErrTitleTooLong        = errors.New("title exceeds the maximum length")

	ErrThreatDetected = errors.New("target url was flagged as malicious")

	ErrPasswordRequired = errors.New("password is required for this link")
	ErrInvalidPassword  = errors.New("invalid password")
)

const (
	MaxShortLinksPerWorkspace = 10000
	MaxTargetURLLength        = 2048
	MaxTitleLength            = 200
	MaxCodeGenerationAttempts = 6
)

type LinkStatus string

const (
	LinkStatusActive   LinkStatus = "active"
	LinkStatusInactive LinkStatus = "inactive"
)

func (s LinkStatus) IsValid() bool {
	return s == LinkStatusActive || s == LinkStatusInactive
}

type RedirectType string

const (
	RedirectTemporary RedirectType = "302"
	RedirectPermanent RedirectType = "301"
)

func (t RedirectType) IsValid() bool {
	return t == RedirectTemporary || t == RedirectPermanent
}

func (t RedirectType) HTTPStatus() int {
	if t == RedirectPermanent {
		return 301
	}
	return 302
}

type ShortLink struct {
	ID               string       `json:"id"`
	WorkspaceID      string       `json:"workspaceId"`
	DepartmentID     string       `json:"departmentId,omitempty"`
	CreatedBy        string       `json:"createdBy,omitempty"`
	Code             string       `json:"code"`
	ShortURL         string       `json:"shortUrl,omitempty"`
	TargetURL        string       `json:"targetUrl"`
	Title            string       `json:"title,omitempty"`
	RedirectType     RedirectType `json:"redirectType"`
	Status           LinkStatus   `json:"status"`
	PasswordHash     string       `json:"-"`
	HasPassword      bool         `json:"hasPassword"`
	ExpiresAt        *time.Time   `json:"expiresAt,omitempty"`
	MaxClicks        *int64       `json:"maxClicks,omitempty"`
	ClickCount       int64        `json:"clickCount"`
	UniqueClickCount int64        `json:"uniqueClickCount"`
	LastClickedAt    *time.Time   `json:"lastClickedAt,omitempty"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

func (l *ShortLink) Normalize() {
	l.ID = strings.TrimSpace(l.ID)
	l.WorkspaceID = strings.TrimSpace(l.WorkspaceID)
	l.DepartmentID = strings.TrimSpace(l.DepartmentID)
	l.Code = strings.TrimSpace(l.Code)
	l.TargetURL = strings.TrimSpace(l.TargetURL)
	l.Title = strings.TrimSpace(l.Title)

	if l.Status == "" {
		l.Status = LinkStatusActive
	}
	if l.RedirectType == "" {
		l.RedirectType = RedirectTemporary
	}
	l.HasPassword = l.PasswordHash != ""
}

func (l *ShortLink) Validate() error {
	if l.ID == "" {
		return ErrShortLinkIDRequired
	}
	if l.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if l.Code == "" {
		return ErrCodeRequired
	}
	if !l.RedirectType.IsValid() {
		return ErrInvalidRedirectType
	}
	if !l.Status.IsValid() {
		return ErrInvalidStatus
	}
	if len(l.Title) > MaxTitleLength {
		return ErrTitleTooLong
	}
	if l.MaxClicks != nil && *l.MaxClicks <= 0 {
		return ErrInvalidMaxClicks
	}
	return ValidateTargetURL(l.TargetURL)
}

func (l *ShortLink) HasPasswordProtection() bool {
	return l.PasswordHash != ""
}

func (l *ShortLink) IsExpired(now time.Time) bool {
	return l.ExpiresAt != nil && !now.Before(*l.ExpiresAt)
}

func (l *ShortLink) ReachedClickLimit() bool {
	return l.MaxClicks != nil && l.ClickCount >= *l.MaxClicks
}

func (l *ShortLink) IsResolvable(now time.Time) bool {
	return l.Status == LinkStatusActive && !l.IsExpired(now) && !l.ReachedClickLimit()
}

type Click struct {
	ID            string    `json:"id"`
	ShortLinkID   string    `json:"shortLinkId"`
	WorkspaceID   string    `json:"workspaceId"`
	OccurredAt    time.Time `json:"occurredAt"`
	IPHash        string    `json:"-"`
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
