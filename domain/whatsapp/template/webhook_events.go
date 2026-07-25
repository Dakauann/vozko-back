package template

const (
	FieldMessageTemplateStatusUpdate     = "message_template_status_update"
	FieldMessageTemplateQualityUpdate    = "message_template_quality_update"
	FieldMessageTemplateComponentsUpdate = "message_template_components_update"
	FieldTemplateCategoryUpdate          = "template_category_update"
)

type TemplateWebhookPayload struct {
	Object string                 `json:"object"`
	Entry  []TemplateWebhookEntry `json:"entry"`
}

type TemplateWebhookEntry struct {
	ID      string                  `json:"id"`
	Time    int64                   `json:"time"`
	Changes []TemplateWebhookChange `json:"changes"`
}

type TemplateWebhookChange struct {
	Field string               `json:"field"`
	Value TemplateWebhookValue `json:"value"`
}

type TemplateWebhookValue struct {
	Event             string `json:"event"`
	MessageTemplateID int64  `json:"message_template_id"`
	// ChannelExternalID is a non-numeric template id used when the status change
	// arrives via the 360dialog partner webhook. 360dialog identifies our template
	// by its channel-scoped id (the value we store in Template.ExternalID), not
	// Meta's numeric message_template_id. Set only on the 360dialog path; empty on
	// the Meta path. Not a Meta wire field.
	ChannelExternalID       string         `json:"-"`
	MessageTemplateName     string         `json:"message_template_name"`
	MessageTemplateLanguage string         `json:"message_template_language"`
	Reason                  string         `json:"reason"`
	MessageTemplateCategory string         `json:"message_template_category"`
	DisableInfo             *DisableInfo   `json:"disable_info,omitempty"`
	OtherInfo               *OtherInfo     `json:"other_info,omitempty"`
	RejectionInfo           *RejectionInfo `json:"rejection_info,omitempty"`

	PreviousQualityScore string `json:"previous_quality_score,omitempty"`
	NewQualityScore      string `json:"new_quality_score,omitempty"`

	PreviousCategory string `json:"previous_category,omitempty"`
	NewCategory      string `json:"new_category,omitempty"`
}

type DisableInfo struct {
	DisableDate string `json:"disable_date"`
}

type OtherInfo struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type RejectionInfo struct {
	Reason         string `json:"reason"`
	Recommendation string `json:"recommendation"`
}

type HandleTemplateWebhookUseCase interface {
	Execute(payload *TemplateWebhookPayload) error
}

type ConsumeTemplateWebhookUseCase interface {
	Start() error
}
