package whatsappoutreach

type StartConversationRequest struct {
	BusinessPhoneID  string   `json:"businessPhoneId"`
	TemplateID       string   `json:"templateId"`
	PhoneNumber      string   `json:"phoneNumber"`
	Name             string   `json:"name,omitempty"`
	Parameters       []string `json:"parameters,omitempty"`
	HeaderParameters []string `json:"headerParameters,omitempty"`
}

type StartedConversationResponse struct {
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`

	LeadID    string `json:"leadId,omitempty"`
	AttemptID string `json:"attemptId,omitempty"`
	MessageID string `json:"messageId,omitempty"`

	ConversationExisted bool  `json:"conversationExisted"`
	Replayed            bool  `json:"replayed"`
	ChargedMicros       int64 `json:"chargedMicros"`
	Recorded            bool  `json:"recorded"`
}

type SendQuoteResponse struct {
	Category      string `json:"category"`
	PriceMicros   int64  `json:"priceMicros"`
	BalanceMicros int64  `json:"balanceMicros"`
	Affordable    bool   `json:"affordable"`
}

type WindowOpenResponse struct {
	Error     bool   `json:"error"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	EntryID   string `json:"entryId,omitempty"`
	EntryType string `json:"entryType,omitempty"`
}
