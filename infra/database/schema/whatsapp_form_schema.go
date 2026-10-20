package schema

import "time"

const (
	WhatsAppFormLiveNameIndex      = "ux_whatsapp_forms_live_name"
	WhatsAppFormVersionNumberIndex = "ux_whatsapp_form_versions_number"
)

type WhatsAppForm struct {
	ID                 string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string       `gorm:"type:uuid;not null;index:idx_whatsapp_forms_workspace_updated,priority:1;uniqueIndex:ux_whatsapp_forms_live_name,priority:1,where:archived_at IS NULL"`
	Name               string       `gorm:"type:varchar(160);not null;uniqueIndex:ux_whatsapp_forms_live_name,priority:2,expression:lower(name)"`
	Description        string       `gorm:"type:text;not null;default:''"`
	Kind               string       `gorm:"type:varchar(16);not null"`
	DraftVersionID     string       `gorm:"type:uuid;not null"`
	PublishedVersionID OptionalText `gorm:"type:uuid"`
	CreatedBy          OptionalText `gorm:"type:uuid"`
	CreatedAt          time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt          time.Time    `gorm:"type:timestamptz;not null;index:idx_whatsapp_forms_workspace_updated,priority:2,sort:desc"`
	ArchivedAt         *time.Time   `gorm:"type:timestamptz"`
}

func (WhatsAppForm) TableName() string {
	return "whatsapp_forms"
}

type WhatsAppFormVersion struct {
	ID              string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID     string       `gorm:"type:uuid;not null"`
	FormID          string       `gorm:"type:uuid;not null;uniqueIndex:ux_whatsapp_form_versions_number,priority:1"`
	Number          int          `gorm:"not null;uniqueIndex:ux_whatsapp_form_versions_number,priority:2"`
	Document        string       `gorm:"type:jsonb;not null"`
	FlowJSON        OptionalText `gorm:"column:flow_json;type:json"`
	FlowVersion     string       `gorm:"type:varchar(16);not null;default:''"`
	CompilerVersion string       `gorm:"type:varchar(32);not null;default:''"`
	Hash            string       `gorm:"type:varchar(128);not null;default:''"`
	Problems        string       `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedBy       OptionalText `gorm:"type:uuid"`
	CreatedAt       time.Time    `gorm:"type:timestamptz;not null"`
}

func (WhatsAppFormVersion) TableName() string {
	return "whatsapp_form_versions"
}

type WhatsAppFormPublication struct {
	ID               string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID      string       `gorm:"type:uuid;not null"`
	FormID           string       `gorm:"type:uuid;not null;uniqueIndex:ux_whatsapp_form_publications_account,priority:1"`
	WABAID           OptionalText `gorm:"column:waba_id;type:uuid"`
	MetaWABAID       string       `gorm:"column:meta_waba_id;type:varchar(64);not null;uniqueIndex:ux_whatsapp_form_publications_account,priority:2;uniqueIndex:ux_whatsapp_form_publications_flow,priority:1,where:meta_flow_id IS NOT NULL"`
	MetaFlowID       OptionalText `gorm:"type:varchar(64);uniqueIndex:ux_whatsapp_form_publications_flow,priority:2,where:meta_flow_id IS NOT NULL"`
	MetaName         string       `gorm:"type:varchar(200);not null"`
	VersionID        string       `gorm:"type:uuid;not null"`
	Status           string       `gorm:"type:varchar(16);not null;index:idx_whatsapp_form_publications_creating,priority:1,where:status = 'creating'"`
	Problems         string       `gorm:"type:jsonb;not null;default:'[]'"`
	LastError        string       `gorm:"type:text;not null;default:''"`
	PreviewURL       string       `gorm:"type:text;not null;default:''"`
	PreviewExpiresAt *time.Time   `gorm:"type:timestamptz"`
	PublishedAt      *time.Time   `gorm:"type:timestamptz"`
	CreatedAt        time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt        time.Time    `gorm:"type:timestamptz;not null;index:idx_whatsapp_form_publications_creating,priority:2,where:status = 'creating'"`
}

func (WhatsAppFormPublication) TableName() string {
	return "whatsapp_form_publications"
}

type WhatsAppFormSend struct {
	ID              string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID     string       `gorm:"type:uuid;not null;index:idx_whatsapp_form_sends_form_created,priority:1"`
	FormID          string       `gorm:"type:uuid;not null;index:idx_whatsapp_form_sends_form_created,priority:2"`
	VersionID       string       `gorm:"type:uuid;not null"`
	PublicationID   string       `gorm:"type:uuid;not null"`
	FlowToken       string       `gorm:"type:varchar(64);not null;uniqueIndex:ux_whatsapp_form_sends_token"`
	Origin          string       `gorm:"type:varchar(32);not null"`
	OriginRef       string       `gorm:"type:varchar(191);not null;default:''"`
	EntryID         OptionalText `gorm:"type:uuid"`
	EntryType       string       `gorm:"type:varchar(20);not null;default:''"`
	ConversationID  string       `gorm:"type:varchar(64);not null;default:''"`
	LeadID          OptionalText `gorm:"type:uuid"`
	BusinessPhoneID string       `gorm:"type:uuid;not null"`
	ContactNumber   string       `gorm:"type:varchar(32);not null"`
	MessageID       OptionalText `gorm:"type:varchar(191);index:idx_whatsapp_form_sends_message"`
	TemplateID      OptionalText `gorm:"type:uuid"`
	WorkflowRunID   OptionalText `gorm:"type:uuid;index:idx_whatsapp_form_sends_workflow_run,where:workflow_run_id IS NOT NULL"`
	SentBy          OptionalText `gorm:"type:uuid"`
	Status          string       `gorm:"type:varchar(16);not null"`
	FailureReason   string       `gorm:"type:text;not null;default:''"`
	CreatedAt       time.Time    `gorm:"type:timestamptz;not null;index:idx_whatsapp_form_sends_form_created,priority:3,sort:desc"`
	SentAt          *time.Time   `gorm:"type:timestamptz"`
	RespondedAt     *time.Time   `gorm:"type:timestamptz"`
}

func (WhatsAppFormSend) TableName() string {
	return "whatsapp_form_sends"
}

type WhatsAppFormResponse struct {
	ID               string       `gorm:"primaryKey;type:uuid;index:idx_whatsapp_form_responses_form_submitted,priority:4,sort:desc"`
	WorkspaceID      string       `gorm:"type:uuid;not null;index:idx_whatsapp_form_responses_form_submitted,priority:1"`
	FormID           OptionalText `gorm:"type:uuid;index:idx_whatsapp_form_responses_form_submitted,priority:2"`
	VersionID        OptionalText `gorm:"type:uuid"`
	SendID           OptionalText `gorm:"type:uuid"`
	Origin           OptionalText `gorm:"type:varchar(32)"`
	Match            string       `gorm:"type:varchar(32);not null"`
	InboundMessageID string       `gorm:"type:varchar(191);not null;uniqueIndex:ux_whatsapp_form_responses_inbound"`
	ConversationID   string       `gorm:"type:varchar(64);not null;default:''"`
	EntryID          OptionalText `gorm:"type:uuid;index:idx_whatsapp_form_responses_entry"`
	EntryType        string       `gorm:"type:varchar(20);not null;default:''"`
	LeadID           OptionalText `gorm:"type:uuid;index:idx_whatsapp_form_responses_lead"`
	BusinessPhoneID  string       `gorm:"type:uuid;not null"`
	ContactNumber    string       `gorm:"type:varchar(32);not null"`
	Answers          string       `gorm:"type:jsonb;not null"`
	Raw              OptionalText `gorm:"type:jsonb"`
	ApplyStatus      string       `gorm:"type:varchar(16);not null"`
	Report           string       `gorm:"type:jsonb;not null;default:'{}'"`
	SubmittedAt      time.Time    `gorm:"type:timestamptz;not null;index:idx_whatsapp_form_responses_form_submitted,priority:3,sort:desc"`
	CreatedAt        time.Time    `gorm:"type:timestamptz;not null"`
}

func (WhatsAppFormResponse) TableName() string {
	return "whatsapp_form_responses"
}
