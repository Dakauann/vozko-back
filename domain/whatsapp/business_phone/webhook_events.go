package businessphone

type PhoneWebhookPayload struct {
	Object string              `json:"object"`
	Entry  []PhoneWebhookEntry `json:"entry"`
}

type PhoneWebhookEntry struct {
	ID      string               `json:"id"`
	Changes []PhoneWebhookChange `json:"changes"`
}

type PhoneWebhookChange struct {
	Value PhoneWebhookValue `json:"value"`
	Field string            `json:"field"`
}

type PhoneWebhookValue struct {
	DisplayPhoneNumber string `json:"display_phone_number,omitempty"`
	CurrentLimit       string `json:"current_limit,omitempty"`
	Event              string `json:"event,omitempty"`

	Decision              string `json:"decision,omitempty"`
	RequestedVerifiedName string `json:"requested_verified_name,omitempty"`
	RejectionReason       string `json:"rejection_reason,omitempty"`

	PhoneNumber string `json:"phone_number,omitempty"`

	MaxDailyConversationPerPhone int `json:"max_daily_conversation_per_phone,omitempty"`
	MaxPhoneNumbersPerBusiness   int `json:"max_phone_numbers_per_business,omitempty"`

	BanInfo  *BanInfo  `json:"ban_info,omitempty"`
	WABAInfo *WABAInfo `json:"waba_info,omitempty"`
}

type BanInfo struct {
	WABABanState string `json:"waba_ban_state,omitempty"`
	WABABanDate  string `json:"waba_ban_date,omitempty"`
}

type WABAInfo struct {
	WABAId          string `json:"waba_id,omitempty"`
	OwnerBusinessID string `json:"owner_business_id,omitempty"`
}

const (
	FieldPhoneNumberQualityUpdate = "phone_number_quality_update"
	FieldPhoneNumberNameUpdate    = "phone_number_name_update"
	FieldAccountAlerts            = "account_alerts"
	FieldBusinessCapabilityUpdate = "business_capability_update"
	FieldAccountUpdate            = "account_update"
	FieldAccountReviewUpdate      = "account_review_update"
	FieldBusinessStatusUpdate     = "business_status_update"
)

func SubscriptionWebhookFields() []string {
	return []string{
		"messages",
		"calls",
		"message_template_status_update",
		"message_template_quality_update",
		"message_template_components_update",
		"template_category_update",
		"phone_number_name_update",
		"phone_number_quality_update",
		"account_alerts",
		"account_update",
		"account_review_update",
		"business_capability_update",
		"business_status_update",
		"security",
		"flows",
		"message_echoes",
		"history",
		"smb_app_state_sync",
		"smb_message_echoes",
	}
}

type HandlePhoneWebhookUseCase interface {
	Execute(payload *PhoneWebhookPayload) error
}

type ConsumePhoneWebhookUseCase interface {
	Start() error
}
