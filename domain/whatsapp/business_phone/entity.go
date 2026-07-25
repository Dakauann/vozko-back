package businessphone

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrPhoneNumberNotFound          = errors.New("whatsapp business phone number not found")
	ErrPhoneNumberRequired          = errors.New("phone number is required")
	ErrPhoneNumberAlreadyExists     = errors.New("whatsapp business phone number already exists")
	ErrOwnerWorkspaceIDRequired     = errors.New("owner workspace id is required")
	ErrOwnerAssignedByRequired      = errors.New("owner assigned by is required")
	ErrVerificationCodeRequired     = errors.New("verification code is required")
	ErrVerificationCodeInvalid      = errors.New("verification code is invalid")
	ErrVerificationMethodRequired   = errors.New("verification method (SMS or VOICE) is required")
	ErrMetaPhoneNumberIDRequired    = errors.New("meta phone number ID is required")
	ErrProfilePictureUploadFailed   = errors.New("failed to upload profile picture")
	ErrCannotUpdateWhileVerifying   = errors.New("cannot update phone number while verification is in progress")
	ErrPhoneNumberNotVerified       = errors.New("phone number is not verified")
	ErrInvalidVerificationMethod    = errors.New("invalid verification method, must be SMS or VOICE")
	ErrDisplayNameRequired          = errors.New("display name is required")
	ErrBusinessProfileUpdateFailed  = errors.New("failed to update business profile")
	ErrMetaAPIError                 = errors.New("meta API returned an error")
	ErrPhoneNumberAlreadyRegistered = errors.New("phone number is already registered with WhatsApp")
	ErrPhoneNumberIneligible        = errors.New("phone number is not eligible for WhatsApp Business")
	ErrInvalidWABAId                = errors.New("invalid WABA ID")
	ErrUnsupportedForProvider       = errors.New("operation is not supported for this number's provider")
	ErrPhoneLimitReached            = errors.New("whatsapp business phone limit reached for this workspace")
	ErrInvalidAccessToken           = errors.New("invalid access token")
	ErrInvalidPhoneNumber           = errors.New("invalid phone number")
	ErrInvalidVerificationCode      = errors.New("invalid verification code")
	ErrVerificationFailed           = errors.New("verification failed")
	ErrVerificationCodeExpired      = errors.New("verification code expired")
	ErrTooManyVerificationAttempts  = errors.New("too many verification attempts")
	ErrPhoneNumberNotConnected      = errors.New("phone number not connected")
	ErrPhoneNumberStillConnected    = errors.New("phone number is still connected to the WhatsApp Cloud API; disconnect or remove it before deleting")
	ErrPhoneConfirmationMismatch    = errors.New("confirmation does not match the phone number")
	ErrProfileUpdateFailed          = errors.New("failed to update business profile")
	ErrInvalidBusinessVertical      = errors.New("invalid business vertical - must be one of: ALCOHOL, APPAREL, AUTO, BEAUTY, EDU, ENTERTAIN, EVENT_PLAN, FINANCE, GOVT, GROCERY, HEALTH, HOTEL, MATRIMONY_SERVICE, NONPROFIT, ONLINE_GAMBLING, OTC_DRUGS, OTHER, PHYSICAL_GAMBLING, PROF_SERVICES, RESTAURANT, RETAIL, TRAVEL, or empty string to clear")
)

// Provider identifies which WhatsApp Business Solution Provider a phone (and
// its WABA) is hosted through. Existing numbers were onboarded directly against
// Meta; new numbers are onboarded through 360dialog as our BSP partner. Empty is
// treated as ProviderMeta so historical rows keep their behavior.
type Provider string

const (
	ProviderMeta      Provider = "meta"
	ProviderDialog360 Provider = "dialog360"
)

// Normalized returns the provider, defaulting an empty value to ProviderMeta so
// no code path has to special case the zero value.
func (p Provider) Normalized() Provider {
	if strings.TrimSpace(string(p)) == "" {
		return ProviderMeta
	}
	return p
}

func (p Provider) IsDialog360() bool { return p.Normalized() == ProviderDialog360 }

type Status string

const (
	StatusPending      Status = "PENDING"
	StatusVerifying    Status = "VERIFYING"
	StatusConnected    Status = "CONNECTED"
	StatusDisconnected Status = "DISCONNECTED"
	StatusBanned       Status = "BANNED"
	StatusFlagged      Status = "FLAGGED"
	StatusRestricted   Status = "RESTRICTED"
	StatusRateLimited  Status = "RATE_LIMITED"
	// StatusOnboardingFailed marks a number whose 360dialog provisioning handover
	// did not complete (for example account_sharing/numbers failed). The phone row
	// is kept so the failure is visible in the UI and can be retried; see
	// OnboardingError for the reason.
	StatusOnboardingFailed Status = "ONBOARDING_FAILED"
	StatusSuspended        Status = "SUSPENDED"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusVerifying, StatusConnected, StatusDisconnected, StatusBanned, StatusFlagged, StatusRestricted, StatusRateLimited, StatusOnboardingFailed, StatusSuspended:
		return true
	default:
		return false
	}
}

type QualityRating string

const (
	QualityRatingGreen   QualityRating = "GREEN"
	QualityRatingYellow  QualityRating = "YELLOW"
	QualityRatingRed     QualityRating = "RED"
	QualityRatingUnknown QualityRating = "UNKNOWN"
	QualityRatingNA      QualityRating = "NA"
)

type NameStatus string

const (
	NameStatusApproved               NameStatus = "APPROVED"
	NameStatusAvailableWithoutReview NameStatus = "AVAILABLE_WITHOUT_REVIEW"
	NameStatusDeclined               NameStatus = "DECLINED"
	NameStatusExpired                NameStatus = "EXPIRED"
	NameStatusPendingReview          NameStatus = "PENDING_REVIEW"
	NameStatusNone                   NameStatus = "NONE"
)

type VerificationMethod string

const (
	VerificationMethodSMS   VerificationMethod = "SMS"
	VerificationMethodVoice VerificationMethod = "VOICE"
)

func (m VerificationMethod) IsValid() bool {
	switch m {
	case VerificationMethodSMS, VerificationMethodVoice:
		return true
	default:
		return false
	}
}

type BusinessVertical string

const (
	VerticalAlcohol          BusinessVertical = "ALCOHOL"
	VerticalApparel          BusinessVertical = "APPAREL"
	VerticalAuto             BusinessVertical = "AUTO"
	VerticalBeauty           BusinessVertical = "BEAUTY"
	VerticalEducation        BusinessVertical = "EDU"
	VerticalEntertainment    BusinessVertical = "ENTERTAIN"
	VerticalEventPlanning    BusinessVertical = "EVENT_PLAN"
	VerticalFinance          BusinessVertical = "FINANCE"
	VerticalGovernment       BusinessVertical = "GOVT"
	VerticalGrocery          BusinessVertical = "GROCERY"
	VerticalHealth           BusinessVertical = "HEALTH"
	VerticalHotel            BusinessVertical = "HOTEL"
	VerticalMatrimony        BusinessVertical = "MATRIMONY_SERVICE"
	VerticalNonprofit        BusinessVertical = "NONPROFIT"
	VerticalOnlineGambling   BusinessVertical = "ONLINE_GAMBLING"
	VerticalOTCDrugs         BusinessVertical = "OTC_DRUGS"
	VerticalOther            BusinessVertical = "OTHER"
	VerticalPhysicalGambling BusinessVertical = "PHYSICAL_GAMBLING"
	VerticalProfServices     BusinessVertical = "PROF_SERVICES"
	VerticalRestaurant       BusinessVertical = "RESTAURANT"
	VerticalRetail           BusinessVertical = "RETAIL"
	VerticalTravel           BusinessVertical = "TRAVEL"
)

func (v BusinessVertical) IsValid() bool {
	if v == "" {
		return true
	}
	switch v {
	case VerticalAlcohol, VerticalApparel, VerticalAuto, VerticalBeauty,
		VerticalEducation, VerticalEntertainment, VerticalEventPlanning,
		VerticalFinance, VerticalGovernment, VerticalGrocery, VerticalHealth,
		VerticalHotel, VerticalMatrimony, VerticalNonprofit, VerticalOnlineGambling,
		VerticalOTCDrugs, VerticalOther, VerticalPhysicalGambling,
		VerticalProfServices, VerticalRestaurant, VerticalRetail, VerticalTravel:
		return true
	default:
		return false
	}
}

func ValidVerticals() []BusinessVertical {
	return []BusinessVertical{
		VerticalAlcohol, VerticalApparel, VerticalAuto, VerticalBeauty,
		VerticalEducation, VerticalEntertainment, VerticalEventPlanning,
		VerticalFinance, VerticalGovernment, VerticalGrocery, VerticalHealth,
		VerticalHotel, VerticalMatrimony, VerticalNonprofit, VerticalOnlineGambling,
		VerticalOTCDrugs, VerticalOther, VerticalPhysicalGambling,
		VerticalProfServices, VerticalRestaurant, VerticalRetail, VerticalTravel,
	}
}

type WhatsAppBusinessPhoneNumber struct {
	ID                     string          `json:"id"`
	Provider               Provider        `json:"provider,omitempty"`
	MetaPhoneNumberID      string          `json:"metaPhoneNumberId"`
	WABAId                 string          `json:"wabaId"`
	OwnerWorkspaceID       string          `json:"ownerWorkspaceId,omitempty"`
	OwnerAssignedBy        string          `json:"ownerAssignedBy,omitempty"`
	OwnerAssignedAt        *time.Time      `json:"ownerAssignedAt,omitempty"`
	BusinessPortfolioID    string          `json:"businessPortfolioId,omitempty"`
	DisplayPhoneNumber     string          `json:"displayPhoneNumber"`
	VerifiedName           string          `json:"verifiedName"`
	Status                 Status          `json:"status"`
	QualityRating          QualityRating   `json:"qualityRating"`
	NameStatus             NameStatus      `json:"nameStatus"`
	CodeVerificationStatus string          `json:"codeVerificationStatus"`
	IsOfficialBusiness     bool            `json:"isOfficialBusiness"`
	BusinessProfile        BusinessProfile `json:"businessProfile,omitempty"`
	AccessToken            string          `json:"-"`
	Dialog360ChannelID     string          `json:"dialog360ChannelId,omitempty"`
	Dialog360APIKey        string          `json:"-"`
	// OnboardingError carries the reason a 360dialog provisioning handover failed,
	// surfaced to the UI alongside StatusOnboardingFailed. Empty when healthy.
	OnboardingError            string    `json:"onboardingError,omitempty"`
	WABAName                   string    `json:"wabaName,omitempty"`
	AccountReviewStatus        string    `json:"accountReviewStatus,omitempty"`
	BusinessVerificationStatus string    `json:"businessVerificationStatus,omitempty"`
	OwnershipType              string    `json:"ownershipType,omitempty"`
	MessagingLimitTier         string    `json:"messagingLimitTier,omitempty"`
	CallsEnabled               bool      `json:"callsEnabled"`
	CreatedAt                  time.Time `json:"createdAt"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

type BusinessProfile struct {
	About                string           `json:"about,omitempty"`
	Address              string           `json:"address,omitempty"`
	Description          string           `json:"description,omitempty"`
	Email                string           `json:"email,omitempty"`
	ProfilePictureURL    string           `json:"profilePictureUrl,omitempty"`
	ProfilePictureHandle string           `json:"profilePictureHandle,omitempty"`
	Websites             []string         `json:"websites,omitempty"`
	Vertical             BusinessVertical `json:"vertical,omitempty"`
}

func NewWhatsAppBusinessPhoneNumber(displayPhoneNumber, metaPhoneNumberID, wabaID string) (*WhatsAppBusinessPhoneNumber, error) {
	displayPhoneNumber = strings.TrimSpace(displayPhoneNumber)
	if displayPhoneNumber == "" {
		return nil, ErrPhoneNumberRequired
	}

	metaPhoneNumberID = strings.TrimSpace(metaPhoneNumberID)
	if metaPhoneNumberID == "" {
		return nil, ErrMetaPhoneNumberIDRequired
	}

	return &WhatsAppBusinessPhoneNumber{
		Provider:           ProviderMeta,
		MetaPhoneNumberID:  metaPhoneNumberID,
		WABAId:             strings.TrimSpace(wabaID),
		DisplayPhoneNumber: displayPhoneNumber,
		Status:             StatusPending,
		QualityRating:      QualityRatingUnknown,
		NameStatus:         NameStatusNone,
	}, nil
}

func (p *WhatsAppBusinessPhoneNumber) CanBeUpdated() bool {
	return p.Status != StatusVerifying
}

func (p *WhatsAppBusinessPhoneNumber) IsVerified() bool {
	return p.Status == StatusConnected
}

func (p *WhatsAppBusinessPhoneNumber) UpdateFromMeta(verifiedName string, status Status, qualityRating QualityRating, nameStatus NameStatus) {
	p.VerifiedName = verifiedName
	p.Status = status
	p.QualityRating = qualityRating
	p.NameStatus = nameStatus
}

func (p *WhatsAppBusinessPhoneNumber) SetBusinessProfile(profile BusinessProfile) {
	p.BusinessProfile = profile
}

func (p *WhatsAppBusinessPhoneNumber) MarkAsVerifying() {
	p.Status = StatusVerifying
}

func (p *WhatsAppBusinessPhoneNumber) MarkAsConnected() {
	p.Status = StatusConnected
}

func (p *WhatsAppBusinessPhoneNumber) AssignOwner(workspaceID, assignedBy string, assignedAt time.Time) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ErrOwnerWorkspaceIDRequired
	}
	assignedBy = strings.TrimSpace(assignedBy)
	if assignedBy == "" {
		return ErrOwnerAssignedByRequired
	}
	at := assignedAt.UTC()
	p.OwnerWorkspaceID = workspaceID
	p.OwnerAssignedBy = assignedBy
	p.OwnerAssignedAt = &at
	return nil
}

func (p *WhatsAppBusinessPhoneNumber) BelongsToWorkspace(workspaceID string) bool {
	return strings.TrimSpace(p.OwnerWorkspaceID) != "" && strings.TrimSpace(p.OwnerWorkspaceID) == strings.TrimSpace(workspaceID)
}
