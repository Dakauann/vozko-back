package emailsending

import (
	"time"

	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

type AccountResponse struct {
	ID                 string     `json:"id"`
	WorkspaceID        string     `json:"workspace_id"`
	Provider           string     `json:"provider"`
	TenantName         string     `json:"tenant_name"`
	Mode               string     `json:"mode"`
	Status             string     `json:"status" enums:"active,paused_by_provider,paused_by_platform,dormant"`
	StatusReason       string     `json:"status_reason"`
	Slug               string     `json:"slug"`
	ProbationStartedAt *time.Time `json:"probation_started_at"`
	PromotedAt         *time.Time `json:"promoted_at"`
}

type ProbationTargetsResponse struct {
	Days               int   `json:"days" example:"14"`
	MinDelivered       int64 `json:"min_delivered" example:"200"`
	MaxBouncePerMil    int64 `json:"max_bounce_per_mil" example:"20"`
	MaxComplaintPer10k int64 `json:"max_complaint_per_10k" example:"8"`
}

type ProbationResponse struct {
	Days         int                      `json:"days"`
	RequiredDays int                      `json:"required_days"`
	Delivered    int64                    `json:"delivered"`
	HardBounces  int64                    `json:"hard_bounces"`
	Complaints   int64                    `json:"complaints"`
	Outcome      string                   `json:"outcome"`
	Targets      ProbationTargetsResponse `json:"targets"`
}

type RecordResponse struct {
	Purpose  string `json:"purpose"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Host     string `json:"host" example:"reply"`
	Value    string `json:"value"`
	Priority int    `json:"priority"`
	TTL      int    `json:"ttl"`
	Status   string `json:"status" enums:"verified,pending,unknown"`
}

type SetupProgressResponse struct {
	State     string `json:"state" enums:"in_progress,failed,done"`
	ErrorCode string `json:"error_code,omitempty" enums:"email_provider_unavailable,email_reply_capacity_reached"`
}

type DomainActionsResponse struct {
	Recheck bool `json:"recheck"`
	Remove  bool `json:"remove"`
}

type DomainResponse struct {
	ID              string                `json:"id"`
	Domain          string                `json:"domain"`
	Kind            string                `json:"kind"`
	Status          string                `json:"status"`
	ConnectMethod   string                `json:"connect_method"`
	MailFromDomain  string                `json:"mail_from_domain"`
	ReplyDomain     string                `json:"reply_domain"`
	Records         []RecordResponse      `json:"records"`
	FailureReason   string                `json:"failure_reason"`
	VerifiedAt      *time.Time            `json:"verified_at"`
	LastCheckedAt   *time.Time            `json:"last_checked_at"`
	Retired         bool                  `json:"retired"`
	RepliesPaused   bool                  `json:"replies_paused"`
	RepliesPausedAt *time.Time            `json:"replies_paused_at"`
	Sendable        bool                  `json:"sendable"`
	Lifecycle       string                `json:"lifecycle" enums:"active,releasing,retired"`
	Actions         DomainActionsResponse `json:"actions"`
	Setup           SetupProgressResponse `json:"setup"`
	UsedToday       *int64                `json:"used_today,omitempty"`
	CapToday        *int64                `json:"cap_today,omitempty"`
	DailyLimit      string                `json:"daily_limit,omitempty" enums:"capped,unlimited,blocked"`
}

type SenderResponse struct {
	ID              string `json:"id"`
	DomainID        string `json:"domain_id"`
	Address         string `json:"address"`
	LocalPart       string `json:"local_part"`
	DisplayName     string `json:"display_name"`
	ReplyForwardTo  string `json:"reply_forward_to"`
	IsDefault       bool   `json:"is_default"`
	DomainLifecycle string `json:"domain_lifecycle" enums:"active,releasing,retired"`
}

type SetupResponse struct {
	Provisioned   bool                  `json:"provisioned"`
	Account       *AccountResponse      `json:"account"`
	Probation     *ProbationResponse    `json:"probation"`
	Domains       []DomainResponse      `json:"domains"`
	Senders       []SenderResponse      `json:"senders"`
	RepliesPaused bool                  `json:"replies_paused"`
	Setup         SetupProgressResponse `json:"setup"`
	Subdomain     SubdomainResponse     `json:"subdomain"`
	Activation    ActivationResponse    `json:"activation"`
}

type ActivationResponse struct {
	Allowed   bool   `json:"allowed"`
	ErrorCode string `json:"error_code,omitempty" enums:"email_plan_required,email_plan_unavailable"`
}

type SubdomainResponse struct {
	Slug    string `json:"slug" example:"cartaodetodososasco"`
	Address string `json:"address" example:"contato@cartaodetodososasco.vozkomsg.com.br"`
	Locked  bool   `json:"locked"`
}

type SubdomainCheckResponse struct {
	Slug      string `json:"slug" example:"cartaodetodososasco"`
	Available bool   `json:"available"`
	ErrorCode string `json:"error_code,omitempty" enums:"email_subdomain_invalid,email_subdomain_reserved,email_subdomain_taken"`
	Address   string `json:"address,omitempty" example:"contato@cartaodetodososasco.vozkomsg.com.br"`
}

type ProvisionRequest struct {
	Slug string `json:"slug" example:"cartaodetodososasco"`
}

type SubdomainRequest struct {
	Slug string `json:"slug" example:"cartaodetodososasco"`
}

type ConnectDomainRequest struct {
	Domain string `json:"domain" example:"loja.com.br"`
}

type CreateSenderRequest struct {
	DomainID       string `json:"domain_id"`
	LocalPart      string `json:"local_part"`
	DisplayName    string `json:"display_name"`
	ReplyForwardTo string `json:"reply_forward_to"`
	IsDefault      bool   `json:"is_default"`
}

type UpdateSenderRequest struct {
	DomainID       *string `json:"domain_id"`
	LocalPart      *string `json:"local_part"`
	DisplayName    *string `json:"display_name"`
	ReplyForwardTo *string `json:"reply_forward_to"`
	IsDefault      *bool   `json:"is_default"`
}

type SuppressionRequest struct {
	Address string `json:"address"`
}

type SuppressionResponse struct {
	Address         string    `json:"address"`
	Reason          string    `json:"reason" enums:"hard_bounce,complaint,manual,soft_bounce"`
	SourceMessageID string    `json:"source_message_id"`
	SuppressedAt    time.Time `json:"suppressed_at"`
	Removable       bool      `json:"removable"`
}

type FindingResponse struct {
	Impact      string `json:"impact"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type AdminAccountResponse struct {
	Account         AccountResponse   `json:"account"`
	WorkspaceName   string            `json:"workspace_name"`
	Sending         string            `json:"sending"`
	Findings        []FindingResponse `json:"findings"`
	HealthError     string            `json:"health_error"`
	HealthCheckedAt *time.Time        `json:"health_checked_at"`
}

type SendQuotaResponse struct {
	Read             bool       `json:"read"`
	ReadAt           *time.Time `json:"read_at"`
	Fresh            bool       `json:"fresh"`
	ProductionAccess bool       `json:"production_access"`
	MaxSendRate      float64    `json:"max_send_rate"`
	Max24HourSend    float64    `json:"max_24_hour_send"`
	Unlimited24Hours bool       `json:"unlimited_24_hours"`
	SentLast24Hours  float64    `json:"sent_last_24_hours"`
	SentSinceRead    int64      `json:"sent_since_read"`
	Used24Hours      int64      `json:"used_24_hours"`
	UsageBasisPoints int64      `json:"usage_basis_points"`
	PriorityReserve  int64      `json:"priority_reserve"`
	NearLimit        bool       `json:"near_limit"`
}

type PauseAccountRequest struct {
	Reason string `json:"reason"`
}

func presentAccount(a es.SenderAccount) AccountResponse {
	out := AccountResponse{
		ID: a.ID, WorkspaceID: a.WorkspaceID, Provider: a.Provider, TenantName: a.TenantName,
		Mode: string(a.Mode), Status: string(a.Status), StatusReason: a.StatusReason, Slug: a.Slug, PromotedAt: a.PromotedAt,
	}
	if !a.ProbationStartedAt.IsZero() {
		started := a.ProbationStartedAt
		out.ProbationStartedAt = &started
	}
	return out
}

func presentSetupProgress(p es.SetupProgress) SetupProgressResponse {
	return SetupProgressResponse{State: string(p.State), ErrorCode: setupErrorCode(p.Failure)}
}

func presentActivation(refusal error) ActivationResponse {
	if refusal == nil {
		return ActivationResponse{Allowed: true}
	}
	return ActivationResponse{ErrorCode: refusalCode(refusal)}
}

func presentSubdomainCheck(c esuc.SubdomainCheck) SubdomainCheckResponse {
	out := SubdomainCheckResponse{Slug: c.Slug, Available: c.Refusal == nil, Address: c.Address}
	if c.Refusal != nil {
		out.ErrorCode = refusalCode(c.Refusal)
	}
	return out
}

func presentRecords(records []es.DNSRecord, domain string) []RecordResponse {
	out := make([]RecordResponse, 0, len(records))
	for _, r := range records {
		out = append(out, RecordResponse{
			Purpose: string(r.Purpose), Type: r.Type, Name: r.Name, Host: es.RelativeHost(r.Name, domain), Value: r.Value,
			Priority: r.Priority, TTL: r.TTL, Status: string(r.CurrentStatus()),
		})
	}
	return out
}

func presentDomain(d es.SendingDomain) DomainResponse {
	return DomainResponse{
		ID: d.ID, Domain: d.Domain, Kind: string(d.Kind), Status: string(d.Status), ConnectMethod: string(d.ConnectMethod),
		MailFromDomain: d.MailFromDomain, ReplyDomain: d.ReplyDomain, Records: presentRecords(d.Records, d.Domain), FailureReason: d.FailureReason,
		VerifiedAt: d.VerifiedAt, LastCheckedAt: d.LastCheckedAt, Retired: d.Retired, Sendable: d.Sendable(),
		RepliesPaused: d.RepliesPaused(), RepliesPausedAt: d.RepliesPausedAt, Setup: presentSetupProgress(d.SetupProgress()),
		Lifecycle: string(d.Lifecycle()), Actions: presentDomainActions(d.Actions()),
	}
}

func presentDomainActions(a es.DomainActions) DomainActionsResponse {
	return DomainActionsResponse{Recheck: a.Recheck, Remove: a.Remove}
}

func presentDomainView(v esuc.DomainView) DomainResponse {
	out := presentDomain(v.Domain)
	used := v.UsedToday
	out.UsedToday = &used
	out.DailyLimit = string(es.DailyLimitOf(v.CapToday))
	if out.DailyLimit == string(es.DailyLimitCapped) {
		capValue := v.CapToday
		out.CapToday = &capValue
	}
	return out
}

func presentSender(v esuc.SenderView) SenderResponse {
	s := v.Sender
	return SenderResponse{
		ID: s.ID, DomainID: s.DomainID, Address: v.Address, LocalPart: s.LocalPart, DisplayName: s.DisplayName,
		ReplyForwardTo: string(s.ReplyForwardTo), IsDefault: s.IsDefault, DomainLifecycle: string(v.Domain.Lifecycle()),
	}
}

func presentSetup(v esuc.StatusView) SetupResponse {
	out := SetupResponse{Provisioned: v.Provisioned, Domains: []DomainResponse{}, Senders: []SenderResponse{}, RepliesPaused: v.RepliesPaused, Setup: presentSetupProgress(v.Setup),
		Subdomain: SubdomainResponse{Slug: v.Subdomain.Slug, Address: v.Subdomain.Address, Locked: v.Subdomain.Locked}, Activation: presentActivation(v.Activation)}
	if v.Account != nil {
		acct := presentAccount(*v.Account)
		out.Account = &acct
	}
	if v.Probation != nil {
		out.Probation = &ProbationResponse{
			Days: v.Probation.Days, RequiredDays: v.Probation.RequiredDays, Delivered: v.Probation.Window.Delivered,
			HardBounces: v.Probation.Window.HardBounces, Complaints: v.Probation.Window.Complaints, Outcome: string(v.Probation.Outcome),
			Targets: ProbationTargetsResponse{
				Days: v.Probation.Targets.Days, MinDelivered: v.Probation.Targets.MinDelivered,
				MaxBouncePerMil: v.Probation.Targets.MaxBouncePerMil, MaxComplaintPer10k: v.Probation.Targets.MaxComplaintPer10k,
			},
		}
	}
	for _, d := range v.Domains {
		out.Domains = append(out.Domains, presentDomainView(d))
	}
	for _, s := range v.Senders {
		out.Senders = append(out.Senders, presentSender(s))
	}
	return out
}

func presentSuppression(s es.Suppression) SuppressionResponse {
	return SuppressionResponse{Address: string(s.Address), Reason: string(s.Reason), SourceMessageID: s.SourceMessage, SuppressedAt: s.SuppressedAt, Removable: s.Reason.Removable()}
}

func presentSendQuota(v esuc.SendQuotaView) SendQuotaResponse {
	q := v.Quota
	out := SendQuotaResponse{
		Read:             q.Read,
		Fresh:            v.Fresh,
		ProductionAccess: q.ProductionAccess,
		MaxSendRate:      q.MaxSendRate,
		Max24HourSend:    q.Max24HourSend,
		Unlimited24Hours: q.Unlimited24Hours(),
		SentLast24Hours:  q.SentLast24Hours,
		SentSinceRead:    v.SentSinceRead,
		Used24Hours:      v.Used24Hours,
		UsageBasisPoints: v.UsageBasisPoints,
		PriorityReserve:  v.PriorityReserve,
		NearLimit:        v.UsageBasisPoints >= es.DailyQuotaAlertBasisPoints,
	}
	if q.Read {
		readAt := q.ReadAt
		out.ReadAt = &readAt
	}
	return out
}

func presentAdminAccount(v esuc.AccountHealthView) AdminAccountResponse {
	out := AdminAccountResponse{Account: presentAccount(v.Account), WorkspaceName: v.WorkspaceName, Findings: []FindingResponse{}, HealthError: v.HealthError, HealthCheckedAt: v.HealthCheckedAt}
	if v.Health != nil {
		out.Sending = string(v.Health.Sending)
		for _, f := range v.Health.Findings {
			out.Findings = append(out.Findings, FindingResponse{Impact: string(f.Impact), Type: f.Type, Description: f.Description})
		}
	}
	return out
}
