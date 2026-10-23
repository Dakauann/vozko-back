package rcs

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
)

const (
	MaxBrandNameRunes        = 40
	MaxBrandDescriptionRunes = 100
)

var (
	ErrSenderIncomplete      = errors.New("rcs: the sender is missing required brand data")
	ErrDescriptionNotISV     = errors.New("rcs: the description must name the brand the messages are sent for")
	ErrContactEmailDomain    = errors.New("rcs: the brand contact must use the brand's own email domain")
	ErrSectorRefused         = errors.New("rcs: carriers refuse senders of this sector")
	ErrGamblingAuthorization = errors.New("rcs: betting senders need proof of government authorization")
	ErrSenderTransition      = errors.New("rcs: the sender cannot make this change in its current state")
	ErrBillingCategoryFixed  = errors.New("rcs: the billing category cannot change after submission")
	ErrSenderNotFound        = errors.New("rcs: sender not found")
	ErrSenderCannotSend      = errors.New("rcs: the sender is not launched")
)

type BillingCategory string

const (
	BillingNonConversational BillingCategory = "non_conversational"
	BillingConversational    BillingCategory = "conversational"
)

func (b BillingCategory) Valid() bool {
	return b == BillingNonConversational || b == BillingConversational
}

type UseCase string

const (
	UseCaseTransactional UseCase = "transactional"
	UseCasePromotional   UseCase = "promotional"
	UseCaseOTP           UseCase = "otp"
	UseCaseMultiUse      UseCase = "multi_use"
)

var useCasePurposes = map[UseCase]string{
	UseCaseTransactional: "atualizações de pedidos e avisos",
	UseCasePromotional:   "promoções e novidades",
	UseCaseOTP:           "códigos de verificação",
	UseCaseMultiUse:      "atualizações de pedidos e promoções",
}

func (u UseCase) Valid() bool {
	_, ok := useCasePurposes[u]
	return ok
}

type Sector string

const (
	SectorRetail    Sector = "retail"
	SectorServices  Sector = "services"
	SectorFinance   Sector = "finance"
	SectorHealth    Sector = "health"
	SectorEducation Sector = "education"
	SectorTravel    Sector = "travel"
	SectorGambling  Sector = "gambling"
	SectorPolitical Sector = "political"
	SectorAdult     Sector = "adult"
	SectorOther     Sector = "other"
)

func (s Sector) Valid() bool {
	switch s {
	case SectorRetail, SectorServices, SectorFinance, SectorHealth, SectorEducation,
		SectorTravel, SectorGambling, SectorPolitical, SectorAdult, SectorOther:
		return true
	}
	return false
}

func (s Sector) Refused() bool {
	return s == SectorPolitical || s == SectorAdult
}

type SenderStatus string

const (
	SenderDraft                SenderStatus = "draft"
	SenderSubmitted            SenderStatus = "submitted"
	SenderAwaitingConfirmation SenderStatus = "awaiting_confirmation"
	SenderLaunched             SenderStatus = "launched"
	SenderPaused               SenderStatus = "paused"
	SenderRejected             SenderStatus = "rejected"
)

type ProviderState string

const (
	ProviderPending       ProviderState = "pending"
	ProviderAwaitingBrand ProviderState = "awaiting_brand"
	ProviderLaunched      ProviderState = "launched"
	ProviderSuspended     ProviderState = "suspended"
	ProviderRejected      ProviderState = "rejected"
)

type PausedBy string

const (
	PausedByPlatform PausedBy = "platform"
	PausedByCarrier  PausedBy = "carrier"
)

type PauseReason string

const (
	PauseManual        PauseReason = "manual"
	PauseComplaints    PauseReason = "complaints"
	PauseOptOutRate    PauseReason = "opt_out_rate"
	PauseUnpaidBalance PauseReason = "unpaid_balance"
	PauseCarrier       PauseReason = "carrier"
)

type Sender struct {
	ID                       string
	WorkspaceID              string
	BrandName                string
	Description              string
	LogoURL                  string
	BannerURL                string
	Color                    string
	UseCase                  UseCase
	Sector                   Sector
	GamblingAuthorizationURL string
	ContactName              string
	ContactEmail             string
	ContactPhone             string
	WebsiteURL               string
	PrivacyURL               string
	TermsURL                 string
	CNPJ                     string
	BillingCategory          BillingCategory
	Status                   SenderStatus
	StatusReason             string
	PausedBy                 PausedBy
	PauseReason              PauseReason
	Provider                 string
	ProviderProfileID        string
	ProviderRequestID        string
	PlatformEmailDomain      string
	Automation               conversation.ChannelAutomation
	DepartmentID             *string
	PipelineID               *string
	SubmittedAt              *time.Time
	LaunchedAt               *time.Time
	PausedAt                 *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (s *Sender) Normalize() {
	s.BrandName = strings.TrimSpace(s.BrandName)
	s.Description = strings.TrimSpace(s.Description)
	s.ContactName = strings.TrimSpace(s.ContactName)
	s.ContactEmail = strings.ToLower(strings.TrimSpace(s.ContactEmail))
	s.ContactPhone = strings.TrimSpace(s.ContactPhone)
	s.Color = strings.ToUpper(strings.TrimSpace(s.Color))
	s.CNPJ = digitsOnly(s.CNPJ)
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func ISVDescription(platform, brandName string, useCase UseCase) string {
	purpose, ok := useCasePurposes[useCase]
	if !ok {
		purpose = useCasePurposes[UseCaseMultiUse]
	}
	return fmt.Sprintf("%s envia %s em nome de %s.", strings.TrimSpace(platform), purpose, strings.TrimSpace(brandName))
}

func (s Sender) Validate(platformEmailDomain string) error {
	s.Normalize()
	missing := func(field string) error { return &IncompleteError{Field: field} }
	if s.BrandName == "" || utf8.RuneCountInString(s.BrandName) > MaxBrandNameRunes {
		return missing("brand_name")
	}
	if s.Description == "" || utf8.RuneCountInString(s.Description) > MaxBrandDescriptionRunes {
		return missing("description")
	}
	if !strings.Contains(strings.ToLower(s.Description), strings.ToLower(s.BrandName)) {
		return ErrDescriptionNotISV
	}
	for field, value := range map[string]string{"logo_url": s.LogoURL, "banner_url": s.BannerURL, "website_url": s.WebsiteURL, "privacy_url": s.PrivacyURL, "terms_url": s.TermsURL} {
		if !secureURL(value) {
			return missing(field)
		}
	}
	if !hexColor.MatchString(s.Color) {
		return missing("color")
	}
	if !s.UseCase.Valid() {
		return missing("use_case")
	}
	if !s.BillingCategory.Valid() {
		return missing("billing_category")
	}
	if !s.Sector.Valid() {
		return missing("sector")
	}
	if s.Sector.Refused() {
		return ErrSectorRefused
	}
	if s.Sector == SectorGambling && !secureURL(s.GamblingAuthorizationURL) {
		return ErrGamblingAuthorization
	}
	if s.ContactName == "" {
		return missing("contact_name")
	}
	if strings.TrimSpace(s.ContactPhone) == "" {
		return missing("contact_phone")
	}
	if len(s.CNPJ) != 14 {
		return missing("cnpj")
	}
	return validateContactEmail(s.ContactEmail, platformEmailDomain)
}

func validateContactEmail(raw, platformDomain string) error {
	address, err := emailaddress.Normalize(raw)
	if err != nil {
		return ErrContactEmailDomain
	}
	domain := address.Domain()
	platform := strings.ToLower(strings.TrimSpace(platformDomain))
	if emailaddress.IsFreeMailbox(domain) {
		return ErrContactEmailDomain
	}
	if platform != "" && (domain == platform || strings.HasSuffix(domain, "."+platform)) {
		return ErrContactEmailDomain
	}
	return nil
}

func (s Sender) Editable() bool {
	return s.Status == SenderDraft || s.Status == SenderRejected
}

func (s Sender) CanSend() bool {
	return s.Status == SenderLaunched
}

func (s Sender) Submit(now time.Time) (Sender, error) {
	if !s.Editable() {
		return s, ErrSenderTransition
	}
	s.Normalize()
	if err := s.Validate(s.PlatformEmailDomain); err != nil {
		return s, err
	}
	s.Status = SenderSubmitted
	s.StatusReason = ""
	s.SubmittedAt = &now
	return s, nil
}

func (s Sender) ChangeBillingCategory(category BillingCategory) (Sender, error) {
	if !s.Editable() {
		return s, ErrBillingCategoryFixed
	}
	if !category.Valid() {
		return s, &IncompleteError{Field: "billing_category"}
	}
	s.BillingCategory = category
	return s, nil
}

func (s Sender) ApplyProviderState(state ProviderState, reason string, now time.Time) Sender {
	if s.Status == SenderDraft {
		return s
	}
	switch state {
	case ProviderPending:
		if s.Status == SenderRejected {
			return s
		}
		if s.Status != SenderPaused {
			s.Status = SenderSubmitted
		}
	case ProviderAwaitingBrand:
		if s.Status == SenderSubmitted {
			s.Status = SenderAwaitingConfirmation
		}
	case ProviderLaunched:
		if s.Status == SenderPaused && s.PausedBy == PausedByPlatform {
			return s
		}
		s.Status = SenderLaunched
		s.PausedBy, s.PauseReason, s.PausedAt = "", "", nil
		if s.LaunchedAt == nil {
			s.LaunchedAt = &now
		}
	case ProviderSuspended:
		s.Status = SenderPaused
		s.PausedBy, s.PauseReason, s.PausedAt = PausedByCarrier, PauseCarrier, &now
	case ProviderRejected:
		s.Status = SenderRejected
	default:
		return s
	}
	s.StatusReason = strings.TrimSpace(reason)
	return s
}

func (s Sender) Pause(by PausedBy, reason PauseReason, now time.Time) (Sender, error) {
	if s.Status != SenderLaunched {
		return s, ErrSenderTransition
	}
	s.Status = SenderPaused
	s.PausedBy, s.PauseReason, s.PausedAt = by, reason, &now
	return s, nil
}

func (s Sender) Resume(now time.Time) (Sender, error) {
	if s.Status != SenderPaused || s.PausedBy != PausedByPlatform {
		return s, ErrSenderTransition
	}
	s.Status = SenderLaunched
	s.PausedBy, s.PauseReason, s.PausedAt = "", "", nil
	s.StatusReason = ""
	return s, nil
}

type IncompleteError struct {
	Field string
}

func (e *IncompleteError) Error() string {
	return ErrSenderIncomplete.Error() + ": " + e.Field
}

func (e *IncompleteError) Is(target error) bool { return target == ErrSenderIncomplete }

func (s Sender) ResumableByWorkspace() bool {
	return s.Status == SenderPaused && s.PausedBy == PausedByPlatform && s.PauseReason == PauseManual
}
