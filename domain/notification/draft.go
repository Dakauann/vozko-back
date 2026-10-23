package notification

import (
	"strings"
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
	SourcePaymentReversal = "payment_reversal"
	SourcePlanExpiry      = "plan_expiry"
	SourceMonthlyInvoice  = "monthly_invoice"
	SourceBillingCutoff   = "billing_cutoff"
	SourceCommentAnalysis = "comment_analysis"
	SourcePhoneAlert      = "whatsapp_phone"
	SourceMercadoLivre    = "mercadolivre_account"
	SourceTikTok          = "tiktok_account"
	SourceWhatsAppForm    = "whatsapp_form"
	SourceWorkflowAlert   = "workflow_alert"
	SourceCarrierSender   = "carrier_sender"
	SourceCarrierFlood    = "carrier_flood"
	SourceGoogleAccount   = "google_account"
	SourceGoogleSheet     = "google_sheet"
	SourceEmailWebhook    = "email_webhook"

	SourceConversationNoteMention = "conversation_note_mention"
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
	EpisodeKey  string
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
	d.EpisodeKey = strings.TrimSpace(d.EpisodeKey)
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
	if utf8.RuneCountInString(d.DedupKey) > DedupKeyMaxRunes || utf8.RuneCountInString(d.EpisodeKey) > DedupKeyMaxRunes {
		return ErrDedupKeyTooLong
	}
	if d.EpisodeKey != "" && d.DedupKey != "" {
		return ErrEpisodeDedupConflict
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
	return clipRunes(title, TitleMaxRunes)
}

func clipRunes(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}
