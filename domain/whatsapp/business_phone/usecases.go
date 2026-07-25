package businessphone

import "vozko/domain/shared"

type ListInput struct {
	WABAId           string
	Status           Status
	QualityRating    QualityRating
	Search           string
	OwnerWorkspaceID string
	AccessPhoneIDs   []string
	Options          shared.QueryOptions
}

type ListUseCase interface {
	Execute(input ListInput) (*shared.PaginatedResult[*WhatsAppBusinessPhoneNumber], error)
}

type GetUseCase interface {
	Execute(id string) (*WhatsAppBusinessPhoneNumber, error)
}

type SyncPhoneNumberInput struct {
	PhoneID     string
	AccessToken string
}

type SyncPhoneNumberUseCase interface {
	Execute(input SyncPhoneNumberInput) (*WhatsAppBusinessPhoneNumber, error)
}

type RegisterPhoneInput struct {
	PhoneID     string
	Pin         string
	AccessToken string
}

type RegisterPhoneUseCase interface {
	Execute(input RegisterPhoneInput) (*WhatsAppBusinessPhoneNumber, error)
}

type DeregisterPhoneInput struct {
	PhoneID     string
	AccessToken string
}

type DeregisterPhoneUseCase interface {
	Execute(input DeregisterPhoneInput) (*WhatsAppBusinessPhoneNumber, error)
}

type RequestVerificationCodeInput struct {
	PhoneID     string
	Method      VerificationMethod
	Language    string
	AccessToken string
}

type RequestVerificationCodeUseCase interface {
	Execute(input RequestVerificationCodeInput) error
}

type VerifyCodeInput struct {
	PhoneID     string
	Code        string
	AccessToken string
}

type VerifyCodeUseCase interface {
	Execute(input VerifyCodeInput) (*WhatsAppBusinessPhoneNumber, error)
}

type UpdateBusinessProfileInput struct {
	PhoneID     string
	Profile     BusinessProfile
	AccessToken string
}

type UpdateBusinessProfileUseCase interface {
	Execute(input UpdateBusinessProfileInput) (*WhatsAppBusinessPhoneNumber, error)
}

type GetBusinessProfileInput struct {
	PhoneID     string
	AccessToken string
}

type GetBusinessProfileUseCase interface {
	Execute(input GetBusinessProfileInput) (*BusinessProfile, error)
}

type DeletePhoneNumberUseCase interface {
	Execute(id string) error
}

// UnassignOwnerUseCase detaches a phone from its owning workspace, returning it
// to the unassigned pool. It is fully reversible (the number can be re-assigned)
// and does not touch the Meta registration or connection state.
type UnassignOwnerUseCase interface {
	Execute(phoneID string) error
}

type ReleasePhoneInput struct {
	PhoneID     string
	AccessToken string
	// ConfirmPhoneNumber, when non-empty, must match the phone's display number.
	// This is the server-side guard for the typed-confirmation UX on the
	// irreversible removal — the backend is the source of truth, not the client.
	ConfirmPhoneNumber string
}

type ReleasePhoneResult struct {
	Deregistered      bool   `json:"deregistered"`
	WebhooksRemoved   bool   `json:"webhooksRemoved"`
	TokenCleared      bool   `json:"tokenCleared"`
	PhoneDeleted      bool   `json:"phoneDeleted"`
	WABACleanedUp     bool   `json:"wabaCleanedUp"`
	Dialog360Canceled bool   `json:"dialog360Canceled,omitempty"`
	DeregisterError   string `json:"deregisterError,omitempty"`
	WebhooksError     string `json:"webhooksError,omitempty"`
}

type ReleasePhoneUseCase interface {
	Execute(input ReleasePhoneInput) (*ReleasePhoneResult, error)
}

// OnboardEmbeddedSignupInput carries the result of a completed Meta Embedded
// Signup. On the native Meta path the number is registered directly on the Cloud
// API and the Graph access token is persisted on the phone; the Dialog360* fields
// are populated only when the same use case is driven by the 360dialog path.
type OnboardEmbeddedSignupInput struct {
	// Provider selects the BSP this number is onboarded through. Empty defaults
	// to ProviderMeta so the existing Meta Embedded Signup callers are unchanged.
	Provider                   Provider
	PhoneNumberID              string
	WABAId                     string
	OwnerWorkspaceID           string
	OwnerAssignedBy            string
	BusinessID                 string
	AccessToken                string
	WABAName                   string
	AccountReviewStatus        string
	BusinessVerificationStatus string
	OwnershipType              string
	MessagingLimitTier         string
	IsCoexistence              bool
	// Dialog360* are populated only on the 360dialog onboarding path.
	Dialog360ChannelID string
	Dialog360APIKey    string
	Dialog360ClientID  string
}

type OnboardEmbeddedSignupResult struct {
	Phone *WhatsAppBusinessPhoneNumber
	IsNew bool
}

type OnboardEmbeddedSignupUseCase interface {
	Execute(input OnboardEmbeddedSignupInput) (*OnboardEmbeddedSignupResult, error)
}
