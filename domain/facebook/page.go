package facebook

import (
	"errors"
	"sort"
	"strings"
	"time"

	"vozko/domain/conversation"
)

var (
	ErrPageNotFound         = errors.New("facebook page not found")
	ErrPageAlreadyLinked    = errors.New("facebook page is already connected to another workspace")
	ErrPageIDRequired       = errors.New("facebook page id is required")
	ErrWorkspaceIDRequired  = errors.New("workspace id is required")
	ErrGrantNotFound        = errors.New("facebook grant not found")
	ErrNoPagesGranted       = errors.New("no facebook page was granted")
	ErrGrantUnverifiable    = errors.New("facebook did not report which pages were granted")
	ErrAuthorizationDenied  = errors.New("facebook authorization was declined")
	ErrInvalidStatus        = errors.New("invalid facebook page status")
	ErrStatusTransition     = errors.New("invalid facebook page status transition")
	ErrCapabilityDenied     = errors.New("facebook page lacks the permission or task for this action")
	ErrAccessTokenRequired  = errors.New("facebook page access token is required")
	ErrThreadOwnedElsewhere = errors.New("another app controls this messenger thread")
	ErrTextTooLong          = errors.New("messenger text is longer than 2000 characters")
)

const (
	ScopeShowList           = "pages_show_list"
	ScopeMessaging          = "pages_messaging"
	ScopeManageMetadata     = "pages_manage_metadata"
	ScopeReadEngagement     = "pages_read_engagement"
	ScopeReadUserContent    = "pages_read_user_content"
	ScopeManagePosts        = "pages_manage_posts"
	ScopeManageEngagement   = "pages_manage_engagement"
	ScopeBusinessManagement = "business_management"
)

type Task string

const (
	TaskManage        Task = "MANAGE"
	TaskCreateContent Task = "CREATE_CONTENT"
	TaskModerate      Task = "MODERATE"
	TaskMessaging     Task = "MESSAGING"
	TaskAdvertise     Task = "ADVERTISE"
	TaskAnalyze       Task = "ANALYZE"
)

type Capability string

const (
	CapMessaging Capability = "messaging"
	CapReadPosts Capability = "read_posts"
	CapPublish   Capability = "publish"
	CapModerate  Capability = "moderate"
	CapComment   Capability = "comment"
	CapSubscribe Capability = "subscribe"
)

type capabilityRule struct {
	scopes []string
	tasks  []Task
}

var capabilityRules = map[Capability]capabilityRule{
	CapMessaging: {scopes: []string{ScopeMessaging, ScopeManageMetadata}, tasks: []Task{TaskMessaging, TaskManage}},
	CapReadPosts: {scopes: []string{ScopeReadEngagement, ScopeReadUserContent}, tasks: []Task{TaskCreateContent, TaskManage, TaskModerate}},
	CapPublish:   {scopes: []string{ScopeManagePosts, ScopeReadEngagement}, tasks: []Task{TaskCreateContent, TaskManage}},
	CapModerate:  {scopes: []string{ScopeManageEngagement, ScopeReadUserContent}, tasks: []Task{TaskModerate, TaskManage}},
	CapComment:   {scopes: []string{ScopeManageEngagement}, tasks: []Task{TaskModerate, TaskCreateContent, TaskManage}},
	CapSubscribe: {scopes: []string{ScopeManageMetadata}, tasks: []Task{TaskCreateContent, TaskManage, TaskModerate}},
}

func AllCapabilities() []Capability {
	out := make([]Capability, 0, len(capabilityRules))
	for c := range capabilityRules {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func RequiredScopes() []string {
	return []string{
		ScopeShowList, ScopeManageMetadata, ScopeMessaging, ScopeReadEngagement,
		ScopeReadUserContent, ScopeManagePosts, ScopeManageEngagement, ScopeBusinessManagement,
	}
}

type Status string

const (
	StatusPending      Status = "PENDING"
	StatusConnected    Status = "CONNECTED"
	StatusTokenRevoked Status = "TOKEN_REVOKED"
	StatusNeedsRole    Status = "NEEDS_ROLE"
	StatusRestricted   Status = "RESTRICTED"
	StatusDisconnected Status = "DISCONNECTED"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusConnected, StatusTokenRevoked, StatusNeedsRole, StatusRestricted, StatusDisconnected:
		return true
	}
	return false
}

func (s Status) CanTransitionTo(next Status) bool {
	if !next.Valid() {
		return false
	}
	if s == next {
		return true
	}
	switch s {
	case StatusPending:
		return next == StatusConnected || next == StatusDisconnected
	case StatusConnected:
		return next == StatusTokenRevoked || next == StatusNeedsRole || next == StatusRestricted || next == StatusDisconnected
	case StatusTokenRevoked, StatusNeedsRole, StatusRestricted:
		return next == StatusConnected || next == StatusDisconnected
	case StatusDisconnected:
		return next == StatusConnected
	}
	return false
}

type GrantStatus string

const (
	GrantActive  GrantStatus = "ACTIVE"
	GrantRevoked GrantStatus = "REVOKED"
)

type TokenKind string

const (
	TokenSystemUser TokenKind = "SYSTEM_USER"
	TokenUser       TokenKind = "USER"
)

type Grant struct {
	ID               string
	WorkspaceID      string
	ConnectedBy      string
	TokenKind        TokenKind
	AccessToken      string
	TokenExpiresAt   *time.Time
	AppScopedUserID  string
	ClientBusinessID string
	Scopes           []string
	GranularScopes   map[string][]string
	Status           GrantStatus
	CheckedAt        *time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (g *Grant) Lists(fbPageID string) bool {
	return contains(g.GranularScopes[ScopeShowList], fbPageID)
}

func (g *Grant) ScopesForPage(fbPageID string) []string {
	if !g.Lists(fbPageID) {
		return nil
	}
	out := make([]string, 0, len(g.GranularScopes))
	for scope, targets := range g.GranularScopes {
		if len(targets) == 0 || contains(targets, fbPageID) {
			out = append(out, scope)
		}
	}
	sort.Strings(out)
	return out
}

func (g *Grant) PageIDs() []string {
	return append([]string(nil), g.GranularScopes[ScopeShowList]...)
}

type Page struct {
	ID           string
	WorkspaceID  string
	DepartmentID *string
	GrantID      string

	FBPageID          string
	Name              string
	Username          string
	Category          string
	Link              string
	PictureStorageKey string
	FollowersCount    int
	LinkedIGUserID    string

	PageToken     string
	Tasks         []Task
	GrantedScopes []string

	AgentID              *string
	WorkflowID           *string
	PipelineID           *string
	EnableAgentResponses bool
	EnableWorkflow       bool
	EnableAnalysis       bool
	EnableAutoStaging    bool
	EnableAutoMemory     bool
	AutomationDisclosure string

	Status       Status
	StatusReason string

	SubscribedFields    []string
	WebhookSubscribedAt *time.Time
	IsDefaultRouteApp   *bool
	RoutingCheckedAt    *time.Time
	PolicyAction        string
	PolicyReason        string
	PolicyAt            *time.Time
	HealthCheckedAt     *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (p *Page) Normalize() {
	p.WorkspaceID = strings.TrimSpace(p.WorkspaceID)
	p.FBPageID = strings.TrimSpace(p.FBPageID)
	p.Name = strings.TrimSpace(p.Name)
	if p.Status == "" {
		p.Status = StatusPending
	}
	p.GrantedScopes = dedupe(p.GrantedScopes, strings.TrimSpace)
	tasks := make([]string, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		tasks = append(tasks, string(t))
	}
	p.Tasks = p.Tasks[:0]
	for _, t := range dedupe(tasks, func(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }) {
		p.Tasks = append(p.Tasks, Task(t))
	}
}

func (p *Page) Validate() error {
	if strings.TrimSpace(p.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if strings.TrimSpace(p.FBPageID) == "" {
		return ErrPageIDRequired
	}
	if !p.Status.Valid() {
		return ErrInvalidStatus
	}
	return nil
}

func (p *Page) HasScope(scope string) bool { return contains(p.GrantedScopes, scope) }

func (p *Page) HasTask(task Task) bool {
	for _, t := range p.Tasks {
		if t == task {
			return true
		}
	}
	return false
}

func (p *Page) Can(c Capability) bool {
	if p == nil || p.Status != StatusConnected {
		return false
	}
	rule, ok := capabilityRules[c]
	if !ok {
		return false
	}
	for _, scope := range rule.scopes {
		if !p.HasScope(scope) {
			return false
		}
	}
	for _, task := range rule.tasks {
		if p.HasTask(task) {
			return true
		}
	}
	return false
}

func (p *Page) Capabilities() map[Capability]bool {
	out := make(map[Capability]bool, len(capabilityRules))
	for c := range capabilityRules {
		out[c] = p.Can(c)
	}
	return out
}

func (p *Page) NeedsReconnect() bool {
	return p.Status == StatusTokenRevoked || p.Status == StatusNeedsRole
}

func (p *Page) Automation() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{
		AgentID:              p.AgentID,
		WorkflowID:           p.WorkflowID,
		EnableAgentResponses: p.EnableAgentResponses,
		EnableWorkflow:       p.EnableWorkflow,
		EnableAnalysis:       p.EnableAnalysis,
		EnableAutoStaging:    p.EnableAutoStaging,
		EnableAutoMemory:     p.EnableAutoMemory,
		Disclosure:           p.disclosure(),
	}
}

const DefaultAutomationDisclosure = "Você está conversando com um assistente virtual. Peça um atendente a qualquer momento."

func (p *Page) disclosure() string {
	if text := strings.TrimSpace(p.AutomationDisclosure); text != "" {
		return text
	}
	return DefaultAutomationDisclosure
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func dedupe(in []string, clean func(string) string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		s = clean(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
