package carriermessaging

import (
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	cmuc "vozko/usecases/carriermessaging"
)

type SenderRequest struct {
	BrandName                string `json:"brand_name"`
	Description              string `json:"description"`
	LogoURL                  string `json:"logo_url"`
	BannerURL                string `json:"banner_url"`
	Color                    string `json:"color"`
	UseCase                  string `json:"use_case"`
	Sector                   string `json:"sector"`
	GamblingAuthorizationURL string `json:"gambling_authorization_url"`
	ContactName              string `json:"contact_name"`
	ContactEmail             string `json:"contact_email"`
	ContactPhone             string `json:"contact_phone"`
	WebsiteURL               string `json:"website_url"`
	PrivacyURL               string `json:"privacy_url"`
	TermsURL                 string `json:"terms_url"`
	CNPJ                     string `json:"cnpj"`
	BillingCategory          string `json:"billing_category"`
}

func (r SenderRequest) input() cmuc.SenderInput {
	return cmuc.SenderInput{
		BrandName: r.BrandName, Description: r.Description, LogoURL: r.LogoURL, BannerURL: r.BannerURL, Color: r.Color,
		UseCase: rcs.UseCase(r.UseCase), Sector: rcs.Sector(r.Sector), GamblingAuthorizationURL: r.GamblingAuthorizationURL,
		ContactName: r.ContactName, ContactEmail: r.ContactEmail, ContactPhone: r.ContactPhone, WebsiteURL: r.WebsiteURL,
		PrivacyURL: r.PrivacyURL, TermsURL: r.TermsURL, CNPJ: r.CNPJ, BillingCategory: rcs.BillingCategory(r.BillingCategory),
	}
}

type SenderDTO struct {
	ID                       string        `json:"id"`
	BrandName                string        `json:"brand_name"`
	Description              string        `json:"description"`
	SuggestedDescription     string        `json:"suggested_description"`
	LogoURL                  string        `json:"logo_url"`
	BannerURL                string        `json:"banner_url"`
	Color                    string        `json:"color"`
	UseCase                  string        `json:"use_case"`
	Sector                   string        `json:"sector"`
	GamblingAuthorizationURL string        `json:"gambling_authorization_url"`
	ContactName              string        `json:"contact_name"`
	ContactEmail             string        `json:"contact_email"`
	ContactPhone             string        `json:"contact_phone"`
	WebsiteURL               string        `json:"website_url"`
	PrivacyURL               string        `json:"privacy_url"`
	TermsURL                 string        `json:"terms_url"`
	CNPJ                     string        `json:"cnpj"`
	BillingCategory          string        `json:"billing_category"`
	Status                   string        `json:"status"`
	StatusReason             string        `json:"status_reason"`
	PausedBy                 string        `json:"paused_by"`
	PauseReason              string        `json:"pause_reason"`
	SubmittedAt              *time.Time    `json:"submitted_at"`
	LaunchedAt               *time.Time    `json:"launched_at"`
	PausedAt                 *time.Time    `json:"paused_at"`
	CreatedAt                time.Time     `json:"created_at"`
	CanSend                  bool          `json:"can_send"`
	Editable                 bool          `json:"editable"`
	CanResume                bool          `json:"can_resume"`
	DepartmentID             *string       `json:"department_id"`
	PipelineID               *string       `json:"pipeline_id"`
	Automation               AutomationDTO `json:"automation"`
	WorkspaceID              string        `json:"workspace_id,omitempty"`
}

func senderDTO(s *rcs.Sender, suggested string) SenderDTO {
	return SenderDTO{
		ID: s.ID, BrandName: s.BrandName, Description: s.Description, SuggestedDescription: suggested,
		LogoURL: s.LogoURL, BannerURL: s.BannerURL, Color: s.Color, UseCase: string(s.UseCase), Sector: string(s.Sector),
		GamblingAuthorizationURL: s.GamblingAuthorizationURL, ContactName: s.ContactName, ContactEmail: s.ContactEmail,
		ContactPhone: s.ContactPhone, WebsiteURL: s.WebsiteURL, PrivacyURL: s.PrivacyURL, TermsURL: s.TermsURL,
		CNPJ: s.CNPJ, BillingCategory: string(s.BillingCategory), Status: string(s.Status), StatusReason: s.StatusReason,
		PausedBy: string(s.PausedBy), PauseReason: string(s.PauseReason), SubmittedAt: s.SubmittedAt,
		LaunchedAt: s.LaunchedAt, PausedAt: s.PausedAt, CreatedAt: s.CreatedAt, CanSend: s.CanSend(), Editable: s.Editable(), CanResume: s.ResumableByWorkspace(),
		DepartmentID: s.DepartmentID, PipelineID: s.PipelineID, Automation: automationDTO(s.Automation),
	}
}

type SendersResponse struct {
	Senders []SenderDTO `json:"senders"`
}

type RouteRequest struct {
	BrandPrefix string `json:"brand_prefix"`
}

type RouteNumberRequest struct {
	Number string `json:"number"`
}

type RouteDTO struct {
	ID           string        `json:"id"`
	BrandPrefix  string        `json:"brand_prefix"`
	Kind         string        `json:"kind"`
	Number       string        `json:"number"`
	Status       string        `json:"status"`
	PauseReason  string        `json:"pause_reason"`
	CanSend      bool          `json:"can_send"`
	DepartmentID *string       `json:"department_id"`
	PipelineID   *string       `json:"pipeline_id"`
	Automation   AutomationDTO `json:"automation"`
	CreatedAt    time.Time     `json:"created_at"`
	WorkspaceID  string        `json:"workspace_id,omitempty"`
}

func routeDTO(r *sms.Route) RouteDTO {
	return RouteDTO{
		ID: r.ID, BrandPrefix: r.BrandPrefix, Kind: string(r.Kind), Number: r.Number, Status: string(r.Status),
		PauseReason: r.PauseReason, CanSend: r.CanSend(), CreatedAt: r.CreatedAt,
		DepartmentID: r.DepartmentID, PipelineID: r.PipelineID, Automation: automationDTO(r.Automation),
	}
}

type RoutesResponse struct {
	Routes []RouteDTO `json:"routes"`
}

type IssueDTO struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
}

func issueCode(err error) string {
	if refusal, ok := response.MatchRefusal(err, refusals); ok {
		return refusal.Code
	}
	return refusalFailed.Code
}

type ContentCheckRequest struct {
	Content rcs.Content `json:"content"`
}

type PriceDTO struct {
	ChargeKind  string `json:"charge_kind"`
	UnitMicros  int64  `json:"unit_micros"`
	Quantity    int    `json:"quantity"`
	TotalMicros int64  `json:"total_micros"`
}

type ContentCheckResponse struct {
	Kind          string     `json:"kind"`
	BillingClass  string     `json:"billing_class"`
	VariableSlots int        `json:"variable_slots"`
	Issues        []IssueDTO `json:"issues"`
	Price         PriceDTO   `json:"price"`
}

func contentCheckResponse(q cmuc.RCSQuote) ContentCheckResponse {
	issues := make([]IssueDTO, 0, len(q.Issues))
	for _, issue := range q.Issues {
		issues = append(issues, IssueDTO{Code: issueCode(issue.Err), Path: issue.Path})
	}
	return ContentCheckResponse{
		Kind: string(q.Kind), BillingClass: string(q.Class), VariableSlots: q.VariableSlots, Issues: issues,
		Price: PriceDTO{ChargeKind: string(q.Charge), UnitMicros: q.UnitMicros, Quantity: 1, TotalMicros: q.UnitMicros},
	}
}

type TextCheckRequest struct {
	Text    string `json:"text"`
	RouteID string `json:"route_id"`
}

type TextCheckResponse struct {
	Measurement   sms.Measurement `json:"measurement"`
	ComposedText  string          `json:"composed_text"`
	VariableSlots int             `json:"variable_slots"`
	Issues        []IssueDTO      `json:"issues"`
	Price         PriceDTO        `json:"price"`
}

func textCheckResponse(q cmuc.SMSQuote) TextCheckResponse {
	issues := make([]IssueDTO, 0, len(q.Issues))
	for _, err := range q.Issues {
		issues = append(issues, IssueDTO{Code: issueCode(err)})
	}
	return TextCheckResponse{
		Measurement: q.Measurement, ComposedText: q.Text, VariableSlots: q.VariableSlots, Issues: issues,
		Price: PriceDTO{ChargeKind: string(q.Charge), UnitMicros: q.UnitMicros, Quantity: q.Measurement.Segments, TotalMicros: q.TotalMicros},
	}
}

type ToGSMRequest struct {
	Text string `json:"text"`
}

type ToGSMResponse struct {
	Text string `json:"text"`
}

type SuppressionRequest struct {
	Phone string `json:"phone"`
}

type SuppressionDTO struct {
	Phone     string    `json:"phone"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type SuppressionsResponse struct {
	Items []SuppressionDTO `json:"items"`
	Total int              `json:"total"`
}

type TestSendRequest struct {
	Channel  string      `json:"channel"`
	SenderID string      `json:"sender_id"`
	Phone    string      `json:"phone"`
	Content  rcs.Content `json:"content"`
	Text     string      `json:"text"`
	Vars     []string    `json:"vars"`
}

type MessageDTO struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code"`
	Class     string `json:"class,omitempty"`
	Segments  int    `json:"segments,omitempty"`
}

func messageDTO(m *carriermessaging.Message) MessageDTO {
	return MessageDTO{
		ID: m.ID, Channel: string(m.Channel), Reference: m.Reference, Status: string(m.Status),
		ErrorCode: m.ErrorCode, Class: string(m.Class), Segments: m.Segments,
	}
}

type AutomationDTO struct {
	AgentID              *string `json:"agent_id"`
	WorkflowID           *string `json:"workflow_id"`
	EnableAgentResponses bool    `json:"enable_agent_responses"`
	EnableWorkflow       bool    `json:"enable_workflow"`
	EnableAnalysis       bool    `json:"enable_analysis"`
	EnableAutoStaging    bool    `json:"enable_auto_staging"`
	EnableAutoMemory     bool    `json:"enable_auto_memory"`
	Disclosure           string  `json:"disclosure"`
}

func automationDTO(a conversation.ChannelAutomation) AutomationDTO {
	return AutomationDTO{
		AgentID: a.AgentID, WorkflowID: a.WorkflowID, EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow: a.EnableWorkflow, EnableAnalysis: a.EnableAnalysis, EnableAutoStaging: a.EnableAutoStaging,
		EnableAutoMemory: a.EnableAutoMemory, Disclosure: a.Disclosure,
	}
}

func (a AutomationDTO) domain() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{
		AgentID: a.AgentID, WorkflowID: a.WorkflowID, EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow: a.EnableWorkflow, EnableAnalysis: a.EnableAnalysis, EnableAutoStaging: a.EnableAutoStaging,
		EnableAutoMemory: a.EnableAutoMemory, Disclosure: a.Disclosure,
	}
}

type InboxRequest struct {
	DepartmentID *string       `json:"department_id"`
	PipelineID   *string       `json:"pipeline_id"`
	Automation   AutomationDTO `json:"automation"`
}

func (r InboxRequest) domain() cmuc.InboxSettings {
	return cmuc.InboxSettings{DepartmentID: r.DepartmentID, PipelineID: r.PipelineID, Automation: r.Automation.domain()}
}

type ChannelDTO struct {
	Channel  string `json:"channel"`
	Launched bool   `json:"launched"`
}

type ChannelsResponse struct {
	Channels []ChannelDTO `json:"channels"`
}

func channelsResponse() ChannelsResponse {
	out := ChannelsResponse{Channels: []ChannelDTO{}}
	for _, c := range carriermessaging.AllChannels() {
		out.Channels = append(out.Channels, ChannelDTO{Channel: string(c), Launched: c.Launched()})
	}
	return out
}
