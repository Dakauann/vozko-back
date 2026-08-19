package whatsappoutreach

// StartConversationRequest is one operator reaching one number with one
// template.
//
// TemplateID and not a template name: a name is resolved through an unordered
// query that can pick a different row than the one shown in the picker, and the
// operator would be charged for a message they did not compose.
type StartConversationRequest struct {
	BusinessPhoneID string `json:"businessPhoneId"`
	TemplateID      string `json:"templateId"`
	PhoneNumber     string `json:"phoneNumber"`
	Name            string `json:"name,omitempty"`
	// Parameters fill the template body, in order.
	Parameters []string `json:"parameters,omitempty"`
	// HeaderParameters fill a TEXT header. Kept separate from the body's because
	// Meta addresses them as different components.
	HeaderParameters []string `json:"headerParameters,omitempty"`
}

// StartedConversationResponse tells the CRM where to go and what it cost.
type StartedConversationResponse struct {
	// EntryID and EntryType address the conversation the way the inbox does, so
	// the caller does not need to know this channel's storage by heart.
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`

	LeadID    string `json:"leadId,omitempty"`
	AttemptID string `json:"attemptId,omitempty"`
	MessageID string `json:"messageId,omitempty"`

	ConversationExisted bool `json:"conversationExisted"`
	// Replayed reports that this exact request had already been sent, so nothing
	// was sent or charged a second time.
	Replayed bool `json:"replayed"`
	// ChargedMicros is what this send cost. Echoed back so the UI can show the
	// operator the price of the action they just took.
	ChargedMicros int64 `json:"chargedMicros"`
	// Recorded is false when the message was delivered but could not be written
	// into the thread. The send still succeeded — reporting it as a failure would
	// invite a retry that charges twice.
	Recorded bool `json:"recorded"`
}

// SendQuoteResponse prices a send before it happens.
type SendQuoteResponse struct {
	Category      string `json:"category"`
	PriceMicros   int64  `json:"priceMicros"`
	BalanceMicros int64  `json:"balanceMicros"`
	Affordable    bool   `json:"affordable"`
}

// WindowOpenResponse is the 409 that is really a redirect.
//
// A number already inside its 24h window can be answered for FREE from the
// ordinary composer, so charging for a template would be spending money on
// something already available. The entry id rides along so the UI can open that
// conversation instead of merely refusing.
type WindowOpenResponse struct {
	Error     bool   `json:"error"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	EntryID   string `json:"entryId,omitempty"`
	EntryType string `json:"entryType,omitempty"`
}
