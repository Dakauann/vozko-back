package businessphone

type MetaPhoneNumberInfo struct {
	ID                     string `json:"id"`
	DisplayPhoneNumber     string `json:"display_phone_number"`
	VerifiedName           string `json:"verified_name"`
	Status                 string `json:"status"`
	QualityRating          string `json:"quality_rating"`
	NameStatus             string `json:"name_status"`
	CodeVerificationStatus string `json:"code_verification_status"`
	IsOfficialBusiness     bool   `json:"is_official_business"`
}

type MetaBusinessProfile struct {
	About                string   `json:"about"`
	Address              string   `json:"address"`
	Description          string   `json:"description"`
	Email                string   `json:"email"`
	ProfilePictureURL    string   `json:"profile_picture_url"`
	ProfilePictureHandle string   `json:"profile_picture_handle,omitempty"`
	Websites             []string `json:"websites"`
	Vertical             string   `json:"vertical"`
}

type MetaWABAInfo struct {
	ID                         string `json:"id"`
	Name                       string `json:"name"`
	Currency                   string `json:"currency"`
	TimezoneId                 string `json:"timezone_id"`
	MessageTemplateNamespace   string `json:"message_template_namespace"`
	AccountReviewStatus        string `json:"account_review_status"`
	BusinessVerificationStatus string `json:"business_verification_status"`
	OwnershipType              string `json:"ownership_type"`
	Status                     string `json:"status"`
	Country                    string `json:"country"`
}

type MetaAPIService interface {
	GetWABA(wabaID string, accessToken string) (*MetaWABAInfo, error)
	ListPhoneNumbers(wabaID string, accessToken string) ([]MetaPhoneNumberInfo, error)
	GetPhoneNumber(phoneNumberID string, accessToken string) (*MetaPhoneNumberInfo, error)
	RequestVerificationCode(phoneNumberID string, method string, language string, accessToken string) error
	VerifyCode(phoneNumberID string, code string, accessToken string) (bool, error)
	RegisterPhone(phoneNumberID string, pin string, accessToken string) error
	DeregisterPhone(phoneNumberID string, accessToken string) error
	UnsubscribeApp(wabaID string, accessToken string) error
	GetBusinessProfile(phoneNumberID string, accessToken string) (*MetaBusinessProfile, error)
	UpdateBusinessProfile(phoneNumberID string, profile MetaBusinessProfile, accessToken string) error
	UploadProfilePicture(data []byte, fileName string, accessToken string) (string, error)

	GetCallingStatus(phoneNumberID string, accessToken string) (bool, error)

	SetCallingStatus(phoneNumberID string, enabled bool, accessToken string) error

	SubscribeWebhooks(wabaID string, accessToken string) error

	// BlockUser / UnblockUser manage the phone number's blocklist via the Meta
	// Cloud API. Blocking stops Meta from forwarding that user's messages to the
	// webhook; userNumber is the contact's WhatsApp number (E.164 digits).
	BlockUser(phoneNumberID string, userNumber string, accessToken string) error
	UnblockUser(phoneNumberID string, userNumber string, accessToken string) error
}
