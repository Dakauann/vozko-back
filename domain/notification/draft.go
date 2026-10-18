package notification

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	TitleMaxRunes       = 120
	BodyMaxRunes        = 2000
	ActionLabelMaxRunes = 40
	DedupKeyMaxRunes    = 200
)

const (
	SourceBroadcast       = "broadcast"
	SourceSmartCampaign   = "smart_campaign"
	SourceAdsFunds        = "ads_funds"
	SourceLowBalance      = "low_balance"
	SourceTopUp           = "wallet_top_up"
	SourcePlanExpiry      = "plan_expiry"
	SourceCommentAnalysis = "comment_analysis"
	SourcePhoneAlert      = "whatsapp_phone"
)

type EmailCopy struct {
	Template     string                 `json:"template"`
	Subject      string                 `json:"subject"`
	Placeholders map[string]interface{} `json:"placeholders,omitempty"`
}

type Draft struct {
	WorkspaceID string
	Category    Category
	Severity    Severity
	Title       string
	Body        string
	ImageURL    string
	ActionURL   string
	ActionLabel string
	Audience    Audience
	Email       bool
	EmailCopy   *EmailCopy
	DedupKey    string
	DedupWindow time.Duration
	Source      string
	BroadcastID string
	CreatedBy   string
}

func (d Draft) Normalize() Draft {
	d.WorkspaceID = strings.TrimSpace(d.WorkspaceID)
	d.Title = strings.TrimSpace(d.Title)
	d.Body = strings.TrimSpace(d.Body)
	d.ImageURL = strings.TrimSpace(d.ImageURL)
	d.ActionURL = strings.TrimSpace(d.ActionURL)
	d.ActionLabel = strings.TrimSpace(d.ActionLabel)
	d.DedupKey = strings.TrimSpace(d.DedupKey)
	if d.Severity == "" {
		d.Severity = SeverityInfo
	}
	d.Audience = d.Audience.Normalize()
	return d
}

func (d Draft) Validate() error {
	if d.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if err := validateContent(d.Category, d.Severity, d.Title, d.Body, d.ImageURL, d.ActionURL, d.ActionLabel); err != nil {
		return err
	}
	if utf8.RuneCountInString(d.DedupKey) > DedupKeyMaxRunes {
		return ErrDedupKeyTooLong
	}
	return d.Audience.Validate()
}

func validateContent(category Category, severity Severity, title, body, imageURL, actionURL, actionLabel string) error {
	if !category.Valid() {
		return ErrCategoryUnknown
	}
	if !severity.Valid() {
		return ErrSeverityUnknown
	}
	if title == "" {
		return ErrTitleRequired
	}
	if utf8.RuneCountInString(title) > TitleMaxRunes {
		return ErrTitleTooLong
	}
	if utf8.RuneCountInString(body) > BodyMaxRunes {
		return ErrBodyTooLong
	}
	if imageURL != "" && !strings.HasPrefix(imageURL, "https://") {
		return ErrImageForeign
	}
	if actionLabel != "" && actionURL == "" {
		return ErrActionURLInvalid
	}
	if actionURL != "" && !validActionURL(actionURL) {
		return ErrActionURLInvalid
	}
	if utf8.RuneCountInString(actionLabel) > ActionLabelMaxRunes {
		return ErrActionLabelTooLong
	}
	return nil
}

func validActionURL(link string) bool {
	if strings.ContainsAny(link, " \t\r\n\\") {
		return false
	}
	if strings.HasPrefix(link, "/") {
		return !strings.HasPrefix(link, "//")
	}
	rest, ok := strings.CutPrefix(link, "https://")
	return ok && rest != "" && !strings.HasPrefix(rest, "/")
}

func AbsoluteLink(base, link string) string {
	if strings.HasPrefix(link, "/") {
		return strings.TrimRight(base, "/") + link
	}
	return link
}

func ClipTitle(title string) string {
	title = strings.TrimSpace(title)
	runes := []rune(title)
	if len(runes) <= TitleMaxRunes {
		return title
	}
	return strings.TrimSpace(string(runes[:TitleMaxRunes-1])) + "…"
}

func (d Draft) StoredDedupKey(at time.Time) string {
	if d.DedupKey == "" {
		return ""
	}
	if d.DedupWindow <= 0 {
		return d.DedupKey
	}
	return fmt.Sprintf("%s@%d", d.DedupKey, at.UTC().Truncate(d.DedupWindow).Unix())
}
